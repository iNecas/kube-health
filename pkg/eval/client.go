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
	dynamic        dynamicclient.Interface
	mapper         meta.RESTMapper
	config         *rest.Config
	corev1client   corev1client.CoreV1Interface
	nsResources    []schema.GroupVersionResource
	nonNsResources []schema.GroupVersionResource
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
			res := schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: apiRes.Name,
			}

			if apiRes.Namespaced {
				c.nsResources = append(c.nsResources, res)
			} else {
				c.nonNsResources = append(c.nonNsResources, res)
			}
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
	var resources []schema.GroupVersionResource
	if ns != "" {
		resources = c.nsResources
	} else {
		resources = c.nonNsResources
	}

	resources = c.evalGroupKindMatcher(resources, matcher)

	if len(excludedGks) > 0 {
		resources = c.filterResources(resources, true, nil, excludedGks)
	}

	return c.listBulk(ctx, ns, resources)
}

func (c *client) filterResources(resources []schema.GroupVersionResource,
	includeAll bool, includedGks, excludedGks []schema.GroupKind) []schema.GroupVersionResource {
	var filtered []schema.GroupVersionResource
	for _, res := range resources {
		resGk, err := c.mapper.KindFor(res)
		if err != nil {
			klog.V(2).Infof("failed to get kind for resource: %v", err)
			continue
		}

		if len(includedGks) > 0 {
			if slices.Contains(includedGks, resGk.GroupKind()) {
				filtered = append(filtered, res)
			}
			continue
		}

		// We can continue only when asking for including all: we will still
		// check on excluded.
		if !includeAll {
			continue
		}

		if len(excludedGks) > 0 {
			if !slices.Contains(excludedGks, resGk.GroupKind()) {
				filtered = append(filtered, res)
			}
			continue
		}

		// We got this far: no filters, include everything.
		filtered = append(filtered, res)
	}
	return filtered
}

func (c *client) evalGroupKindMatcher(resources []schema.GroupVersionResource,
	matcher GroupKindMatcher) []schema.GroupVersionResource {
	return c.filterResources(resources, matcher.IncludeAll, matcher.IncludedKinds, matcher.ExcludedKinds)
}

// listBulk lists all objects of the resources in the given namespace.
// The loading happens in parallel. If any of the resources fails to load,
// we return an error. We return the first error that occurred.
func (c *client) listBulk(ctx context.Context, ns string, resources []schema.GroupVersionResource) ([]*unstructured.Unstructured, error) {
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

	klog.V(2).InfoS("starting to query resources", "count", len(resources))
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

	klog.V(2).Infof("query results", "objects", len(out), "error", errResult)
	return out, errResult
}

func (c *client) list(ctx context.Context, resource schema.GroupVersionResource, ns string) ([]*unstructured.Unstructured, error) {
	var out []*unstructured.Unstructured

	var next string

	for {
		var intf dynamicclient.ResourceInterface
		nintf := c.dynamic.Resource(resource)
		if ns != "" {
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

func (c *client) podLogs(obj *status.Object, container string, tailLines int64) ([]byte, error) {
	opts := &corev1.PodLogOptions{
		Container: container,
		Follow:    false,
		Previous:  false,
		TailLines: &tailLines,
	}

	// TODO: pass context
	return c.corev1client.Pods(obj.Namespace).GetLogs(obj.Name, opts).DoRaw(context.TODO())
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
