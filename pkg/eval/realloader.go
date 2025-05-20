package eval

import (
	"context"
	"fmt"
	"slices"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	discoveryclient "k8s.io/client-go/discovery"
	dynamicclient "k8s.io/client-go/dynamic"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/inecas/kube-health/pkg/status"
)

// RealLoader is responsible for loading the objects from the cluster.
type RealLoader struct {
	client *client
	mapper meta.RESTMapper
}

func NewRealLoader(config RESTClientGetter) (*RealLoader, error) {
	client, err := newGenericClient(config)
	if err != nil {
		return nil, err
	}

	return &RealLoader{client: client}, nil
}

// Get returns the updated version of the object. If the object is not
// in the cache, it loads it from the cluster first.
func (l *RealLoader) Get(ctx context.Context, obj *status.Object) (*status.Object, error) {
	unst, err := l.client.get(ctx, obj)
	if err != nil {
		return nil, err
	}

	ret, err := status.NewObjectFromUnstructured(unst)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

// To replace to original interface, and get the common logic from loader
// to evaluator.
func (l *RealLoader) Load(ctx context.Context, ns string, matcher GroupKindMatcher, exclude []schema.GroupKind) ([]*status.Object, error) {
	var ret []*status.Object
	unsts, err := l.client.listWithMatcher(ctx, ns, matcher, exclude)

	if err != nil {
		return nil, err
	}

	for _, unst := range unsts {
		obj, err := status.NewObjectFromUnstructured(unst)
		if err != nil {
			return nil, err
		}
		ret = append(ret, obj)
	}

	return ret, nil
}

func (l *RealLoader) LoadPodLogs(ctx context.Context, obj *status.Object, container string, tailLines int64) ([]byte, error) {
	return l.client.podLogs(ctx, obj, container, tailLines)
}

func (l *RealLoader) ResourceToKind(gr schema.GroupResource) schema.GroupVersionKind {
	return l.client.resources[gr].GroupVersionKind
}

func (l *RealLoader) LoadResource(ctx context.Context, gr schema.GroupResource, namespace string, name string) ([]*status.Object, error) {
	gvk := l.client.resources[gr].GroupVersionKind

	gvr := schema.GroupVersionResource{
		Group:    gr.Group,
		Version:  gvk.Version,
		Resource: gr.Resource,
	}

	// if we know the name then get the resource directly
	if name != "" {
		u, err := l.client.dynamic.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		obj, err := status.NewObjectFromUnstructured(u)
		if err != nil {
			return nil, err
		}
		return []*status.Object{obj}, nil
	}

	unsts, err := l.client.list(ctx, gvr, namespace)
	if err != nil {
		return nil, err
	}

	var ret []*status.Object
	for _, unst := range unsts {
		obj, err := status.NewObjectFromUnstructured(unst)
		if err != nil {
			return nil, err
		}
		ret = append(ret, obj)
	}

	return ret, nil
}

// RESTClientGetter is an interface with a subset of
// k8s.io/cli-runtime/pkg/genericclioptions.RESTClientGetter,
// We reduce it only to the methods we need.
type RESTClientGetter interface {
	ToRESTConfig() (*rest.Config, error)
	ToDiscoveryClient() (discoveryclient.CachedDiscoveryInterface, error)
	ToRESTMapper() (meta.RESTMapper, error)
}

// client provides different ways to query the cluster to support the Loader.
type client struct {
	dynamic      dynamicclient.Interface
	mapper       meta.RESTMapper
	config       *rest.Config
	corev1client corev1client.CoreV1Interface
	resources    resourcesMap
}

func newGenericClient(clientGetter RESTClientGetter) (*client, error) {
	config, err := clientGetter.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	dynamic, err := buildDynamicClient(config)
	if err != nil {
		return nil, err
	}

	discovery, err := clientGetter.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}

	coreclient, err := corev1client.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create corev1 client: %w", err)
	}

	mapper, err := clientGetter.ToRESTMapper()
	if err != nil {
		return nil, err
	}

	ret := &client{
		dynamic:      dynamic,
		corev1client: coreclient,
		mapper:       mapper,
		resources:    make(resourcesMap),
	}

	if err := ret.discover(discovery); err != nil {
		return nil, err
	}

	return ret, nil
}

