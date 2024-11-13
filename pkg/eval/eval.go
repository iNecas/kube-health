package eval

import (
	"context"

	"k8s.io/apimachinery/pkg/types"

	"github.com/inecas/kube-health/pkg/status"
)

// Analyzer calculates status for the object.
type Analyzer interface {
	Analyze(obj *status.Object) status.ObjectStatus
	// Supports should return true if the particular analyzer supports
	// the given resource.
	//
	// It's used when searching the appropriate analyzer in the register.
	Supports(obj *status.Object) bool
}

// AnalyzerInit is a function that initializes an Analyzer and can
// optionally pass an Evaluator reference to it.
type AnalyzerInit func(*Evaluator) Analyzer

// Evaluator is the entry structure for the status evaluation cycle.
//
// It peformes the following steps:
//   - Loading fresh data for the object (though the Loader struct).
//   - Finding an appropriate Analyzer for the object.
//   - Evaluating the Analyzer on the object.
type Evaluator struct {
	analyzers      []Analyzer
	loader         *Loader
	analyzersCache map[types.UID]Analyzer
	ctx            context.Context
}

// NewEvaluator creates a new Evaluator instance.
func NewEvaluator(ctx context.Context, analyzerInits []AnalyzerInit, config RESTClientGetter) (*Evaluator, error) {
	loader, err := NewLoader(config)
	if err != nil {
		return nil, err
	}

	evaluator := &Evaluator{
		loader:         loader,
		analyzersCache: make(map[types.UID]Analyzer),
		ctx:            ctx,
	}

	// Initialize the analyzers.
	analyzers := make([]Analyzer, 0, len(analyzerInits))
	for _, init := range analyzerInits {
		analyzers = append(analyzers, init(evaluator))
	}
	evaluator.analyzers = analyzers
	return evaluator, nil
}

// Reset clears the cache of the evaluator.
func (e *Evaluator) Reset() {
	e.loader.reset()
}

// Evaluates the status of the object. It gets the most recent version
// of the object and runs the appropriate analyzer on it.
func (e *Evaluator) Eval(obj *status.Object) status.ObjectStatus {
	analyzer := e.findAnalyzer(obj)

	obj, err := e.loader.Get(e.ctx, obj)
	if err != nil {
		return status.UnknownStatusWithError(obj, err)
	}

	return analyzer.Analyze(obj)
}

// EvalQuery loads the objects specified by the query and runs the analyzer.
// If the analyzer is not provided, it tries to find the appropriate one
// in the register.
func (e *Evaluator) EvalQuery(q QuerySpec, analyzer Analyzer) ([]status.ObjectStatus, error) {
	objects, err := e.Load(q)
	if err != nil {
		return nil, err
	}

	var ret []status.ObjectStatus
	for _, obj := range objects {
		a := analyzer
		if a == nil {
			a = e.findAnalyzer(obj)
		}
		ret = append(ret, a.Analyze(obj))
	}
	return ret, nil
}

// Load loads the objects specified by the query.
func (e *Evaluator) Load(q QuerySpec) ([]*status.Object, error) {
	objects, err := e.loader.Load(e.ctx, q)
	if err != nil {
		return nil, err
	}

	return objects, nil
}

func (e *Evaluator) findAnalyzer(obj *status.Object) Analyzer {
	for _, analyzer := range e.analyzers {
		if analyzer.Supports(obj) {
			e.analyzersCache[obj.UID] = analyzer
			return analyzer
		}
	}
	return nil
}
