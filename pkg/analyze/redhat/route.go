package redhat

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

var (
	gkRoute = schema.GroupKind{Group: "route.openshift.io", Kind: "Route"}
)

type RouteAnalyzer struct {
	e *eval.Evaluator
}

func (_ RouteAnalyzer) Supports(obj *status.Object) bool {
	return (obj.GroupVersionKind().GroupKind() ==
		schema.GroupKind{Group: "route.openshift.io", Kind: "Route"})
}

func (_ RouteAnalyzer) Analyze(ctx context.Context, obj *status.Object) status.ObjectStatus {
	var conditions []status.ConditionStatus

	ingresses, _, _ := unstructured.NestedSlice(obj.Unstructured.Object, "status", "ingress")

	for _, ingress := range ingresses {
		ingress, ok := ingress.(map[string]interface{})
		if !ok {
			continue
		}

		data, found, _ := unstructured.NestedSlice(ingress, "conditions")

		if found {
			c, err := analyze.AnalyzeRawConditions(data,
				[]analyze.ConditionAnalyzer{analyze.GenericConditionAnalyzer{
					Conditions: analyze.NewStringMatchers("Admitted"),
				}})
			if err != nil {
				return status.UnknownStatusWithError(obj, err)
			}
			conditions = append(conditions, c...)
		}
	}

	return analyze.AggregateResult(obj, nil, conditions)
}

func init() {
	analyze.Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return RouteAnalyzer{e: e}
	})
}
