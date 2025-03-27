package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/cmd/util"

	healthcmd "github.com/inecas/kube-health/cmd"
	"github.com/inecas/kube-health/pkg/analyze"

	// Extra analyzers for Red Hat related projects.
	_ "github.com/inecas/kube-health/pkg/analyze/redhat"
	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/monitor"
	"github.com/inecas/kube-health/pkg/print"
	"github.com/inecas/kube-health/pkg/status"
)

func Execute() {
	klog.InitFlags(nil)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	flags := newFlags()

	cmd := &cobra.Command{
		Use:          "kube-health-monitor",
		Short:        "Monitor Kubernetes resource status and expose it via Prometheus",
		SilenceUsage: true,
		RunE:         runFunc(flags),
	}

	flags.addFlags(cmd.Flags())
	cmd.MarkFlagFilename("config", "yaml", "yml")
	cmd.MarkFlagRequired("config")
	if err := cmd.Execute(); err != nil {
		os.Exit(128)
	}
}

type flags struct {
	printVersion bool
	configFile   string
	configFlags  *genericclioptions.ConfigFlags
	printOnly    bool
	interval     int // refresh interval in seconds
	host         string
	port         int
}

func newFlags() *flags {
	return &flags{
		configFlags: genericclioptions.NewConfigFlags(true),
		interval:    30,
		host:        "localhost",
		port:        8080,
	}
}

func (f *flags) addFlags(fl *pflag.FlagSet) {
	f.configFlags.AddFlags(fl)

	fs := pflag.NewFlagSet("options", pflag.ExitOnError)
	fs.StringVarP(&f.configFile, "config", "c", f.configFile, "Path to monitor configuration file")
	fs.BoolVar(&f.printVersion, "version", false, "Print version information")
	fs.BoolVar(&f.printOnly, "print-only", false, "Print the status and exit")
	fs.IntVarP(&f.interval, "interval", "i", f.interval, "Refresh interval in seconds")
	fs.StringVar(&f.host, "host", f.host, "Host to bind the server to")
	fs.IntVar(&f.port, "port", f.port, "Port to bind the server to")
	fl.AddFlagSet(fs)
}

func runFunc(fl *flags) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, posArgs []string) error {
		if fl.printVersion {
			healthcmd.PrintVersion()
			return nil
		}

		f := util.NewFactory(fl.configFlags)

		mapper, err := f.ToRESTMapper()
		if err != nil {
			return err
		}

		cfg, err := monitor.ReadConfig(mapper, fl.configFile)
		if err != nil {
			return err
		}

		ctx := cmd.Context()
		ctx, cancelFunc := context.WithCancel(ctx)
		defer cancelFunc()

		ldr, err := eval.NewRealLoader(f)
		if err != nil {
			return fmt.Errorf("Can't create loader: %w", err)
		}

		evaluator := eval.NewEvaluator(analyze.DefaultAnalyzers(), ldr)

		interval := time.Duration(fl.interval) * time.Second
		poller := monitor.NewMonitorPoller(interval, evaluator, cfg)

		klog.V(1).InfoS("starting poller", "interval", interval)
		updatesChan := poller.Start(ctx)
		dedupUpdatesChan := dedupFilter(updatesChan)

		if fl.printOnly {
			fl.printStatus(ctx, cmd, printerAdapter(dedupUpdatesChan), cancelFunc)
			return nil
		}

		err = fl.startServer(ctx, dedupUpdatesChan)
		if err != nil {
			return err
		}

		return nil
	}
}

func (fl *flags) printStatus(ctx context.Context, cmd *cobra.Command, updatesChan <-chan eval.StatusUpdate,
	cancelFunc func()) {

	printOpts := print.PrintOptions{
		ShowOk: true,
	}

	printer := print.NewTreePrinter(printOpts)
	outStreams := print.OutStreams{
		Std: cmd.OutOrStdout(),
		Err: cmd.ErrOrStderr(),
	}
	wf := waitFunction(fl, cancelFunc)
	print.NewPeriodicPrinter(printer, outStreams, updatesChan, wf).Start()
}

func (fl *flags) startServer(ctx context.Context, updatesChan <-chan monitor.TargetsStatusUpdate) error {
	klog.V(1).InfoS("starting metrics server", "host", fl.host, "port", fl.port)
	server := monitor.NewSimpleServer(fl.host, fl.port)
	exporter := monitor.NewExporter(updatesChan, server,
		"kube:health", "Kubernetes objects health status")

	return exporter.Start(ctx)
}

func dedupFilter(updateChan <-chan monitor.TargetsStatusUpdate) <-chan monitor.TargetsStatusUpdate {
	// TODO: added deduplicate option per category in monitoring config - we don't
	// always want to support this.
	outChan := make(chan monitor.TargetsStatusUpdate)
	go func() {
		defer close(outChan)
		for update := range updateChan {
			outChan <- dedup(update)
		}
	}()
	return outChan
}

func dedup(update monitor.TargetsStatusUpdate) monitor.TargetsStatusUpdate {
	seen := make(map[string]struct{})
	var targetStatuses []monitor.TargetStatuses
	for _, target := range update.Statuses {
		for _, s := range target.Statuses {
			for _, id := range subObjectsIDs(s) {
				seen[id] = struct{}{}
			}
		}
	}

	for _, target := range update.Statuses {
		var statuses []status.ObjectStatus

		for _, s := range target.Statuses {
			// Only add the status if the object or any of its sub-objects wasn't seen yet.
			if _, found := seen[string(s.Object.UID)]; !found {
				statuses = append(statuses, s)
			}
		}

		targetStatuses = append(targetStatuses, monitor.TargetStatuses{
			Target:   target.Target,
			Statuses: statuses,
		})
	}

	return monitor.TargetsStatusUpdate{Statuses: targetStatuses}
}

func printerAdapter(updateChan <-chan monitor.TargetsStatusUpdate) <-chan eval.StatusUpdate {
	outChan := make(chan eval.StatusUpdate)
	go func() {
		defer close(outChan)
		for update := range updateChan {
			outChan <- update.ToStatusUpdate()
		}
	}()
	return outChan
}

func subObjectsIDs(obj status.ObjectStatus) []string {
	var ids []string
	subStatuses := obj.SubStatuses

	for len(subStatuses) > 0 {
		var nextSubStatuses []status.ObjectStatus
		for _, sub := range subStatuses {
			ids = append(ids, string(sub.Object.UID))
			nextSubStatuses = append(nextSubStatuses, sub.SubStatuses...)
		}
		subStatuses = nextSubStatuses
	}

	return ids
}

// waitFunction decides when to stop waiting for the resources.
// It's used by the PeriodicPrinter to decide when to stop the loop.
func waitFunction(fl *flags, cancelFunc func()) func([]status.ObjectStatus) {
	return func(statuses []status.ObjectStatus) {
		cancelFunc()
	}
}

func main() {
	Execute()
}
