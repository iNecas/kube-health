package eval

import (
	"encoding/json"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	"github.com/inecas/kube-health/pkg/status"
)

// QuerySpec is a specification of a query for objects.
//
// It's a generic interface Loader understands to load objects. The methods
// are used for preloading the objects before the actual evaluation.
type QuerySpec interface {
	// GroupKindMatcher specifies the kinds of objects to load.
	GroupKindMatcher() GroupKindMatcher

	// Namespace specifies the namespace of the objects to load.
	// Together with the GroupKindMatcher, it's used to preload the objects
	// into the Loader's cache.
	Namespace() string

	// Eval evaluates the query and returns the objects.
	// If applicable, the loader is already preloaded with the objects based
	// on the GroupKindMatcher and Namespace. It's still the repsonsibility
	// of the Eval method to do the final filtering.
	Eval(l *Loader) []*status.Object
}

// GroupKindMatcher allows specifying a set of kinds to match.
type GroupKindMatcher struct {
	// IncludeAll specifies whether all kinds should be included.
	// If true, the IncludedKinds are ignored.
	IncludeAll bool

	// IncludedKinds specifies the kinds to include. It's mutually exclusive
	// with IncludeAll.
	IncludedKinds []schema.GroupKind

	// ExcludedKinds specifies the kinds to exclude. It's only used with
	// IncludeAll.
	ExcludedKinds []schema.GroupKind
}

// NewGroupKindMatcherSingle returns a new GroupKindMatcher that matches only
// a single kind.
func NewGroupKindMatcherSingle(kind schema.GroupKind) GroupKindMatcher {
	return GroupKindMatcher{
		IncludedKinds: []schema.GroupKind{kind},
	}
}

// Merge returns a new GroupKindMatcher that matches the union of the kinds
// matched by the receiver and the other matcher.
func (m GroupKindMatcher) Merge(other GroupKindMatcher) GroupKindMatcher {
	includeAll := false
	includedKinds := []schema.GroupKind{}

	// If any of the matchers includes all kinds, the result will include all kinds.
	if !m.IncludeAll && !other.IncludeAll {
		includedKinds = append(includedKinds, m.IncludedKinds...)
		includedKinds = append(includedKinds, other.IncludedKinds...)
	} else {
		includeAll = true
	}

	excludeInputs := [][]schema.GroupKind{}
	if m.IncludeAll {
		excludeInputs = append(excludeInputs, m.ExcludedKinds)
	}
	if other.IncludeAll {
		excludeInputs = append(excludeInputs, other.ExcludedKinds)
	}
	// The result exclude list should include all kinds excluded by
	// both input matchers.
	excludedKinds := intersect(excludeInputs)

	return GroupKindMatcher{
		IncludeAll:    includeAll,
		IncludedKinds: includedKinds,
		ExcludedKinds: excludedKinds,
	}
}

func (m GroupKindMatcher) Equal(other GroupKindMatcher) bool {
	if len(m.IncludedKinds) != len(other.IncludedKinds) ||
		len(m.ExcludedKinds) != len(other.ExcludedKinds) {
		return false
	}

	if m.IncludeAll != other.IncludeAll {
		return false
	}

	includedInterset := intersect2(m.IncludedKinds, other.IncludedKinds)

	if len(includedInterset) != len(m.IncludedKinds) {
		return false
	}

	excludedInterset := intersect2(m.ExcludedKinds, other.ExcludedKinds)

	if len(excludedInterset) != len(m.ExcludedKinds) {
		return false
	}

	return true
}

func (m GroupKindMatcher) Match(gk schema.GroupKind) bool {
	if len(m.IncludedKinds) > 0 {
		return slices.Contains(m.IncludedKinds, gk)
	}

	if !m.IncludeAll {
		return false
	}

	if len(m.ExcludedKinds) > 0 {
		return !slices.Contains(m.ExcludedKinds, gk)
	}

	return true
}

// intersect returns the intersection of the sets.
//
// If the input has only one set, it returns that set.
// If the input has more than two sets, it panics.
func intersect[T comparable](sets [][]T) []T {
	switch len(sets) {
	case 0:
		return []T{}
	case 1:
		return sets[0]
	case 2:
		return intersect2(sets[0], sets[1])
	default:
		panic("intersect only supports up to two inputs")
	}
}

// intersect2 returns the intersection of two sets.
func intersect2[T comparable](a, b []T) []T {
	m := make(map[T]struct{})
	for _, kind := range a {
		m[kind] = struct{}{}
	}

	intersection := []T{}
	for _, kind := range b {
		if _, present := m[kind]; present {
			intersection = append(intersection, kind)
		}
	}

	return intersection
}

// OwnerQuerySpec is a query that returns objects owned by the specified object.
type OwnerQuerySpec struct {
	Object *status.Object
	GK     GroupKindMatcher
	// NamespaceOverride specifies the namespace of the child object.
	// If nil, the namespace of the Object is used.
	NamespaceOverride *string
}

func (qs OwnerQuerySpec) Namespace() string {
	if qs.NamespaceOverride != nil {
		return *qs.NamespaceOverride
	}
	return qs.Object.GetNamespace()
}

func (qs OwnerQuerySpec) GroupKindMatcher() GroupKindMatcher {
	return qs.GK
}

