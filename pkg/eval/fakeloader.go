package eval

import (
	"context"
	"fmt"
	"time"

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

	// baseTime is used to replace the datetime data
	// Given we focus mainly on relative values, we want the relative time
	// to be stable so that we can use the values in the tests. By defualt, it
	// updates to Now()-24h.
	baseTime time.Time
}

func NewFakeLoader() *FakeLoader {
	return &FakeLoader{
		cache:    make(map[types.UID]*status.Object),
		nsCache:  make(map[string]*nsCache),
		podLogs:  make(map[string]string),
		baseTime: time.Now().UTC().Add(-24 * time.Hour),
	}
}

func (l *FakeLoader) Load(ctx context.Context, ns string, matcher GroupKindMatcher, exclude []schema.GroupKind) ([]*status.Object, error) {
	var ret []*status.Object
	nsCache := l.getNsCache(ns)
	for gk, objects := range nsCache.objects {
		if matcher.Match(gk) {
			ret = append(ret, objects...)
		}
	}
	return ret, nil
}

func (l *FakeLoader) ResourceToKind(gr schema.GroupResource) schema.GroupVersionKind {
	// noop
	return schema.GroupVersionKind{}
}

func (l *FakeLoader) LoadResource(ctx context.Context, gr schema.GroupResource, namespace string, name string) ([]*status.Object, error) {
	// noop
	return []*status.Object{}, nil
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
		updateTime(uo, l.baseTime)
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

func updateTime(d unstructured.Unstructured, t time.Time) {
	walkMap(d.Object, func(k string, v interface{}) interface{} {
		switch v := v.(type) {
		case string:
			_, err := time.Parse(time.RFC3339, v)
			if err != nil {
				// The string was not time-related, keep unchanged.
				return v
			}
			return t.Format(time.RFC3339)
		default:
			return v
		}
	})
}

func walkMap(m map[string]interface{}, fn func(k string, v interface{}) interface{}) {
	for k, v := range m {
		switch v := v.(type) {
		case map[string]interface{}:
			walkMap(v, fn)
		case []interface{}:
			walkSlice(v, fn)
		default:
			m[k] = fn(k, v)
		}
	}
}

func walkSlice(s []interface{}, fn func(k string, v interface{}) interface{}) {
	for i, v := range s {
		switch v := v.(type) {
		case map[string]interface{}:
			walkMap(v, fn)
		case []interface{}:
			walkSlice(v, fn)
		default:
			// We don't pass anything as keys in lists, as we don't use it right now.
			s[i] = fn("", v)
		}
	}
}
