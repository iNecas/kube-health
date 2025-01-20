package print

import (
	"fmt"
	"io"
	"strings"

	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

// PeriodicPrinter prints status updates to the terminal, as they arrive
// to the update channel.
// It tracks the number of lines printed and clears the screen before printing
// the next update.
type PeriodicPrinter struct {
	printer       StatusPrinter
	out           OutStreams
	previousLines int
	updateChan    <-chan eval.StatusUpdate
	callback      func([]status.ObjectStatus)
}

type lineCountWriter struct {
	w     io.Writer
	lines int
}

func (lcw *lineCountWriter) Write(p []byte) (n int, err error) {
	n, err = lcw.w.Write(p)
	var sb strings.Builder
	sb.Write(p[:n])
	lcw.lines += strings.Count(sb.String(), "\n")

	return n, err
}

func NewPeriodicPrinter(printer StatusPrinter, out OutStreams, updateChan <-chan eval.StatusUpdate,
	callback func([]status.ObjectStatus)) *PeriodicPrinter {
	return &PeriodicPrinter{
		printer:    printer,
		out:        out,
		updateChan: updateChan,
		callback:   callback,
	}
}

func (p *PeriodicPrinter) Start() {
	for update := range p.updateChan {
		if update.Error != nil {
			fmt.Fprintf(p.out.Err, "Error: %s", update.Error)
			p.previousLines = 0
		}
		p.resetScreen()

		// Wrap writer to count number of emitted lines.
		lcw := &lineCountWriter{w: p.out.Std}
		p.printer.PrintStatuses(update.Statuses, lcw)
		p.previousLines = lcw.lines

		if p.callback != nil {
			p.callback(update.Statuses)
		}
	}
}

func (p *PeriodicPrinter) resetScreen() {
	for i := 0; i < p.previousLines; i++ {
		p.moveUp()
		p.eraseCurrentLine()
	}
}

func (p *PeriodicPrinter) moveUp() {
	fmt.Fprintf(p.out.Std, "%c[%dA", ESC, 1)
}

func (p *PeriodicPrinter) eraseCurrentLine() {
	fmt.Fprintf(p.out.Std, "%c[2K\r", ESC)
}
