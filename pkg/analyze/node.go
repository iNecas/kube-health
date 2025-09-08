package analyze

import (
	"context"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var gkNode = schema.GroupKind{Group: "", Kind: "Node"}

type NodeAnalyzer struct {
	e *eval.Evaluator
}

func (_ NodeAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkNode
}

func (a NodeAnalyzer) Analyze(ctx context.Context, obj *status.Object) status.ObjectStatus {
	conditions, err := AnalyzeObjectConditions(obj, DefaultConditionAnalyzers)
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	unschedulable, _, _ := unstructured.NestedBool(obj.Unstructured.Object, "spec", "unschedulable")
	if unschedulable {
		conditions = append(conditions, SyntheticConditionError("Unschedulable", "Unschedulable", "Node is marked as unschedulable"))
	}
	return AggregateResult(obj, nil, conditions)
}

func init() {
	Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return NodeAnalyzer{e: e}
	})
}
