package analyze

import (
	"regexp"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/status"
)

// ignoredGroupKinds is a list of GroupKinds that are ignored by the default
// when evaluating sub-objects.
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
}

type Matcher interface {
	Match(string) bool
}

type StringMatcher string

func NewStringMatchers(patterns ...string) []Matcher {
	matchers := make([]Matcher, len(patterns))
	for i, pattern := range patterns {
		matchers[i] = StringMatcher(pattern)
	}
	return matchers
}

func (m StringMatcher) Match(s string) bool {
	return strings.ToLower(string(m)) == strings.ToLower(s)
}

type RegexpMatcher regexp.Regexp

func (m *RegexpMatcher) Match(s string) bool {
	r := (*regexp.Regexp)(m)
	return len(r.FindStringSubmatch(s)) > 0
}

func NewRegexpMatchers(patterns ...string) []Matcher {
	matchers := make([]Matcher, len(patterns))
	for i, pattern := range patterns {
		matchers[i] = (*RegexpMatcher)(regexp.MustCompile("(?i)" + pattern))
	}
	return matchers
}

// GenericConditionAnalyzer is a generic condition analyzer that can be used
// for any condition type. It can be configured to match all conditions or
// only specific ones.
// The analyzer can also be configured to reverse the polarity of the condition:
// by default, True is considered OK and False is considered Error. The ReversedPolarityTypes
// is used for conditions that should be treated the other way around:
// False is OK, True is Error, e.g. Degraded.
//
// By default, when a condition is matched and the value is in unexpected state,
// it's considered as Error, unless the condition is matched by `WarningConditions`,
// `UnknownConditions` or `ProgressingConditions`, in which case the corresponding
// status is set.
type GenericConditionAnalyzer struct {
	MatchAll                   bool
	Conditions                 []Matcher
	ReversedPolarityConditions []Matcher
	WarningConditions          []Matcher
	ProgressingConditions      []Matcher
	UnknownConditions          []Matcher
}

func (a GenericConditionAnalyzer) match(condType string) (match, reverse, progressing bool, result status.Result) {
	match = false
	result = status.Unknown
	progressing = false

	if a.MatchAll {
		match = true
	}

	for _, t := range a.Conditions {
		if t.Match(condType) {
			match = true
			result = status.Error
			break
		}
	}

	for _, t := range a.ReversedPolarityConditions {
		if t.Match(condType) {
			match = true
			reverse = true
			// Assigning Error by default, can be further overridden by matchers below.
			result = status.Error
			break
		}
	}

	for _, t := range a.ProgressingConditions {
		if t.Match(condType) {
			match = true
			progressing = true
			result = status.Unknown
			break
		}
	}

	for _, t := range a.WarningConditions {
		if t.Match(condType) {
			match = true
			result = status.Warning
			break
		}
	}

	for _, t := range a.UnknownConditions {
		if t.Match(condType) {
			match = true
			result = status.Unknown
			break
		}
	}

	return match, reverse, progressing, result
}

func (a GenericConditionAnalyzer) Analyze(cond *metav1.Condition) status.ConditionStatus {
	res := status.Unknown
	progressing := false
	match, reverse, targetProgressing, targetRes := a.match(cond.Type)

	if !match {
		return ConditionStatusNoMatch
	}

	if (!reverse && cond.Status == metav1.ConditionFalse) ||
		(reverse && cond.Status == metav1.ConditionTrue) {
		res = targetRes
		progressing = targetProgressing
	} else if cond.Status == metav1.ConditionUnknown {
		res = status.Unknown
	} else {
		res = status.Ok
	}

	return status.ConditionStatus{
		Condition: cond,
		CondStatus: &status.Status{
			Result:      res,
			Progressing: progressing,
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
