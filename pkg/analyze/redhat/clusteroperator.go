package redhat

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/status"
)

var (
	gkClusterOperator                 = schema.GroupKind{Group: "config.openshift.io", Kind: "ClusterOperator"}
	clusteroperatorConditionsAnalyzer = analyze.GenericConditionAnalyzer{
		Conditions:                 analyze.NewStringMatchers("Available"),
		ReversedPolarityConditions: analyze.NewStringMatchers("Degraded"),
	}
	insightsConditionsAnalyzer = analyze.GenericConditionAnalyzer{
		ReversedPolarityConditions: analyze.NewStringMatchers("ClusterTransferAvailable"),
		WarningConditions:          analyze.NewRegexpMatchers("RemoteConfiguration"),
		ProgressingConditions:      analyze.NewStringMatchers("ClusterTransferAvailable"),
	}
)

type ClusterOperatorAnalyzer struct{}

func (_ ClusterOperatorAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkClusterOperator
}

func (_ ClusterOperatorAnalyzer) Analyze(obj *status.Object) status.ObjectStatus {
	conditionAnalyzers := append([]analyze.ConditionAnalyzer{clusteroperatorConditionsAnalyzer},
		analyze.DefaultConditionAnalyzers...,
	)

	if obj.Name == "insights" {
		conditionAnalyzers = append(conditionAnalyzers, insightsConditionsAnalyzer)
	}
	conditions, err := analyze.AnalyzeObjectConditions(obj, conditionAnalyzers)

	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	return analyze.AggregateResult(obj, nil, conditions)
}

func init() {
	analyze.Register.RegisterSimple(ClusterOperatorAnalyzer{})
}
