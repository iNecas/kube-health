package redhat

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

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

	objectInfos := adaptRelatedObjects(obj, relatedObjects)

	subStatuses := c.evaluateRelatedObjects(ctx, objectInfos)
	return analyze.AggregateResult(obj, subStatuses, conditions)
}

func (c *ClusterOperatorAnalyzer) evaluateRelatedObjects(ctx context.Context, objectInfos []objectInfo) []status.ObjectStatus {
	var statuses []status.ObjectStatus
	for _, objInfo := range objectInfos {
		if analyze.Register.IsIgnoredKind(c.evaluator.ResourceToKind(objInfo.groupResource).GroupKind()) {
			continue
		}
		relObjectsStatuses, err := c.evaluator.EvalResource(ctx, objInfo.groupResource, objInfo.namespace, objInfo.name)
		if err != nil {
			klog.V(5).Infof("Failed to evaluate %s with name %s in the namespac %s: %v",
				objInfo.groupResource, objInfo.namespace, objInfo.name, err)
			continue
		}
		statuses = append(statuses, relObjectsStatuses...)
	}
	return statuses
}

type objectInfo struct {
	groupResource   schema.GroupResource
	name, namespace string
}

// adaptRelatedObjects reads and transforms the untyped related objects and
// checks if a related object name is not the same as the parent object name.
func adaptRelatedObjects(parent *status.Object, relatedObjects []interface{}) []objectInfo {
	var adaptedObjects []objectInfo
	for _, relObjec := range relatedObjects {
		relObjecMap, ok := relObjec.(map[string]interface{})
		if !ok {
			klog.V(5).Infof("Failed to convert %s to map[string]interface{}", relObjec)
			continue
		}
		resource := relObjecMap["resource"].(string)
		group := relObjecMap["group"].(string)

		gr := schema.GroupResource{Group: group, Resource: resource}
		var namespace string
		if ns, ok := relObjecMap["namespace"]; ok {
			namespace = ns.(string)
		}
		name := relObjecMap["name"].(string)
		objInf := objectInfo{
			groupResource: gr,
			name:          name,
			namespace:     namespace,
		}
		// some clusteroperators references themselves in the relatedObjects
		if parent.Name == name {
			continue
		}
		adaptedObjects = append(adaptedObjects, objInf)
	}
	return adaptedObjects
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
