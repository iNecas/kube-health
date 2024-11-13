package redhat

// olm.go implements an analyzer for resources managed by Operator Lifecycle Manager (OLM)
// (https://olm.operatorframework.io/). This is not a third-party operator, but it
// demonstrates how to extend kube-health with custom analyzers.

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

var (
	gkOLMSubscription              = schema.GroupKind{Group: "operators.coreos.com", Kind: "Subscription"}
	gkOLMInstallPlan               = schema.GroupKind{Group: "operators.coreos.com", Kind: "InstallPlan"}
	gkOLMOperatorGroup             = schema.GroupKind{Group: "operators.coreos.com", Kind: "OperatorGroup"}
	gkOLMCSV                       = schema.GroupKind{Group: "operators.coreos.com", Kind: "ClusterServiceVersion"}
	subscriptionConditionsAnalyzer = analyze.GenericConditionAnalyzer{
		ReversedPolarityTypes: []string{"CatalogSourcesUnhealthy", "ResolutionFailed"},
	}

	olmAlwaysGreenAnalyzer = analyze.AlwaysGreenAnalyzer{Kinds: []schema.GroupKind{gkOLMOperatorGroup}}
)

type OLMSubscriptionAnalyzer struct {
	e *eval.Evaluator
}

func (_ OLMSubscriptionAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkOLMSubscription
}

func (a OLMSubscriptionAnalyzer) Analyze(obj *status.Object) status.ObjectStatus {
	installPlanStatuses := a.AnalyzeInstallPlans(obj)
	csvStatuses := a.AnalyzeCSV(obj)

	conditions, err := analyze.AnalyzeObjectConditions(obj, append(
		[]analyze.ConditionAnalyzer{subscriptionConditionsAnalyzer},
		analyze.DefaultConditionAnalyzers...))

	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	if len(installPlanStatuses) == 0 {
		conditions = append(conditions, analyze.ConditionStatusProgressing(
			analyze.SyntheticCondition("InstallPlan", false, "InstallPlanMissing", "Install plan not found", time.Time{})))
	}

	subStatuses := append(installPlanStatuses, csvStatuses...)

	return analyze.AggregateResult(obj, subStatuses, conditions)
}

func (a OLMSubscriptionAnalyzer) AnalyzeInstallPlans(obj *status.Object) []status.ObjectStatus {
	var objRef corev1.ObjectReference
	refData, found, err := unstructured.NestedMap(obj.Unstructured.Object, "status", "installPlanRef")
	if err != nil {
		klog.V(5).ErrorS(err, "Failed to get install plan reference from OLM Subscription", "object", obj)
		return nil
	}
	if !found {
		return nil
	}

	err = analyze.FromUnstructured(refData, &objRef)
	if err != nil {
		klog.ErrorS(err, "Failed to get object reference from OLM Subscription", "object", obj)
		return nil
	}

	installPlans, err := a.e.EvalQuery(eval.RefQuerySpec{
		Object:    obj,
		RefObject: objRef,
	}, nil)

	if err != nil {
		klog.V(5).ErrorS(err, "Failed to evaluate install plan dependency", "object", obj)
		return nil
	}

	return installPlans
}

func (a OLMSubscriptionAnalyzer) AnalyzeCSV(obj *status.Object) []status.ObjectStatus {
	csvName, found, err := unstructured.NestedString(obj.Unstructured.Object, "status", "currentCSV")
	if err != nil {
		klog.V(5).ErrorS(err, "Failed to get install plan reference from OLM Subscription", "object", obj)
		return nil
	}
	if !found {
		return nil
	}

	objRef := corev1.ObjectReference{
		APIVersion: "operators.coreos.com/v1alpha1",
		Kind:       "ClusterServiceVersion",
		Name:       csvName,
		Namespace:  obj.Namespace,
	}

	csv, err := a.e.EvalQuery(eval.RefQuerySpec{
		Object:    obj,
		RefObject: objRef,
	}, OLMCSVAnalyzer{})

	if err != nil {
		klog.V(5).ErrorS(err, "Failed to evaluate csv status", "object", obj)
		return nil
	}

	return csv
}

type OLMCSVAnalyzer struct{}

// TODO: Supports should not be needed for analyzers not registered in the evaluator.
func (_ OLMCSVAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkOLMCSV
}

func (_ OLMCSVAnalyzer) Analyze(obj *status.Object) status.ObjectStatus {
	var conditions []*metav1.Condition

	conditionsData, found, err := unstructured.NestedSlice(obj.Unstructured.Object, "status", "conditions")
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}
	if !found {
		return status.UnknownStatus(obj)
	}

	for _, condData := range conditionsData {
		cond, ok := condData.(map[string]interface{})
		if !ok {
			continue
		}

		condition := metav1.Condition{}
		err := analyze.FromUnstructured(cond, &condition)
		if err != nil {
			return status.UnknownStatusWithError(obj, err)
		}

		// OLM CSVs use "phase" instead of "type" for condition type, we convert it here.
		phase, found, _ := unstructured.NestedString(cond, "phase")
		if found {
			condition.Type = phase
		}

		conditions = append(conditions, &condition)
	}

	conditionsStatuses := analyze.AnalyzeConditions(conditions,
		[]analyze.ConditionAnalyzer{olmCSVConditionAnalyzer{}})
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	return analyze.AggregateResult(obj, nil, conditionsStatuses)
}

type olmCSVConditionAnalyzer struct{}

func (a olmCSVConditionAnalyzer) Analyze(cond *metav1.Condition) status.ConditionStatus {
	if cond.Type == "Failed" {
		return analyze.ConditionStatusError(cond)
	}

	return analyze.ConditionStatusNoMatch
}

func init() {
	analyze.Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return OLMSubscriptionAnalyzer{e: e}
	})

}
