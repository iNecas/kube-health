package analyze

import (
	"slices"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/status"
)

// ignoredGroupKinds is a list of GroupKinds that are ignored by the default.
// These are mostly resources that are not interesting for the status evaluation.
var ignoredGroupKinds = []schema.GroupKind{
	{Kind: "ConfigMap"},
	{Kind: "ServiceAccount"},
	{Kind: "Role", Group: "rbac.authorization.k8s.io"},
	{Kind: "RoleBinding", Group: "rbac.authorization.k8s.io"},
	{Kind: "Secret"},
	{Kind: "EndpointSlice", Group: "discovery.k8s.io"},
	{Kind: "Service", Group: ""},
	{Kind: "ControllerRevision", Group: "apps"},
	{Kind: "Log", Group: "kube-health.io"},
}

type ReadyConditionAnalyzer struct{}

func (a ReadyConditionAnalyzer) Analyze(cond *metav1.Condition) status.ConditionStatus {
	if cond.Type != "Ready" {
		return ConditionStatusNoMatch
	}
	res := status.Unknown
	switch cond.Status {
	case metav1.ConditionTrue:
		res = status.Ok
	case metav1.ConditionFalse:
		res = status.Error
	}
	return status.ConditionStatus{
		Condition: cond,
		CondStatus: &status.Status{
			Result: res,
		},
	}
}

type ProgressingConditionAnalyzer struct{}

func (a ProgressingConditionAnalyzer) Analyze(cond *metav1.Condition) status.ConditionStatus {
	if cond.Type != "Progressing" {
		return ConditionStatusNoMatch
	}
	if cond.Status == metav1.ConditionTrue {
		return ConditionStatusProgressing(cond)
	} else {
		// We can't tell much if the false condition means problem or not:
		// need to rely on other conditions to fail, so we part the progressing
		// condition itself as OK.
		return ConditionStatusOk(cond)
	}
}

// GenericConditionAnalyzer is a generic condition analyzer that can be used
// for any condition type. It can be configured to match all conditions or
// only specific ones.
// The analyzer can also be configured to reverse the polarity of the condition:
// by default, True is considered OK and False is considered Error. The ReversedPolarityTypes
// is used for conditions that should be treated the other way around:
// False is OK, True is Error, e.g. Degraded.
type GenericConditionAnalyzer struct {
	MatchAll              bool
	Types                 []string
	ReversedPolarityTypes []string
}

func (a GenericConditionAnalyzer) Analyze(cond *metav1.Condition) status.ConditionStatus {
	res := status.Unknown
	if !a.MatchAll && !slices.Contains(a.Types, cond.Type) && !slices.Contains(a.ReversedPolarityTypes, cond.Type) {
		return ConditionStatusNoMatch
	}

	reverse := slices.Contains(a.ReversedPolarityTypes, cond.Type)
	switch cond.Status {
	case metav1.ConditionTrue:
		if reverse {
			res = status.Error
		} else {
			res = status.Ok
		}
	case metav1.ConditionFalse:
		if reverse {
			res = status.Ok
		} else {
			res = status.Error
		}
	}
	return status.ConditionStatus{
		Condition: cond,
		CondStatus: &status.Status{
			Result: res,
		},
	}
}

func ConditionStatusUnknown(cond *metav1.Condition) status.ConditionStatus {
	return status.ConditionStatus{
		Condition: cond,
		CondStatus: &status.Status{
			Result:      status.Unknown,
			Progressing: false,
		},
	}
}

func ConditionStatusUnknownWithError(cond *metav1.Condition, err error) status.ConditionStatus {
	return status.ConditionStatus{
		Condition: cond,
		CondStatus: &status.Status{
			Result:      status.Unknown,
			Progressing: false,
			Err:         err,
		},
	}
}

func ConditionStatusOk(cond *metav1.Condition) status.ConditionStatus {
	return status.ConditionStatus{
		Condition: cond,
		CondStatus: &status.Status{
			Result:      status.Ok,
			Progressing: false,
		},
	}
}

func ConditionStatusWarning(cond *metav1.Condition) status.ConditionStatus {
	return status.ConditionStatus{
		Condition: cond,
		CondStatus: &status.Status{
			Result:      status.Warning,
			Progressing: false,
		},
	}
}

func ConditionStatusError(cond *metav1.Condition) status.ConditionStatus {
	return status.ConditionStatus{
		Condition: cond,
		CondStatus: &status.Status{
			Result:      status.Error,
			Progressing: false,
		},
	}
}

func ConditionStatusProgressing(cond *metav1.Condition) status.ConditionStatus {
	return status.ConditionStatus{
		Condition: cond,
		CondStatus: &status.Status{
			Result:      status.Unknown,
			Progressing: true,
		},
	}
}

// SyntheticCondition creates a synthetic condition with the given values.
// It's used for cases when the condition is not present in the object but
// we want to indicate a particular status. For example, when the object
// is not reporting Ready condition, we can synthesize it based on other
// conditions.
func SyntheticCondition(condType string, statusVal bool, reason, message string,
	lastTransitionTime time.Time) *metav1.Condition {
	var mStatus metav1.ConditionStatus = metav1.ConditionFalse
	if statusVal {
		mStatus = metav1.ConditionTrue
	}

	return &metav1.Condition{
		Type:               condType,
		Status:             mStatus,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Time{lastTransitionTime},
	}
}

func SyntheticConditionOk(condType, message string) status.ConditionStatus {
	return ConditionStatusOk(
		SyntheticCondition(condType, true, "", message, time.Time{}))
}

func SyntheticConditionWarning(condType, reason, message string) status.ConditionStatus {
	return ConditionStatusWarning(
		SyntheticCondition(condType, true, reason, message, time.Time{}))
}

func SyntheticConditionProgressing(condType, reason, message string) status.ConditionStatus {
	return ConditionStatusProgressing(
		SyntheticCondition(condType, true, reason, message, time.Time{}))
}

func SyntheticConditionError(condType, reason, message string) status.ConditionStatus {
	return ConditionStatusError(
		SyntheticCondition(condType, true, reason, message, time.Time{}))
}

func init() {
	Register.RegisterIgnoredKinds(ignoredGroupKinds...)
}
