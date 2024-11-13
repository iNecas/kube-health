package redhat

// mco.go implements an analyzer for MultiClusterObservability objects.
import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

var (
	gkMCO = schema.GroupKind{Group: "observability.open-cluster-management.io",
		Kind: "MultiClusterObservability"}
	mcoNs = "open-cluster-management-observability"
)

type MCOAnalyzer struct {
	e *eval.Evaluator
}

func (_ MCOAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkMCO
}

func (a MCOAnalyzer) Analyze(obj *status.Object) status.ObjectStatus {
	// We need to specify the namespace explicitly, as the MCO object
	// is namespace-less.
	ds := analyze.GenericOwnerQuerySpec(obj)
	ds.NamespaceOverride = &mcoNs
	subStatuses, err := a.e.EvalQuery(ds, nil)

	conditions, err := analyze.AnalyzeObjectConditions(obj, analyze.DefaultConditionAnalyzers)

	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	return analyze.AggregateResult(obj, subStatuses, conditions)
}

func init() {
	analyze.Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return MCOAnalyzer{e: e}
	})
}
