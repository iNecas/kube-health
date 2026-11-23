package eval

import (
	"context"
	"errors"
	"testing"

	"github.com/rhobs/kube-health/pkg/status"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type appearingLoader struct {
	Loader
	current *status.Object
}

func (l *appearingLoader) Get(_ context.Context, obj *status.Object) (*status.Object, error) {
	if l.current == nil {
		return nil, errors.New("not found")
	}
	if obj.Name != l.current.Name {
		return nil, errors.New("wrong name")
	}
	return l.current, nil
}

type healthyAnalyzer struct{}

func (healthyAnalyzer) Supports(*status.Object) bool { return true }
func (healthyAnalyzer) Analyze(_ context.Context, obj *status.Object) status.ObjectStatus {
	return status.OkStatus(obj, nil)
}

func TestEvalMissingThenRecreated(t *testing.T) {
	makeObject := func(uid string) *status.Object {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
		u.SetNamespace("default")
		u.SetName("foo")
		u.SetUID(types.UID(uid))
		obj, err := status.NewObjectFromUnstructured(u)
		if err != nil {
			t.Fatal(err)
		}
		return obj
	}
	loader := &appearingLoader{}
	evaluator := NewEvaluator([]AnalyzerInit{func(*Evaluator) Analyzer { return healthyAnalyzer{} }}, loader)
	placeholder := makeObject("")
	ctx := context.Background()
	if got := evaluator.Eval(ctx, placeholder).Status().Result; got != status.Unknown {
		t.Fatalf("missing: got %v", got)
	}
	loader.current = makeObject("new-uid")
	evaluator.Reset()
	got := evaluator.Eval(ctx, placeholder)
	if got.Status().Result != status.Ok || got.Object.UID != loader.current.UID {
		t.Fatalf("recreated: got %+v", got)
	}
	if _, found := evaluator.cache[loader.current.UID]; !found {
		t.Fatal("recreated object was not cached under its new UID")
	}
}
