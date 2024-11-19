package monitor

import (
	"context"
	"time"

	"k8s.io/klog/v2"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

// StatusPoller polls the status of a set of objects at a regular interval.
type MonitorPoller struct {
	interval  time.Duration
	evaluator *eval.Evaluator
	cfg       Config
	eventChan chan TargetsStatusUpdate
}

func NewMonitorPoller(interval time.Duration, evaluator *eval.Evaluator, cfg Config) *MonitorPoller {
	return &MonitorPoller{
		interval:  interval,
		evaluator: evaluator,
		cfg:       cfg,
		eventChan: make(chan TargetsStatusUpdate),
	}
}

type TargetStatuses struct {
	Target   Target
	Statuses []status.ObjectStatus
}

type TargetsStatusUpdate struct {
	Statuses []TargetStatuses
}

func (t TargetsStatusUpdate) ToStatusUpdate() eval.StatusUpdate {
	statuses := make([]status.ObjectStatus, 0)
	for _, target := range t.Statuses {
		statuses = append(statuses, target.Statuses...)
	}
	return eval.StatusUpdate{
		Statuses: statuses,
	}
}

// Start starts the poller and returns a channel that will receive status updates.
// The poller will run until the context is canceled.
// The channel will be closed when the context is canceled.
func (s *MonitorPoller) Start(ctx context.Context) <-chan TargetsStatusUpdate {
	go func() {
		defer close(s.eventChan)
		// Initial run
		s.run()
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.interval):
				s.run()
			}
		}
	}()

	return s.eventChan
}

func (s *MonitorPoller) run() {
	// Reset the evaluator to clear the cache from previous run.
	s.evaluator.Reset()

	klog.V(1).Info("reloading health data")
	start := time.Now()

	statuses := make([]TargetStatuses, 0)
	for _, target := range s.cfg.Targets {
		querySpec := eval.KindQuerySpec{
			GK: eval.GroupKindMatcher{IncludedKinds: target.Kinds},
			Ns: expandNamespace(""),
			// TODO: add namespace support
			//Namespace: target.Namespace,
		}
		s, err := s.evaluator.EvalQuery(querySpec, nil)
		if err != nil {
			klog.ErrorS(err, "failed to evaluate query", "query", querySpec)
			continue
		}
		klog.V(3).InfoS("evaluated query", "query", querySpec, "objects", len(s))
		statuses = append(statuses, TargetStatuses{Target: target, Statuses: s})
	}

	klog.V(1).InfoS("health data reloaded", "duration", time.Since(start))

	s.eventChan <- TargetsStatusUpdate{
		Statuses: statuses,
	}
}

func expandNamespace(ns string) string {
	if ns == "" {
		return eval.NamespaceAll
	}
	return ns
}