// discover queries the API server to discover all available resources.
func (c *client) discover(discovery discoveryclient.DiscoveryInterface) error {
	resList, err := discovery.ServerPreferredResources()
	if err != nil {
		return fmt.Errorf("failed to query api discovery: %w", err)
	}

	for _, group := range resList {

		gv, err := schema.ParseGroupVersion(group.GroupVersion)
		if err != nil {
			return fmt.Errorf("%q cannot be parsed into groupversion: %w", group.GroupVersion, err)
		}

		for _, apiRes := range group.APIResources {
			klog.V(5).InfoS("discovered api", "group", gv.Group, "version", gv.Version,
				"api", apiRes.Name, "namespaced", apiRes.Namespaced)

			if !slices.Contains(apiRes.Verbs, "list") {
				klog.V(5).Infof("api (%s) doesn't have required verb, skipping: %v", apiRes.Name, apiRes.Verbs)
				continue
			}
			gr := schema.GroupResource{
				Group:    gv.Group,
				Resource: apiRes.Name,
			}
			gvk := groupVersionKindNamespaced{
				GroupVersionKind: schema.GroupVersionKind{
					Group:   gv.Group,
					Version: gv.Version,
					Kind:    apiRes.Kind,
				},
				namespaced: apiRes.Namespaced,
			}

			c.resources[gr] = gvk
		}
	}
	return nil
}

// listWithMatcher lists all resources that match the given matcher.
// We support additional filtering by excluding some GroupKinds, to skip loading
// objects that are matched by the matcher, but we want to avoid them (for example
// when we already loaded the objects before).
func (c *client) listWithMatcher(ctx context.Context, ns string,
	matcher GroupKindMatcher, excludedGks []schema.GroupKind) ([]*unstructured.Unstructured, error) {

	resources := c.compileGroupKindMatcher(matcher, ns)

	if len(excludedGks) > 0 {
		resources = c.filterResources(resources, true, nil, excludedGks)
	}

	return c.listBulk(ctx, ns, resources.toSlice())
}

func (c *client) compileGroupKindMatcher(matcher GroupKindMatcher, ns string) resourcesMap {
	filterResources := func(resources resourcesMap) resourcesMap {
		return c.filterResources(resources, matcher.IncludeAll, matcher.IncludedKinds, matcher.ExcludedKinds)
	}

	switch ns {
	case NamespaceAll:
		return filterResources(c.resources)
	case NamespaceNone:
		return filterResources(c.resources.nonNamespacedResources())
	default:
		return filterResources(c.resources.namespacedResources())
	}
}

func (c *client) filterResources(resources resourcesMap,
	includeAll bool, includedGks, excludedGks []schema.GroupKind) resourcesMap {
	filtered := make(resourcesMap)
	for gr, gvk := range resources {
		if len(includedGks) > 0 {
			if slices.Contains(includedGks, gvk.GroupKind()) {
				filtered[gr] = gvk
			}
			continue
		}

		// We can continue only when asking for including all: we will still
		// check on excluded.
		if !includeAll {
			continue
		}

		if len(excludedGks) > 0 {
			if !slices.Contains(excludedGks, gvk.GroupKind()) {
				filtered[gr] = gvk
			}
			continue
		}

		// We got this far: no filters, include everything.
		filtered[gr] = gvk
	}
	return filtered
}

