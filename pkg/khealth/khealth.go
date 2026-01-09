// Package khealth provides a way to use kube-health
// as a library. The main usage is to create a new instance
// of `eval.Evaluator` that can then be used programmatically to
// evaluate the health of Kubernetes resources.
package khealth

import (
	"fmt"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/eval"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
)

// NewHealthEvaluator creates a new kube-health evaluator using the provided rest.Config.
// If nil is passed, the in-cluster configuration will be used by default.
func NewHealthEvaluator(restConfig *rest.Config) (*eval.Evaluator, error) {
	cf := genericclioptions.NewConfigFlags(true)

	if restConfig != nil {
		cf.WrapConfigFn = func(*rest.Config) *rest.Config {
			return restConfig
		}
	} else {
		inClusterConf, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
		cf.WrapConfigFn = func(*rest.Config) *rest.Config {
			return inClusterConf
		}
	}

	ldr, err := eval.NewRealLoader(cf)
	if err != nil {
		return nil, fmt.Errorf("can't create kube-health loader: %w", err)
	}
	return eval.NewEvaluator(analyze.DefaultAnalyzers(), ldr), nil
}
