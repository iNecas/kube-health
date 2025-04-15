package eval

import (
	"context"
	"time"

	"github.com/inecas/kube-health/pkg/status"
)

// StatusPoller polls the status of a set of objects at a regular interval.
type StatusPoller struct {
	interval  time.Duration
	evaluator *Evaluator
	objects   []*status.Object
	eventChan chan StatusUpdate
}

func NewStatusPoller(interval time.Duration, evaluator *Evaluator, objects []*status.Object) *StatusPoller {
	return &StatusPoller{
		interval:  interval,
		evaluator: evaluator,
		objects:   objects,
		eventChan: make(chan StatusUpdate),
	}
}

type StatusUpdate struct {
	Statuses []status.ObjectStatus
	Error    error
}

// Start starts the poller and returns a channel that will receive status updates.
// The poller will run until the context is canceled.
// The channel will be closed when the context is canceled.
func (s *StatusPoller) Start(ctx context.Context) <-chan StatusUpdate {
	go func() {
		defer close(s.eventChan)
		// Initial run
		s.run(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.interval):
				s.run(ctx)
			}
		}
	}()

	return s.eventChan
}

func (s *StatusPoller) run(ctx context.Context) {
	// Reset the evaluator to clear the cache from previous run.
	s.evaluator.Reset()

	statuses := make([]status.ObjectStatus, 0, len(s.objects))
	for _, obj := range s.objects {
		statuses = append(statuses, s.evaluator.Eval(ctx, obj))
	}

	s.eventChan <- StatusUpdate{
		Statuses: statuses,
	}
}
