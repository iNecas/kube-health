package analyze

import (
	"context"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

var gkDeployment = appsv1.SchemeGroupVersion.WithKind("Deployment").GroupKind()

type DeploymentAnalyzer struct {
	e *eval.Evaluator
}

func (_ DeploymentAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkDeployment
}

func (a DeploymentAnalyzer) Analyze(ctx context.Context, obj *status.Object) status.ObjectStatus {
	subStatuses, err := a.e.EvalQuery(ctx,
		eval.NewSelectorLabelQuerySpec(obj, gkReplicaSet), ReplicaSetAnalyzer{e: a.e})

	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	conditions, err := AnalyzeObjectConditions(obj, append(
		[]ConditionAnalyzer{deploymentConditionAnalyzer{}},
		DefaultConditionAnalyzers...))

	// We don't care about ReplicaSets scaled down to 0.
	subStatuses = slices.DeleteFunc(subStatuses, func(s status.ObjectStatus) bool {
		replicas, found, _ := unstructured.NestedInt64(s.Object.Unstructured.Object, "spec", "replicas")
		return found && replicas == 0
	})

	// More precise progress detection based on ReplicaSets status.
	progressingCond := status.GetCondition(conditions, "Progressing")
	if progressingCond != nil {
		allDone := len(subStatuses) > 0
		for _, subStatus := range subStatuses {
			if subStatus.Status().Result != status.Ok || subStatus.Status().Progressing {
				allDone = false
				break
			}
		}
		if allDone {
			progressingCond.CondStatus.Progressing = false
			progressingCond.CondStatus.Result = status.Ok
		}
	}

	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	return AggregateResult(obj, subStatuses, conditions)
}

// deploymentConditionAnalyzer implements ConditionAnalyzer for Deployment
type deploymentConditionAnalyzer struct{}

func (a deploymentConditionAnalyzer) Analyze(cond *metav1.Condition) status.ConditionStatus {
	if cond.Type == "Progressing" {
		if cond.Reason == "ProgressDeadlineExceeded" {
			return ConditionStatusError(cond)
		}
	}

	if cond.Type == "Available" {
		if cond.Status == metav1.ConditionFalse {
			return ConditionStatusError(cond)
		}
	}

	return ConditionStatusNoMatch
}

func init() {
	Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return DeploymentAnalyzer{e: e}
	})
}
