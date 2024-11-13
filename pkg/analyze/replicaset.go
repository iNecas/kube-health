package analyze

import (
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

var (
	gkReplicaSet = appsv1.SchemeGroupVersion.WithKind("ReplicaSet").GroupKind()
)

type ReplicaSetAnalyzer struct {
	e *eval.Evaluator
}

func (_ ReplicaSetAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkReplicaSet
}

func (a ReplicaSetAnalyzer) Analyze(obj *status.Object) status.ObjectStatus {
	subStatuses, err := a.e.EvalQuery(
		eval.NewSelectorLabelQuerySpec(obj, gkPod), PodAnalyzer{e: a.e})

	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	conditions, err := AnalyzeObjectConditions(obj, append(
		[]ConditionAnalyzer{replicaSetConditionAnalyzer{}},
		DefaultConditionAnalyzers...))

	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	synthConditions, err := replicaSetSyntehticConditions(obj)
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}
	conditions = append(conditions, synthConditions...)

	return AggregateResult(obj, subStatuses, conditions)
}

func replicaSetSyntehticConditions(obj *status.Object) ([]status.ConditionStatus, error) {
	var rs appsv1.ReplicaSet
	var conditions []status.ConditionStatus

	err := FromUnstructured(obj.Unstructured.Object, &rs)
	if err != nil {
		return nil, err
	}

	var replicas int32
	if rs.Spec.Replicas != nil {
		replicas = *rs.Spec.Replicas
	} else {
		// Controller uses 1 as default if not specified.
		replicas = 1
	}

	if replicas > rs.Status.FullyLabeledReplicas {
		conditions = append(conditions, ConditionStatusError(
			SyntheticCondition("ReplicasLabeled", false, "Unlabeled",
				fmt.Sprintf("Labeled: %d/%d", rs.Status.FullyLabeledReplicas, replicas), time.Time{})))
	}
	if replicas > rs.Status.AvailableReplicas {
		conditions = append(conditions, ConditionStatusError(
			SyntheticCondition("ReplicasAvailable", false, "Unavailable",
				fmt.Sprintf("Available: %d/%d", rs.Status.AvailableReplicas, replicas), time.Time{})))
	}
	if replicas > rs.Status.ReadyReplicas {
		conditions = append(conditions, ConditionStatusError(
			SyntheticCondition("ReplicasReady", false, "NotReady",
				fmt.Sprintf("Ready: %d/%d", rs.Status.ReadyReplicas, replicas), time.Time{})))
	} else if replicas == rs.Status.ReadyReplicas {
		conditions = append(conditions, ConditionStatusOk(
			SyntheticCondition("ReplicasReady", true, "Ready", "All replicas are ready", time.Time{})))
	}
	if rs.Status.Replicas > replicas {
		conditions = append(conditions, ConditionStatusError(
			SyntheticCondition("TerminatedReplicas", false, "Terminating",
				fmt.Sprintf("Pending terminations: %d", rs.Status.Replicas-replicas), time.Time{})))
	}
	return conditions, nil
}

// deploymentConditionAnalyzer implements ConditionAnalyzer for ReplicaSet
type replicaSetConditionAnalyzer struct{}

func (a replicaSetConditionAnalyzer) Analyze(cond *metav1.Condition) status.ConditionStatus {
	if cond.Type == "ReplicaFailure" && cond.Status == metav1.ConditionTrue {
		return ConditionStatusError(cond)
	}

	return ConditionStatusNoMatch
}

func init() {
	Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return ReplicaSetAnalyzer{e: e}
	})
}
