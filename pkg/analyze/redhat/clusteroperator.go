package redhat

import (
	"context"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/eval"
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

type ClusterOperatorAnalyzer struct {
	evaluator *eval.Evaluator
}

func (_ ClusterOperatorAnalyzer) Supports(obj *status.Object) bool {
	return obj.GroupVersionKind().GroupKind() == gkClusterOperator
}

func (c *ClusterOperatorAnalyzer) Analyze(ctx context.Context, obj *status.Object) status.ObjectStatus {
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

	relatedObjects, _, err := unstructured.NestedSlice(obj.Unstructured.Object, "status", "relatedObjects")
	if err != nil {
		// do not add any substatuses in case of error
		return analyze.AggregateResult(obj, nil, conditions)
	}

	subStatuses := c.evaluateRelatedObjects(ctx, relatedObjects)
	return analyze.AggregateResult(obj, subStatuses, conditions)
}

func (c *ClusterOperatorAnalyzer) evaluateRelatedObjects(ctx context.Context, relatedObjects []interface{}) []status.ObjectStatus {
	var statuses []status.ObjectStatus
	for _, relObjec := range relatedObjects {
		relObjecMap, ok := relObjec.(map[string]interface{})
		if !ok {
			continue
		}
		resource := relObjecMap["resource"].(string)
		group := relObjecMap["group"].(string)

		gr := schema.GroupResource{Group: group, Resource: resource}

		if slices.Contains(analyze.Register.IgnoredResources(), gr) {
			continue
		}
		name := relObjecMap["name"].(string)
		// TODO try to get namespace name
		relObjectsStatuses, err := c.evaluator.EvalResource(ctx, gr, "", name)
		if err != nil {
			continue
		}
		statuses = append(statuses, relObjectsStatuses...)
	}
	return statuses
}

func init() {
	analyze.Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return &ClusterOperatorAnalyzer{
			evaluator: e,
		}
	})

	analyze.Register.RegisterIgnoredResources(
		schema.GroupResource{Resource: "namespaces"},
		schema.GroupResource{Resource: "secrets"},
		schema.GroupResource{Resource: "configmaps"},
		schema.GroupResource{Resource: "clusterroles", Group: "rbac.authorization.k8s.io"},
		schema.GroupResource{Resource: "clusterrolebindings", Group: "rbac.authorization.k8s.io"},
		schema.GroupResource{Resource: "roles", Group: "rbac.authorization.k8s.io"},
		schema.GroupResource{Resource: "rolesbindings", Group: "rbac.authorization.k8s.io"},
		schema.GroupResource{Resource: "customresourcedefinitions", Group: "apiextensions.k8s.io"},
		schema.GroupResource{Resource: "securitycontextconstraints", Group: "security.openshift.io"},
		schema.GroupResource{Resource: "validatingwebhookconfigurations", Group: "admissionregistration.k8s.io"},
		schema.GroupResource{Resource: "mutatingwebhookconfigurations", Group: "admissionregistration.k8s.io"},
	)

}
