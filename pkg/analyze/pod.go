package analyze

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

var (
	gkPod              = schema.GroupKind{Group: "", Kind: "Pod"}
	progressingTimeout = 3 * time.Minute
)

type PodAnalyzer struct {
	e *eval.Evaluator
}

func (_ PodAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkPod
}

func (a PodAnalyzer) Analyze(obj *status.Object) status.ObjectStatus {
	conditions, err := AnalyzeObjectConditions(obj, DefaultConditionAnalyzers)
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	var pod corev1.Pod
	err = FromUnstructured(obj.Unstructured.Object, &pod)
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}
	conditions = append(conditions, podSyntheticConditions(&pod)...)

	// We treat the containers as sub-objects of the pod, even though technically
	// they are just fields of the pod object. This makes it easier to report
	// details of each container separately.
	containerStatuses := a.analyzePodContainers(obj, &pod)

	return AggregateResult(obj, containerStatuses, conditions)
}

func podSyntheticConditions(pod *corev1.Pod) []status.ConditionStatus {
	var conditions []status.ConditionStatus

	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		conditions = append(conditions, SyntheticConditionOk("Succeeded", ""))
	case corev1.PodFailed:
		conditions = append(conditions, SyntheticConditionError("Failed", "Failed", ""))
	}

	return conditions
}

func (a PodAnalyzer) analyzePodContainers(obj *status.Object, pod *corev1.Pod) []status.ObjectStatus {
	var ret []status.ObjectStatus

	for _, cs := range pod.Status.ContainerStatuses {
		containerObjStatus := a.analyzeContainer(obj, cs)
		if containerObjStatus.Object != nil {
			ret = append(ret, containerObjStatus)
		}
	}

	return ret
}

// analyzeContainer analyzes the status of a container, treating it as a separate
// sub-object of the pod.
func (a PodAnalyzer) analyzeContainer(obj *status.Object, cs corev1.ContainerStatus) status.ObjectStatus {
	containerObj := &status.Object{
		TypeMeta: metav1.TypeMeta{
			Kind: "Container",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: cs.Name,
		},
	}

	conditions := []status.ConditionStatus{}
	var cond status.ConditionStatus
	if cs.State.Waiting != nil {
		var lastTransitionTime time.Time
		progressing := true
		if lastState := cs.LastTerminationState.Terminated; lastState != nil {
			lastTransitionTime = lastState.FinishedAt.Time
		}

		if !lastTransitionTime.IsZero() && time.Since(lastTransitionTime) > progressingTimeout {
			progressing = false
		}
		reason := cs.State.Waiting.Reason
		cond = SyntheticConditionError("Waiting", reason, "")
		cond.LastTransitionTime = metav1.NewTime(lastTransitionTime)
		cond.CondStatus.Progressing = progressing
	}

	if cs.State.Running != nil {
		cond = SyntheticConditionOk("Running", "")
		cond.LastTransitionTime = cs.State.Running.StartedAt
	}

	if !cs.Ready {
		cond = SyntheticConditionError("Ready", "NotReady", "")
	}

	if cs.State.Terminated != nil {
		reason := cs.State.Terminated.Reason
		cond = SyntheticConditionError("Terminated", reason, "")
	}

	if (cond == status.ConditionStatus{}) {
		return status.ObjectStatus{}
	}

	if cond.Status().Result > status.Ok {
		a.expandWithLogs(obj, cs.Name, &cond)
	}

	conditions = append(conditions, cond)

	return AggregateResult(containerObj, nil, conditions)
}

// expandWithLogs loads container logs and appends them to the condition message.
func (a PodAnalyzer) expandWithLogs(obj *status.Object, container string, cond *status.ConditionStatus) {
	logs, err := a.loadContainerLogs(obj, container)
	if err != nil {
		logs = "Error loading logs: " + err.Error() + "\n"
	}

	if logs == "" {
		return
	}

	if cond.Message != "" {
		cond.Message = "\n"
	}

	cond.Message += "Logs:\n"
	cond.Message += logs
}

func (a PodAnalyzer) loadContainerLogs(obj *status.Object, container string) (string, error) {
	logobjs, err := a.e.Load(eval.PodLogQuerySpec{
		Object:    obj,
		Container: container,
	})
	if err != nil {
		return "", err
	}

	if len(logobjs) == 0 {
		return "", nil
	}

	logs, _, _ := unstructured.NestedString(logobjs[0].Unstructured.Object, "log")
	return logs, nil
}

func init() {
	Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return PodAnalyzer{e: e}
	})
}
