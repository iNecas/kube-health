package analyze

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

var (
	gkPvc = schema.GroupKind{Group: "", Kind: "PersistentVolumeClaim"}
)

type PVCAnalyzer struct {
	e *eval.Evaluator
}

func (_ PVCAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkPvc
}

func (a PVCAnalyzer) Analyze(obj *status.Object) status.ObjectStatus {
	phase, _, _ := unstructured.NestedString(obj.Unstructured.Object, "status", "phase")
	var conditions []status.ConditionStatus
	if phase != "Bound" {
		conditions = append(conditions,
			SyntheticConditionProgressing("NotBound", phase, "PVC is not bound."))
	} else {
		conditions = append(conditions,
			SyntheticConditionOk("Bound", "PVC is bound."))
	}

	return AggregateResult(obj, nil, conditions)
}

func init() {
	Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return PVCAnalyzer{e: e}
	})
}