func (qs OwnerQuerySpec) Eval(l *Loader) []*status.Object {
	candidates := l.Filter(qs.Namespace(), qs.GK)
	return l.filterOwnedBy(qs.Object, candidates)
}

// labelSelectorMode specifies the mode of the label selector.
// Different kinds use different modes. See
// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors
// for more information.
type labelSelectorMode int

const (
	// labelSelectorSetBased is use more complex label selector, used
	// in newer resources, such as ReplicaSet or deployment.
	labelSelectorSetBased labelSelectorMode = iota

	// labelSelectorEqualityBased is a simple selector form.
	// Used for example in services.
	labelSelectorEqualityBased
)

// LabelQuerySpec is a query that returns objects based on the labels selector.
// The namespace is taken from the Object.
type LabelQuerySpec struct {
	Object   *status.Object
	GK       GroupKindMatcher
	Selector labels.Selector
}

func (qs LabelQuerySpec) GroupKindMatcher() GroupKindMatcher {
	return qs.GK
}

func (qs LabelQuerySpec) Namespace() string {
	return qs.Object.GetNamespace()
}

func (qs LabelQuerySpec) Eval(l *Loader) []*status.Object {
	candidates := l.Filter(qs.Object.GetNamespace(), qs.GK)
	var ret []*status.Object

	for _, cand := range candidates {
		if qs.Selector.Matches(labels.Set(cand.GetLabels())) {
			ret = append(ret, cand)
		}
	}

	return ret
}

func NewSelectorLabelQuerySpec(obj *status.Object, gk schema.GroupKind) LabelQuerySpec {
	return LabelQuerySpec{
		Object:   obj,
		GK:       NewGroupKindMatcherSingle(gk),
		Selector: buildSelectorOrNil(obj, labelSelectorSetBased, "spec", "selector")}
}

func NewSelectorLabelEqualityQuerySpec(obj *status.Object, gk schema.GroupKind) LabelQuerySpec {
	return LabelQuerySpec{
		Object:   obj,
		GK:       NewGroupKindMatcherSingle(gk),
		Selector: buildSelectorOrNil(obj, labelSelectorEqualityBased, "spec", "selector"),
	}
}

func buildSelectorOrNil(obj *status.Object, mode labelSelectorMode, path ...string) labels.Selector {
	selector, err := buildSelector(obj, mode, path...)
	if err != nil {
		klog.V(2).ErrorS(err, "Error building selector")
		return nil
	}
	return selector
}

func buildSelector(obj *status.Object, mode labelSelectorMode, path ...string) (labels.Selector, error) {
	selector, found, err := unstructured.NestedMap(obj.Unstructured.Object, path...)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	if mode == labelSelectorEqualityBased {
		conv := labels.Set{}
		for k, v := range selector {
			conv[k] = v.(string)
		}
		return labels.SelectorFromSet(conv), nil
	} else {
		bytes, err := json.Marshal(selector)
		if err != nil {
			return nil, err
		}
		var s metav1.LabelSelector
		err = json.Unmarshal(bytes, &s)
		if err != nil {
			return nil, err
		}
		return metav1.LabelSelectorAsSelector(&s)
	}
}

// RefQuerySpec is a query that returns objects referenced by the specified object.
// It assumes the reference to be in the same namespace.
type RefQuerySpec struct {
	Object    *status.Object
	RefObject corev1.ObjectReference
}

func (qs RefQuerySpec) GroupKindMatcher() GroupKindMatcher {
	return GroupKindMatcher{
		IncludedKinds: []schema.GroupKind{
			qs.RefObject.GroupVersionKind().GroupKind(),
		},
	}
}

func (qs RefQuerySpec) Namespace() string {
	return qs.Object.GetNamespace()
}

func (qs RefQuerySpec) Eval(l *Loader) []*status.Object {
	candidates := l.Filter(qs.Object.GetNamespace(), qs.GroupKindMatcher())
	var ret []*status.Object

	for _, cand := range candidates {
		if qs.RefObject.UID == cand.GetUID() ||
			qs.RefObject.Name == cand.GetName() {
			ret = append(ret, cand)
		}
	}

	return ret
}

// PodLogQuerySpec is a query that returns logs of the specified pod.
type PodLogQuerySpec struct {
	Object    *status.Object
	Container string
}

func (qs PodLogQuerySpec) GroupKindMatcher() GroupKindMatcher {
	// Empty matcher: we don't want load any objects implicitly.
	return GroupKindMatcher{}
}

func (qs PodLogQuerySpec) Namespace() string {
	return qs.Object.GetNamespace()
}

func (qs PodLogQuerySpec) Eval(l *Loader) []*status.Object {
	data := make(map[string]interface{}, 1)
	logs, err := l.client.podLogs(qs.Object, qs.Container, 5)
	if err != nil {
		klog.V(4).ErrorS(err, "Failed to get logs", "object", qs.Object)
	} else {
		data["log"] = string(logs)
	}

	// Synthetic object to contain logs.
	logobj := &status.Object{
		TypeMeta: metav1.TypeMeta{
			Kind: "Log",
			// Just to differentiate it from any other type.
			APIVersion: "kube-health.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: qs.Container,
		},
		Unstructured: &unstructured.Unstructured{Object: map[string]interface{}(data)},
	}

	return []*status.Object{logobj}
}