// listBulk lists all objects of the resources in the given namespace.
// The loading happens in parallel. If any of the resources fails to load,
// we return an error. We return the first error that occurred.
func (c *client) listBulk(ctx context.Context, ns string, resources []schema.GroupVersionResource) ([]*unstructured.Unstructured, error) {
	if len(resources) == 0 {
		return nil, nil
	}
	resultsChan := make(chan []*unstructured.Unstructured)
	doneChan := make(chan struct{})
	wg := sync.WaitGroup{}

	var out []*unstructured.Unstructured
	go func() {
		for res := range resultsChan {
			out = append(out, res...)
		}
		close(doneChan)
	}()

	klog.V(3).InfoS("starting to query resources", "count", len(resources))
	var errResult error

	for _, resource := range resources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := c.list(ctx, resource, ns)
			if err != nil {
				// We only return one error.
				errResult = fmt.Errorf("listing resources failed (%s): %w", resource, err)
				return
			}
			resultsChan <- res
		}()
	}

	wg.Wait()
	close(resultsChan)
	<-doneChan

	klog.V(3).InfoS("query results", "objects", len(out), "error", errResult)
	return out, errResult
}

func (c *client) list(ctx context.Context, resource schema.GroupVersionResource, ns string) ([]*unstructured.Unstructured, error) {
	var out []*unstructured.Unstructured

	var next string

	for {
		var intf dynamicclient.ResourceInterface
		nintf := c.dynamic.Resource(resource)
		if ns != "" && ns != NamespaceAll {
			intf = nintf.Namespace(ns)
		} else {
			intf = nintf
		}
		resp, err := intf.List(ctx, metav1.ListOptions{
			Limit:    250,
			Continue: next,
		})
		if err != nil {
			return nil, fmt.Errorf("listing resources failed (%s): %w", resource, err)
		}

		for _, item := range resp.Items {
			out = append(out, &item)
		}

		next = resp.GetContinue()
		if next == "" {
			break
		}
	}
	return out, nil
}

func (c *client) get(ctx context.Context, obj *status.Object) (*unstructured.Unstructured, error) {
	mapping, err := c.mapper.RESTMapping(obj.GroupVersionKind().GroupKind())
	if err != nil {
		return nil, fmt.Errorf("failed to map object: %w", err)
	}

	unst, err := c.dynamic.Resource(mapping.Resource).
		Namespace(obj.GetNamespace()).
		Get(ctx, obj.GetName(), metav1.GetOptions{})

	if err != nil {
		return nil, err
	}

	return unst, nil
}

func (c *client) podLogs(ctx context.Context, obj *status.Object, container string, tailLines int64) ([]byte, error) {
	opts := &corev1.PodLogOptions{
		Container: container,
		Follow:    false,
		Previous:  false,
		TailLines: &tailLines,
	}

	return c.corev1client.Pods(obj.Namespace).GetLogs(obj.Name, opts).DoRaw(ctx)
}

func buildDynamicClient(c *rest.Config) (*dynamicclient.DynamicClient, error) {
	c = rest.CopyConfig(c)

	// We need higher limits for bulk operations to avoid slowing down too soon.
	c.WarningHandler = rest.NoWarnings{}
	c.QPS = 150
	c.Burst = 150
	dynamicClient, err := dynamicclient.NewForConfig(c)
	if err != nil {
		return nil, err
	}
	return dynamicClient, nil
}

type groupVersionKindNamespaced struct {
	schema.GroupVersionKind
	namespaced bool
}

// resourcesMap is a map for mapping a groupResource to groupVersionKind
// which also has a flag whether it is a namespaced resource or not
type resourcesMap map[schema.GroupResource]groupVersionKindNamespaced

func (r resourcesMap) namespacedResources() resourcesMap {
	filtered := make(resourcesMap, len(r))
	for k, v := range r {
		if v.namespaced {
			filtered[k] = v
		}
	}
	return filtered
}

func (r resourcesMap) nonNamespacedResources() resourcesMap {
	filtered := make(resourcesMap, len(r))
	for k, v := range r {
		if !v.namespaced {
			filtered[k] = v
		}
	}
	return filtered
}

func (r resourcesMap) toSlice() []schema.GroupVersionResource {
	var s []schema.GroupVersionResource
	for k, v := range r {
		s = append(s, schema.GroupVersionResource{
			Group:    k.Group,
			Resource: k.Resource,
			Version:  v.Version,
		})
	}
	return s
}
