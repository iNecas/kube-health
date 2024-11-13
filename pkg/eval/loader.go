package eval

import (
	"context"
	"slices"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/inecas/kube-health/pkg/status"
)

// nsCache holds objects loaded from a single namespace, the matcher to
// load the data and tracks deed for refilling the data when the matcher
// changes.
type nsCache struct {
	objects     map[schema.GroupKind][]*status.Object
	matcher     GroupKindMatcher
	needsRefill bool
}

func newNsCache() *nsCache {
	return &nsCache{
		objects: make(map[schema.GroupKind][]*status.Object),
	}
}

// append adds an object to the cache.
func (n *nsCache) append(obj *status.Object) {
	gk := obj.GroupVersionKind().GroupKind()
	n.objects[gk] = append(n.objects[gk], obj)
}

func (n *nsCache) get(gk schema.GroupKind) []*status.Object {
	if gk.Kind == "" {
		return n.getAll()
	}
	return n.objects[gk]
}

func (n *nsCache) getAll() []*status.Object {
	var ret []*status.Object
	for _, objs := range n.objects {
		ret = append(ret, objs...)
	}
	return ret
}

// updateMatcher updates the matcher and returns true if the matcher has changed.
func (n *nsCache) updateMatcher(gk GroupKindMatcher) bool {
	matcher := n.matcher.Merge(gk)
	if !matcher.Equal(n.matcher) {
		n.matcher = matcher
		n.needsRefill = true
		return true
	}
	return false
}

// Loader is responsible for loading and caching the objects from the cluster.
// It also allows finding objects based on their ownership relations.
type Loader struct {
	client *client
	mapper meta.RESTMapper

	cache              map[types.UID]*status.Object         // mapping of UID to the object
	nsCache            map[string]*nsCache                  // mapping of namespace to its cache
	ownership          map[types.UID]map[types.UID]struct{} // mapping of owner UID to the set of owned UIDs
	ownershipRefreshNs []string                             // indicator to refresh the ownership relations (after a change)
}

func NewLoader(config RESTClientGetter) (*Loader, error) {
	client, err := newGenericClient(config)
	if err != nil {
		return nil, err
	}

	return &Loader{client: client,
		cache:     make(map[types.UID]*status.Object),
		ownership: make(map[types.UID]map[types.UID]struct{}),
		nsCache:   make(map[string]*nsCache),
	}, nil
}

func (l *Loader) extendNsKinds(ns string, gk GroupKindMatcher) {
	nsCache := l.getNsCache(ns)

	nsCache.updateMatcher(gk)
}

// Load loads the objects specified by the query. It uses the cache to
// avoid loading the same objects multiple times.
func (l *Loader) Load(ctx context.Context, q QuerySpec) ([]*status.Object, error) {
	l.preloadQuery(ctx, q)
	objects := q.Eval(l)
	return objects, nil
}

// preloadQuery loads the objects that could match the query spec.
func (l *Loader) preloadQuery(ctx context.Context, query QuerySpec) {
	if l.getNsCache(query.Namespace()).updateMatcher(query.GroupKindMatcher()) {
		l.loadNamespace(ctx, query.Namespace())
	}
}

// Get returns the updated version of the object. If the object is not
// in the cache, it loads it from the cluster first.
func (l *Loader) Get(ctx context.Context, obj *status.Object) (*status.Object, error) {
	ret, found := l.cache[obj.GetUID()]

	if !found {
		unst, err := l.client.get(ctx, obj)
		if err != nil {
			return nil, err
		}

		ret, err = l.injest(unst)
		if err != nil {
			return nil, err
		}
		l.cache[obj.GetUID()] = ret
	}

	return ret, nil

}

// Filter returns the objects from the cache that match the matcher.
// It expects the objects to be in the cache. This methods is intended
// to run during evaluation of the Load method in the following order:
//
//  1. The Load method runs preloadQuery to fill in the cache.
//  2. The Load method runs Eval on the query spec to get the objects.
//  3. The Eval method runs Filter to get the objects from the cache.
//
// We need to run the preloadQuery before the Eval method to support
// searching for objects based on the ownership relations.
func (l *Loader) Filter(ns string, matcher GroupKindMatcher) []*status.Object {
	ret := []*status.Object{}
	for gk, objects := range l.getNsCache(ns).objects {
		if matcher.Match(gk) {
			ret = append(ret, objects...)
		}
	}
	return ret
}

func (l *Loader) reset() {
	clear(l.cache)
	clear(l.ownership)
	clear(l.nsCache)
	clear(l.ownershipRefreshNs)
}

func (l *Loader) loadNamespace(ctx context.Context, ns string) error {
	var gksLoaded []schema.GroupKind
	nsCache := l.getNsCache(ns)
	for gk, _ := range nsCache.objects {
		gksLoaded = append(gksLoaded, gk)
	}

	var err error
	var unsts []*unstructured.Unstructured

	unsts, err = l.client.listWithMatcher(ctx, ns, nsCache.matcher, gksLoaded)

	if err != nil {
		return err
	}

	nsCache.needsRefill = false

	for _, unst := range unsts {
		l.injest(unst)
	}

	if !slices.Contains(l.ownershipRefreshNs, ns) {
		l.ownershipRefreshNs = append(l.ownershipRefreshNs, ns)
	}

	return nil
}

func (l *Loader) injest(unst *unstructured.Unstructured) (*status.Object, error) {
	obj, err := status.NewObjectFromUnstructured(unst)
	if err != nil {
		return nil, err
	}

	l.cache[obj.GetUID()] = obj

	l.getNsCache(obj.GetNamespace()).append(obj)

	return obj, nil
}

func (l *Loader) getNsCache(ns string) *nsCache {
	if l.nsCache[ns] == nil {
		l.nsCache[ns] = newNsCache()
	}
	return l.nsCache[ns]
}

func (l *Loader) filterOwnedBy(owner *status.Object, candidates []*status.Object) []*status.Object {
	// Ensure the ownership relations are up-to-date.
	l.refreshOwnership()

	var ret []*status.Object
	childUIDs := l.ownership[owner.GetUID()]
	for _, cand := range candidates {
		if _, present := childUIDs[cand.GetUID()]; present {
			ret = append(ret, cand)
		}
	}

	return ret
}

func (l *Loader) refreshOwnership() {
	for _, ns := range l.ownershipRefreshNs {
		for _, obj := range l.getNsCache(ns).getAll() {
			for _, ownerRef := range obj.GetOwnerReferences() {
				if l.ownership[ownerRef.UID] == nil {
					l.ownership[ownerRef.UID] = make(map[types.UID]struct{})
				}
				l.ownership[ownerRef.UID][obj.GetUID()] = struct{}{}
			}
		}
	}
	l.ownershipRefreshNs = nil
}
