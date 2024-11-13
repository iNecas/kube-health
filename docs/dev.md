## Project Structure

The code of the project is split into the following packages:

- `cmd/status` - entry-point to the CLI
- `pkg/status` - common type definitions
- `pkg/analyze` - logic for health evaluation of various resources
- `pkg/eval` - glue code for loading data from Kubernetes and evaluating the analyzers
- `pkg/print` - code for printing the results.

## Data types

`kube-health` works with the following set of structs:

```go
// Basic unit of status
type Status struct {
	Result      Result // one of (Ok, Warning, Error, Unknown)
	Progressing bool   // true if the object is still progressing
	Status      string // human readable status
	Err         error  // error appeared during the evaluation
}

// Mapping from a k8s object to its status details
type ObjectStatus struct {
	Object      *Object           // the subject of the status
	ObjStatus   Status            // overall status of the object
	SubStatuses []ObjectStatus    // statuses of the sub-objects (optional)
	Conditions  []ConditionStatus // conditions of the object (optional)
}

// Mapping of metav1.Condition to it's status
type ConditionStatus struct {
	*metav1.Condition  // condition struct as defined in Kubernetes meta api.
	// CondStatus is a pointer to the underlying condition status.
	// We're using the pointer to allow modifying the status.
	CondStatus *Status
}
```


## Analyzers

The purpose of an analyzer is to provide mapping from an `Object` to `ObjectStatus`. It needs to implement `eval.Analyzer` interface. The sections below describe the most common approaches. For more details, we advise looking at the implementation of available analyzers.


## Simple Analyzer

Here's a very simple example of a custom analyzer that uses just basic data from the object:

```go
import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

type MyAnalyzer struct{}

// Supports gets called for each object to determine if the analyzer can handle it.
func (_ MyAnalyzer) Supports(obj *status.Object) bool {
	return (obj.GroupVersionKind().GroupKind() ==
		schema.GroupKind{Group: "mygroup.example.org", Kind: "MyResource"})
}

// Analyze gets called for each object supported by the analyzer.
func (_ MyAnalyzer) Analyze(obj *status.Object) status.ObjectStatus {
	myresult, found, err := unstructured.NestedString(obj.Unstructured.Object, "status", "myresult")

	// If evaluation fails, use UnknownStatusWithError.
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	// If you can't determine the status, use UnknownStatus.
	if !found {
		return status.UnknownStatus(obj)
	}

	if myresult != "ok" {
		// The best way to indicate a state for an object is via conditions.
		// If the resource doesn't provide conditions, it's possible to create a synthetic one.
		return analyze.AggregateResult(obj, nil, []status.ConditionStatus{
			analyze.SyntheticConditionError("MyResultFailed", myresult, "MyResult is not ok")})
	}
	
	// Indicate OK status of the object directly.
	return status.OkStatus(obj, nil)
}

func init() {
	// Make the evaluator aware of the new analyzer.
	// We use the RegisterSimple helper, as we don't need to pass any additional configuration.
	analyze.Register.RegisterSimple(MyAnalyzer{})
}
```

## Complex Analyzer

In more complex (and common) scenario, the analyzed resource provides `conditions`
to analyze and can be composed of multiple sub-resources:

```go
import (
	"github.com/inecas/kube-health/pkg/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/eval"
)

type MyAnalyzer struct {
	// Keep a reference to the evaluator to analyze sub-objects.
	e *eval.Evaluator
}

func (_ MyAnalyzer) Supports(obj *status.Object) bool {
	return (obj.GroupVersionKind().GroupKind() ==
		schema.GroupKind{Group: "mygroup.example.org", Kind: "MyResource"})
}

func (a MyAnalyzer) Analyze(obj *status.Object) status.ObjectStatus {
	// Evaluate sub-objects based on owner references.
	subStatuses, err := a.e.EvalQuery(analyze.GenericOwnerQuerySpec(obj), nil)

	// The GenericConditionAnalyzer looks at presence of specific conditions.
	// By default it considers True conditions to be Ok, unless they are listed
	// in ReversedPolarityTypes.
	myConditionsAnalyzer := analyze.GenericConditionAnalyzer{
		Types:                 []string{"WeAreTheChampions"},
		ReversedPolarityTypes: []string{"UnderPressure"},
	}

	// AnalyzeObjectConditions analyzes conditions in `status.conditions` field.
	conditions, err := analyze.AnalyzeObjectConditions(obj, append(
		[]analyze.ConditionAnalyzer{
			myConditionsAnalyzer,
			// To implement more complex condition analysis, you can implement
			// implement analyze.ConditionAnalyzer interface.
			myCustomConditionsAnalyzer{},
		},
		// Common set of generic condition analyzers (e.g. Ready).
		analyze.DefaultConditionAnalyzers...))

	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	// Use AggregateResult to combine status from subStatuses and conditions.
	return analyze.AggregateResult(obj, subStatuses, conditions)
}

// custom condition analyzer
type myCustomConditionsAnalyzer struct{}

func (_ myCustomConditionsAnalyzer) Analyze(cond *metav1.Condition) status.ConditionStatus {
	if cond.Type == "DontStopMeNow" && cond.Status == metav1.ConditionFalse {
		return analyze.ConditionStatusProgressing(cond)
	}

	// Indicate this analyzer has not matched the condition: let it analyze by other
	// analyzers in the queue.
	return analyze.ConditionStatusNoMatch
}

func init() {
	// Since we need to evaluate the sub-resources, pass the Evaluator reference
	analyze.Register.Register(func(e *eval.Evaluator) eval.Analyzer {
		return MyAnalyzer{e: e}
	})
}
```

## Sub-objects status evaluation

There are multiple ways to find sub-objects of an object. Each can be represented
by implementing a `eval.QuerySpec` interface. Current available implementations are:

- `OwnerQuerySpec` - find sub-objects referenced via `ownerReference`
- `LabelQuerySpec` - find sub-objects referenced via selectors (e.g. in `Deployment` or `Service`)
- `RefQuerySpec` - find sub-objects via a generic reference
- `PodLogQuerySpec` - find logs for a pod: for consistency reasons, we model the logs as special kind of objects, so that they fit into the rest of the model

The health of the sub-objects can be evaluated via `EvalQuery` method of the `Evaluator`. It accepts:
- An instance of the desired query spec to find the desired objects.
- Optionally: and analyzer to run against found objects. If `nil`, it tries to find suitable analyzer in the register.

In order to load the sub-objects without running the analyzers, one can use `Evaluator`'s `Load` method.
