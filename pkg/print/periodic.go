package print

import (
	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/status"
)

// PeriodicPrinter prints status updates to the terminal, as they arrive
// to the update channel.
// It tracks the number of lines printed and clears the screen before printing
// the next update.
type PeriodicPrinter struct {
	printer       StatusPrinter
	previousLines int
	updateChan    <-chan eval.StatusUpdate
	callback      func([]status.ObjectStatus)
}

func NewPeriodicPrinter(printer StatusPrinter, updateChan <-chan eval.StatusUpdate,
	callback func([]status.ObjectStatus)) *PeriodicPrinter {
	return &PeriodicPrinter{
		printer:    printer,
		updateChan: updateChan,
		callback:   callback,
	}
}

func (p *PeriodicPrinter) Start() {
	for update := range p.updateChan {
		p.resetScreen()
		if update.Error != nil {
			p.printer.PrintError(update.Error)
			p.previousLines = 0
		}
		p.previousLines = p.printer.PrintStatuses(update.Statuses)
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
	p.printer.Printf("%c[%dA", ESC, 1)
}

func (p *PeriodicPrinter) eraseCurrentLine() {
	p.printer.Printf("%c[2K\r", ESC)
}
