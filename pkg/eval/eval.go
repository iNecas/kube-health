package eval

import (
	"context"
	"slices"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/inecas/kube-health/pkg/status"
)

// Analyzer calculates status for the object.
type Analyzer interface {
	Analyze(obj *status.Object) status.ObjectStatus
	// Supports should return true if the particular analyzer supports
	// the given resource.
	//
	// It's used when searching the appropriate analyzer in the register.
	Supports(obj *status.Object) bool
}

// AnalyzerInit is a function that initializes an Analyzer and can
// optionally pass an Evaluator reference to it.
type AnalyzerInit func(*Evaluator) Analyzer

// Interface to be implemented to support the evaluator.
type Loader interface {
	// Get loads a refreshed version of the objects.
	// It might be cached still since the last Reset() call.
	Get(context.Context, *status.Object) (*status.Object, error)

	// Load evaluates the query based on the backend data.
	Load(c context.Context, ns string, gkm GroupKindMatcher, exclude []schema.GroupKind) ([]*status.Object, error)

	// Load evaluates the query based on the backend data.
	LoadPodLogs(c context.Context, obj *status.Object, container string, tailLines int64) ([]byte, error)
}

// Evaluator is the entry structure for the status evaluation cycle.
//
// It peformes the following steps:
//   - Loading fresh data for the object (though the Loader struct).
//   - Finding an appropriate Analyzer for the object.
//   - Evaluating the Analyzer on the object.
type Evaluator struct {
	analyzers      []Analyzer
	loader         Loader
	analyzersCache map[types.UID]Analyzer
	ctx            context.Context

	cache              map[types.UID]*status.Object         // mapping of UID to the object
	nsCache            map[string]*nsCache                  // mapping of namespace to its cache
	ownership          map[types.UID]map[types.UID]struct{} // mapping of owner UID to the set of owned UIDs
	ownershipRefreshNs []string                             // indicator to refresh the ownership relations (after a change)
}

