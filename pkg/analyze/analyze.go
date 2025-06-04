package analyze

import (
	"context"
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

type ConditionAnalyzer interface {
	Analyze(*metav1.Condition) status.ConditionStatus
}

var (
	// DefaultAlwaysGreenAnalyzer is an analyzer that always returns OK status.
	// It's empty right now, but it can be configured to match specific kinds
	// we want to always consider OK.
	DefaultAlwaysGreenAnalyzer = AlwaysGreenAnalyzer{
		Kinds: []schema.GroupKind{
			{Group: "", Kind: "Namespace"},
		},
	}

	// ConditionStatusNoMatch is returned by condition analyzer when it's not
	// applicable to the condition.
	ConditionStatusNoMatch = status.ConditionStatus{}

	// Register is a global registry of analyzers.
	Register = &AnalyzerRegister{}
)

// AnalyzeObjectConditions analyzes the conditions of the object using the
// provided analyzers. It expects the conditions to be in the "status.conditions"
// field of the object.
func AnalyzeObjectConditions(obj *status.Object, analyzers []ConditionAnalyzer) ([]status.ConditionStatus, error) {
	data, _, err := unstructured.NestedSlice(obj.Unstructured.Object, "status", "conditions")
	if err != nil {
		return nil, fmt.Errorf("Error getting conditions: %w", err)
	}

	return AnalyzeRawConditions(data, analyzers)
}

func AnalyzeRawConditions(data []interface{}, analyzers []ConditionAnalyzer) ([]status.ConditionStatus, error) {
	conditions, err := loadConditions(data)
	if err != nil {
		return nil, err
	}

	return AnalyzeConditions(conditions, analyzers), nil
}

func AnalyzeConditions(conditions []*metav1.Condition, analyzers []ConditionAnalyzer) []status.ConditionStatus {
	ret := make([]status.ConditionStatus, 0, len(conditions))
	for _, cond := range conditions {
		var cs status.ConditionStatus
		for _, a := range analyzers {
			cs = a.Analyze(cond)
			if cs != ConditionStatusNoMatch {
				break
			}
		}

		if cs.Condition == nil {
			ret = append(ret, ConditionStatusUnknown(cond))
		} else {
			ret = append(ret, cs)
		}
	}
	return ret
}

func loadConditions(conditions []interface{}) ([]*metav1.Condition, error) {

	ret := make([]*metav1.Condition, 0, len(conditions))

	for _, condData := range conditions {
		cond := metav1.Condition{}
		err := FromUnstructured(condData.(map[string]interface{}), &cond)
		if err != nil {
			return nil, fmt.Errorf("Error converting condition: %w", err)
		}
		ret = append(ret, &cond)
	}

	return ret, nil
}

func FromUnstructured(data map[string]interface{}, obj interface{}) error {
	return runtime.DefaultUnstructuredConverter.FromUnstructured(data, obj)
}

func AggregateResult(obj *status.Object, subStatuses []status.ObjectStatus,
	conditions []status.ConditionStatus) status.ObjectStatus {
	res := status.Unknown
	progressing := false

	for _, cond := range conditions {
		st := cond.Status()
		if st.Result > res {
			res = st.Result
		}
		if st.Progressing {
			progressing = true
		}
	}

	for _, sub := range subStatuses {
		subst := sub.Status()
		if subst.Result > res {
			res = subst.Result
		}
		if subst.Progressing {
			progressing = true
		}
	}

	return status.ObjectStatus{
		Object: obj,
		ObjStatus: status.Status{
			Result:      res,
			Progressing: progressing,
			Status:      res.String()},
		SubStatuses: subStatuses,
		Conditions:  conditions,
	}
}

// AlwaysGreenAnalyzer is an analyzer that always returns OK status
// for the supported kinds.
type AlwaysGreenAnalyzer struct {
	Kinds []schema.GroupKind
}

func (a AlwaysGreenAnalyzer) Supports(obj *status.Object) bool {
	return slices.Contains(a.Kinds, obj.GroupVersionKind().GroupKind())
}

func (a AlwaysGreenAnalyzer) Analyze(ctx context.Context, obj *status.Object) status.ObjectStatus {
	return status.OkStatus(obj, nil)
}

// AnalyzerRegister is a registry of analyzers.
// It allows to register new analyzers and ignored GroupKinds.
type AnalyzerRegister struct {
	analyzerInits []eval.AnalyzerInit
	ignored       []schema.GroupKind
}

// Register registers new analyzers.
func (r *AnalyzerRegister) Register(a ...eval.AnalyzerInit) {
	r.analyzerInits = append(r.analyzerInits, a...)
}

// RegisterSimple registers analyzers without any additional configuration.
func (r *AnalyzerRegister) RegisterSimple(as ...eval.Analyzer) {
	for _, a := range as {
		r.Register(func(e *eval.Evaluator) eval.Analyzer {
			return a
		})
	}
}

func (r AnalyzerRegister) IsIgnoredKind(gvk schema.GroupKind) bool {
	return slices.Contains(r.ignored, gvk)
}

func (r *AnalyzerRegister) RegisterIgnoredKinds(gk ...schema.GroupKind) {
	r.ignored = append(r.ignored, gk...)
}

func (r *AnalyzerRegister) AnalyzerInits() []eval.AnalyzerInit {
	return r.analyzerInits
}

func DefaultAnalyzers() []eval.AnalyzerInit {
	ret := make([]eval.AnalyzerInit, len(Register.AnalyzerInits()))
	copy(ret, Register.AnalyzerInits())
	ret = append(ret,
		func(_ *eval.Evaluator) eval.Analyzer { return DefaultAlwaysGreenAnalyzer },
		DefaultAnalyzerInit)
	return ret
}

// TODO: add support for more kinds from
// https://github.com/kubernetes-sigs/cli-utils/blob/master/pkg/kstatus/status/core.go
// - [  ] statefulset
// - [  ] job
// - [  ] daemonset
// - [  ] pdb
