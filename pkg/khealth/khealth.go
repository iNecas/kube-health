package khealth

import (
	"fmt"
	"log/slog"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/eval"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

// NewHealthEvaluator creates a new kube-health evaluator using the provided rest.Config.
// If nil is passed, the in-cluster configuration will be used by default.
func NewHealthEvaluator(restConfig *rest.Config) (*eval.Evaluator, error) {
	var restClientGetter *restClientGETTER
	if restConfig != nil {
		restClientGetter = newRESTClientGETTER(restConfig)
	} else {
		config, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
		slog.Info("Using inClusterConfig")
		restClientGetter = newRESTClientGETTER(config)
	}

	ldr, err := eval.NewRealLoader(restClientGetter)
	if err != nil {
		return nil, fmt.Errorf("can't create kube-health loader: %w", err)
	}
	return eval.NewEvaluator(analyze.DefaultAnalyzers(), ldr), nil
}

func newRESTClientGETTER(config *rest.Config) *restClientGETTER {
	return &restClientGETTER{
		rConfig: config,
	}
}

// restClientGETTER is a wrapper around
// provided rest.Config
type restClientGETTER struct {
	rConfig *rest.Config
}

func (r *restClientGETTER) ToRESTConfig() (*rest.Config, error) {
	return r.rConfig, nil
}

func (r *restClientGETTER) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.rConfig)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(discoveryClient), nil
}

func (r *restClientGETTER) ToRESTMapper() (meta.RESTMapper, error) {
	cli, err := r.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}

	deferredRESTMAPPER := restmapper.NewDeferredDiscoveryRESTMapper(cli)
	return deferredRESTMAPPER, nil
}
