package redhat

// olm.go implements an analyzer for resources managed by Operator Lifecycle Manager (OLM)
// (https://olm.operatorframework.io/). This is not a third-party operator, but it
// demonstrates how to extend kube-health with custom analyzers.

import (
	"context"
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
		ReversedPolarityConditions: analyze.NewStringMatchers("CatalogSourcesUnhealthy", "ResolutionFailed"),
	}

	olmAlwaysGreenAnalyzer = analyze.AlwaysGreenAnalyzer{Kinds: []schema.GroupKind{gkOLMOperatorGroup}}
)

type OLMSubscriptionAnalyzer struct {
	e *eval.Evaluator
}

func (_ OLMSubscriptionAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkOLMSubscription
}

func (a OLMSubscriptionAnalyzer) Analyze(ctx context.Context, obj *status.Object) status.ObjectStatus {
	installPlanStatuses := a.AnalyzeInstallPlans(ctx, obj)
	csvStatuses := a.AnalyzeCSV(ctx, obj)

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

func (a OLMSubscriptionAnalyzer) AnalyzeInstallPlans(ctx context.Context, obj *status.Object) []status.ObjectStatus {
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

	installPlans, err := a.e.EvalQuery(ctx, eval.RefQuerySpec{
		Object:    obj,
		RefObject: objRef,
	}, OLMInstallPlanAnalyzer{})

	if err != nil {
		klog.V(5).ErrorS(err, "Failed to evaluate install plan dependency", "object", obj)
		return nil
	}

	return installPlans
}

func (a OLMSubscriptionAnalyzer) AnalyzeCSV(ctx context.Context, obj *status.Object) []status.ObjectStatus {
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

	csv, err := a.e.EvalQuery(ctx, eval.RefQuerySpec{
		Object:    obj,
		RefObject: objRef,
	}, OLMCSVAnalyzer{})

	if err != nil {
		klog.V(5).ErrorS(err, "Failed to evaluate csv status", "object", obj)
		return nil
	}

	return csv
}

type OLMInstallPlanAnalyzer struct{}

// not really needed, as we call the analyzer explicitly
func (_ OLMInstallPlanAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkOLMInstallPlan
}

func (_ OLMInstallPlanAnalyzer) Analyze(ctx context.Context, obj *status.Object) status.ObjectStatus {
	conditions, err := analyze.AnalyzeObjectConditions(obj, []analyze.ConditionAnalyzer{
		analyze.GenericConditionAnalyzer{
			Conditions: analyze.NewStringMatchers("Installed"),
		}})

	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	return analyze.AggregateResult(obj, nil, conditions)
}

type OLMCSVAnalyzer struct{}

// not really needed, as we call the analyzer explicitly
func (_ OLMCSVAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkOLMCSV
}

func (_ OLMCSVAnalyzer) Analyze(ctx context.Context, obj *status.Object) status.ObjectStatus {
	statusData, found, err := unstructured.NestedMap(obj.Unstructured.Object, "status")
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}
	if !found {
		return status.UnknownStatus(obj)
	}

	condition := metav1.Condition{}
	err = analyze.FromUnstructured(statusData, &condition)
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}
	phase, found, _ := unstructured.NestedString(statusData, "phase")
	if found {
		condition.Type = phase
	}

	conditionsStatuses := analyze.AnalyzeConditions([]*metav1.Condition{&condition},
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
	analyze.Register.RegisterSimple(olmAlwaysGreenAnalyzer)
}
