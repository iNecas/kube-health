package print

import (
	"io"

	"github.com/inecas/kube-health/pkg/status"
)

type PrintOptions struct {
	ShowGroup bool // By default, group names are not shown.
	ShowOk    bool // By default, OK statuses are not shown.
	Width     int  // Width of the output. If 0, wrapping is disabled.
	Color     bool // Use colors to indicate the health.
}

type OutStreams struct {
	Std io.Writer
	Err io.Writer
}

// StatusPrinter is an interface for printing status updates.
type StatusPrinter interface {
	PrintStatuses(statuses []status.ObjectStatus, w io.Writer)
}
