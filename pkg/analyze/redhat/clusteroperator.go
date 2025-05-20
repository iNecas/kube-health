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

	// cloud-controller-manager references itself in the related objects
	// so this is to avoid endless loop
	if obj.Name == "cloud-controller-manager" {
		return analyze.AggregateResult(obj, nil, conditions)
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

		if slices.Contains(analyze.Register.IgnoredKinds(), c.evaluator.ResourceToKind(gr).GroupKind()) {
			continue
		}
		var namespace string
		if ns, ok := relObjecMap["namespace"]; ok {
			namespace = ns.(string)
		}
		name := relObjecMap["name"].(string)
		relObjectsStatuses, err := c.evaluator.EvalResource(ctx, gr, namespace, name)
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

	analyze.Register.RegisterIgnoredKinds(
		schema.GroupKind{Kind: "Namespace"},
		schema.GroupKind{Kind: "Secret"},
		schema.GroupKind{Kind: "ConfigMap"},
		schema.GroupKind{Kind: "ServiceAccount"},
		schema.GroupKind{Kind: "ClusterRole", Group: "rbac.authorization.k8s.io"},
		schema.GroupKind{Kind: "ClusterRoleBinding", Group: "rbac.authorization.k8s.io"},
		schema.GroupKind{Kind: "Role", Group: "rbac.authorization.k8s.io"},
		schema.GroupKind{Kind: "RoleBinding", Group: "rbac.authorization.k8s.io"},
		schema.GroupKind{Kind: "CustomResourceDefinition", Group: "apiextensions.k8s.io"},
		schema.GroupKind{Kind: "SecurityContextConstraints", Group: "security.openshift.io"},
		schema.GroupKind{Kind: "MutatingWebhookConfiguration", Group: "admissionregistration.k8s.io"},
		schema.GroupKind{Kind: "ValidatingWebhookConfiguration", Group: "admissionregistration.k8s.io"},
		schema.GroupKind{Kind: "OAuth", Group: "config.openshift.io"},
		schema.GroupKind{Kind: "Node", Group: "config.openshift.io"},
		schema.GroupKind{Kind: "CloudCredential", Group: "operator.openshift.io"},
		schema.GroupKind{Kind: "ConsolePlugin", Group: "console.openshift.io"},
		schema.GroupKind{Kind: "MachineConfig", Group: "machineconfiguration.openshift.io"},
		schema.GroupKind{Kind: "Template", Group: "template.openshift.io"},
		schema.GroupKind{Kind: "ServiceMonitor", Group: "monitoring.coreos.com"},
		schema.GroupKind{Kind: "PrometheusRule", Group: "monitoring.coreos.com"},
	)
}
