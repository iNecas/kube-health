package analyze

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

var (
	gkService = schema.GroupKind{Group: "", Kind: "Service"}
)

type ServiceAnalyzer struct {
	e *eval.Evaluator
}

func (_ ServiceAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkService
}

func (a ServiceAnalyzer) Analyze(obj *status.Object) status.ObjectStatus {
	subStatuses, err := a.e.EvalQuery(
		eval.NewSelectorLabelEqualityQuerySpec(obj, gkPod), PodAnalyzer{e: a.e})

	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	return AggregateResult(obj, subStatuses, nil)
}

func init() {
	Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return ServiceAnalyzer{e: e}
	})
}