// NewEvaluator creates a new Evaluator instance.
func NewEvaluator(ctx context.Context, analyzerInits []AnalyzerInit, loader Loader) *Evaluator {
	evaluator := &Evaluator{
		loader:         loader,
		analyzersCache: make(map[types.UID]Analyzer),
		ctx:            ctx,

		cache:     make(map[types.UID]*status.Object),
		ownership: make(map[types.UID]map[types.UID]struct{}),
		nsCache:   make(map[string]*nsCache),
	}

	// Initialize the analyzers.
	analyzers := make([]Analyzer, 0, len(analyzerInits))
	for _, init := range analyzerInits {
		analyzers = append(analyzers, init(evaluator))
	}
	evaluator.analyzers = analyzers
	return evaluator
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
func (e *Evaluator) Filter(ns string, matcher GroupKindMatcher) []*status.Object {
	ret := []*status.Object{}
	if ns == NamespaceAll {
		for ns := range e.nsCache {
			if ns != NamespaceAll { // prevent infinite recursion
				ret = append(ret, e.Filter(ns, matcher)...)
			}
		}
	} else {
		for gk, objects := range e.getNsCache(ns).objects {
			if matcher.Match(gk) {
				ret = append(ret, objects...)
			}
		}
	}
	return ret
}

func (e *Evaluator) Reset() {
	clear(e.cache)
	clear(e.ownership)
	clear(e.nsCache)
	clear(e.ownershipRefreshNs)
}

// Evaluates the status of the object. It gets the most recent version
// of the object and runs the appropriate analyzer on it.
func (e *Evaluator) Eval(obj *status.Object) status.ObjectStatus {
	analyzer := e.findAnalyzer(obj)

	var updatedObj *status.Object

	updatedObj, found := e.cache[obj.UID]

	if !found {
		var err error
		updatedObj, err = e.loader.Get(e.ctx, obj)
		if err != nil {
			return status.UnknownStatusWithError(obj, err)
		}
		e.updateCache(obj)
	}

	return analyzer.Analyze(updatedObj)
}

// EvalQuery loads the objects specified by the query and runs the analyzer.
// If the analyzer is not provided, it tries to find the appropriate one
// in the register.
func (e *Evaluator) EvalQuery(q QuerySpec, analyzer Analyzer) ([]status.ObjectStatus, error) {
	objects, err := e.Load(q)
	if err != nil {
		return nil, err
	}

	var ret []status.ObjectStatus
	for _, obj := range objects {
		a := analyzer
		if a == nil {
			a = e.findAnalyzer(obj)
		}
		ret = append(ret, a.Analyze(obj))
	}
	return ret, nil
}

// Load loads the objects specified by the query.
func (e *Evaluator) Load(q QuerySpec) ([]*status.Object, error) {
	if e.getNsCache(q.Namespace()).updateMatcher(q.GroupKindMatcher()) {
		e.loadNamespace(q.Namespace())
	}

	objects := q.Eval(e)
	return objects, nil
}

func (e *Evaluator) findAnalyzer(obj *status.Object) Analyzer {
	for _, analyzer := range e.analyzers {
		if analyzer.Supports(obj) {
			e.analyzersCache[obj.UID] = analyzer
			return analyzer
		}
	}
	return nil
}

func (e *Evaluator) getNsCache(ns string) *nsCache {
	if e.nsCache[ns] == nil {
		e.nsCache[ns] = newNsCache()
	}
	return e.nsCache[ns]
}

func (e *Evaluator) loadNamespace(ns string) error {
	var gksLoaded []schema.GroupKind
	nsCache := e.getNsCache(ns)
	for gk, _ := range nsCache.objects {
		gksLoaded = append(gksLoaded, gk)
	}

	var err error

	objs, err := e.loader.Load(e.ctx, ns, nsCache.matcher, gksLoaded)
	if err != nil {
		return err
	}

	nsCache.needsRefill = false

	touchedNs := make(map[string]struct{})

	for _, obj := range objs {
		if !e.updateCache(obj) {
			continue
		}

		touchedNs[obj.GetNamespace()] = struct{}{}

		// Inject only adds the object to it's home namespace. When we're loading
		// the NamespaceAll, we also mark the object as loaded here to avoid
		// loading it multiple times.
		if ns == NamespaceAll {
			nsCache.append(obj)
		}
	}

	// Mark namespaces that were affected after the load.
	// We can't use the original ns, as it might be the NamespaceAll placeholder.
	for ns := range touchedNs {
		if !slices.Contains(e.ownershipRefreshNs, ns) {
			e.ownershipRefreshNs = append(e.ownershipRefreshNs, ns)
		}
	}

	return nil
}

func (e *Evaluator) updateCache(obj *status.Object) bool {
	if _, found := e.cache[obj.UID]; found {
		return false
	}
	e.cache[obj.UID] = obj
	e.getNsCache(obj.GetNamespace()).append(obj)
	return true
}

func (e *Evaluator) filterOwnedBy(owner *status.Object, candidates []*status.Object) []*status.Object {
	// Ensure the ownership relations are up-to-date.
	e.refreshOwnership()

	var ret []*status.Object
	childUIDs := e.ownership[owner.GetUID()]
	for _, cand := range candidates {
		if _, present := childUIDs[cand.GetUID()]; present {
			ret = append(ret, cand)
		}
	}

	return ret
}

func (e *Evaluator) refreshOwnership() {
	for _, ns := range e.ownershipRefreshNs {
		for _, obj := range e.getNsCache(ns).getAll() {
			for _, ownerRef := range obj.GetOwnerReferences() {
				if e.ownership[ownerRef.UID] == nil {
					e.ownership[ownerRef.UID] = make(map[types.UID]struct{})
				}
				e.ownership[ownerRef.UID][obj.GetUID()] = struct{}{}
			}
		}
	}
	e.ownershipRefreshNs = nil
}

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
