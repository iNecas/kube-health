package eval

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/inecas/kube-health/pkg/status"
)

// FakeLoader mocks data to be loaded for the evaluator.
// It's used in tests.
type FakeLoader struct {
	cache   map[types.UID]*status.Object
	nsCache map[string]*nsCache
	podLogs map[string]string
}

func NewFakeLoader() *FakeLoader {
	return &FakeLoader{
		cache:   make(map[types.UID]*status.Object),
		nsCache: make(map[string]*nsCache),
		podLogs: make(map[string]string),
	}
}

func (l *FakeLoader) Load(ctx context.Context, ns string, matcher GroupKindMatcher, exclude []schema.GroupKind) ([]*status.Object, error) {
	var ret []*status.Object
	fmt.Printf("ns = %#v, matcher: %#v, exclude %#v\n", ns, matcher, exclude)
	return ret, nil
}

func (l *FakeLoader) LoadPodLogs(ctx context.Context, obj *status.Object, container string, tailLines int64) ([]byte, error) {
	logs := l.podLogs[fmt.Sprintf("%s-%s-%s", obj.Namespace, obj.Name, container)]
	return []byte(logs), nil
}

func (l *FakeLoader) Get(ctx context.Context, obj *status.Object) (*status.Object, error) {
	obj, found := l.cache[obj.UID]
	if !found {
		return nil, fmt.Errorf("Object %v not found", obj)
	}

	return obj, nil
}

func (l *FakeLoader) Register(objects ...unstructured.Unstructured) ([]*status.Object, error) {
	var ret []*status.Object
	for _, uo := range objects {
		nsCache := l.getNsCache(uo.GetNamespace())
		o, err := status.NewObjectFromUnstructured(&uo)
		if err != nil {
			return nil, err
		}

		if o.UID == "" {
			return nil, fmt.Errorf("Object %#v has no UID provided", uo)
		}

		l.cache[o.UID] = o
		nsCache.append(o)
		ret = append(ret, o)
	}
	return ret, nil
}

func (f *FakeLoader) RegisterPodLogs(namespace, pod, container, logs string) {
	f.podLogs[fmt.Sprintf("%s-%s-%s", namespace, pod, container)] = logs
}

func (l *FakeLoader) getNsCache(ns string) *nsCache {
	if l.nsCache[ns] == nil {
		l.nsCache[ns] = newNsCache()
	}
	return l.nsCache[ns]
}
