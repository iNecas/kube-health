package analyze

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

// GenericAnalyzer is an analyzer is a generic implementation of an analyzer.
// It evaluates object conditions against conditionsAnalyzers. It also evaluates
// the sub-objects based on owner references.
type GenericAnalyzer struct {
	e                   *eval.Evaluator
	conditionsAnalyzers []ConditionAnalyzer
}

func (a *GenericAnalyzer) Supports(obj *status.Object) bool {
	return true
}

func (a *GenericAnalyzer) Analyze(ctx context.Context, obj *status.Object) status.ObjectStatus {
	subStatuses, err := a.e.EvalQuery(ctx, GenericOwnerQuerySpec(obj), nil)
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	_, hasstatus, _ := unstructured.NestedMap(obj.Unstructured.Object, "status")
	if !hasstatus && len(subStatuses) == 0 {
		// By default, objects without status are considered OK.
		return status.OkStatus(obj, subStatuses)
	}

	conditions := AnalyzeObservedGeneration(obj)

	conds, err := AnalyzeObjectConditions(obj, a.conditionsAnalyzers)
	if err != nil {
		err = fmt.Errorf("Error analyzing conditions: %w", err)
		return status.UnknownStatusWithError(obj, err)
	}

	conditions = append(conditions, conds...)

	return AggregateResult(obj, subStatuses, conditions)
}

func GenericOwnerQuerySpec(obj *status.Object) eval.OwnerQuerySpec {
	return eval.OwnerQuerySpec{
		Object: obj,
		GK: eval.GroupKindMatcher{
			IncludeAll:    true,
			ExcludedKinds: Register.ignoredKinds,
		},
	}
}

// GenericConditionAnalyzers adds additional conditions based on values
// of generation and observedGeneration fields.
func AnalyzeObservedGeneration(obj *status.Object) []status.ConditionStatus {
	observedGeneration, found, err := unstructured.NestedInt64(obj.Unstructured.Object, "status", "observedGeneration")
	if err != nil {
		return []status.ConditionStatus{ConditionStatusUnknownWithError(
			SyntheticCondition("ObservedGeneration", false, "", "", time.Time{}), err)}
	}

	if found {
		if observedGeneration < obj.Generation {
			return []status.ConditionStatus{ConditionStatusProgressing(
				SyntheticCondition("ObservedGeneration", false, "Outdated",
					fmt.Sprintf("Observed generation %d is less than desired generation %d",
						observedGeneration, obj.Generation), time.Time{}))}
		}
	}
	return nil
}
