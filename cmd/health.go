package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/util/term"

	"github.com/inecas/kube-health/pkg/analyze"
	// Extra analyzers for Red Hat related projects.
	_ "github.com/inecas/kube-health/pkg/analyze/redhat"
	"github.com/inecas/kube-health/pkg/eval"
	"github.com/inecas/kube-health/pkg/print"
	"github.com/inecas/kube-health/pkg/status"
)

var (
	exitCode int
	Version  = "dev"
	Commit   = "dev"
	Date     = "n/a"
)

func Execute() {
	klog.InitFlags(nil)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	flags := newFlags()

	cmd := &cobra.Command{
		Use:          "kube-health",
		Short:        "Monitor Kubernetes resource health",
		SilenceUsage: true,
		RunE:         runFunc(flags),
	}

	flags.addFlags(cmd)
	if err := cmd.Execute(); err != nil {
		os.Exit(128)
	}
	os.Exit(exitCode)
}

type flags struct {
	waitForever  bool
	waitProgress bool
	waitOk       bool
	showGroup    bool
	showOk       bool
	printVersion bool
	width        int
	configFlags  *genericclioptions.ConfigFlags
	printFlags   *genericclioptions.PrintFlags
}

func newFlags() *flags {
	return &flags{
		configFlags: genericclioptions.NewConfigFlags(true),
		printFlags:  genericclioptions.NewPrintFlags("").WithDefaultOutput("tree+color"),
	}
}

func (f *flags) addFlags(cmd *cobra.Command) {
	fl := cmd.PersistentFlags()
	f.configFlags.AddFlags(fl)
	f.addPrintFlags(cmd)

	fs := pflag.NewFlagSet("options", pflag.ExitOnError)
	fs.BoolVarP(&f.waitProgress, "wait-progress", "W", false,
		"Wait until resources finish progressing (regarless of the result)")
	fs.BoolVarP(&f.waitOk, "wait-ok", "O", false,
		"Wait until the resources are ready (success only)")
	fs.BoolVarP(&f.waitForever, "wait-forever", "F", false,
		"Wait forever")
	fs.BoolVarP(&f.showGroup, "show-group", "G", false,
		"For each object, show API group it belongs to")
	fs.BoolVarP(&f.showOk, "show-healthy", "H", false,
		"Show details for all objects, including those with OK status")
	fs.IntVar(&f.width, "width", -1,
		"Width of the output. By default, it's inferred from the terminal width. Set to 0 to disable wrapping")
	fs.BoolVar(&f.printVersion, "version", false, "Print version information")
	fl.AddFlagSet(fs)
}

// AddFlags receives a *cobra.Command reference and binds
// flags related to JSON/Yaml/Name/Template printing to it
func (f *flags) addPrintFlags(cmd *cobra.Command) {
	f.printFlags.JSONYamlPrintFlags.AddFlags(cmd)
	f.printFlags.TemplatePrinterFlags.AddFlags(cmd)

	allowedFormats := append([]string{"tree", "tree+color"}, f.printFlags.AllowedFormats()...)

	if f.printFlags.OutputFormat != nil {
		cmd.Flags().StringVarP(f.printFlags.OutputFormat, "output", "o", *f.printFlags.OutputFormat,
			fmt.Sprintf(`Output format. One of: (%s).`, strings.Join(allowedFormats, ", ")))
		if f.printFlags.OutputFlagSpecified == nil {
			f.printFlags.OutputFlagSpecified = func() bool {
				return cmd.Flag("output").Changed
			}
		}
	}
}

func (f *flags) printOpts() print.PrintOptions {
	termWidth := f.width
	if termWidth < 0 {
		termsize := term.GetSize(os.Stdout.Fd())
		if termsize != nil {
			termWidth = int(termsize.Width)
		}
	}
	po := print.PrintOptions{
		ShowGroup: f.showGroup,
		ShowOk:    f.showOk,
		Width:     termWidth,
	}

	if strings.Contains(*f.printFlags.OutputFormat, "+color") {
		po.Color = true
	}

	return po
}

func (f *flags) toPrinter() (print.StatusPrinter, error) {
	switch *f.printFlags.OutputFormat {
	case "tree", "tree+color":
		return print.NewTreePrinter(f.printOpts()), nil
	default:
		kubectlPrinter, err := f.printFlags.ToPrinter()
		if err != nil {
			return nil, err
		}
		return print.KubectlPrinter{Printer: kubectlPrinter}, nil
	}
}

func runFunc(fl *flags) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, posArgs []string) error {
		if fl.printVersion {
			PrintVersion()
			return nil
		}
		if len(posArgs) == 0 {
			return fmt.Errorf("no resources specified")
		}

		filenameOpts := &resource.FilenameOptions{}
		if len(posArgs) == 1 && posArgs[0] == "-" {
			filenameOpts.Filenames = []string{"-"}
			posArgs = nil
		}

		f := util.NewFactory(fl.configFlags)

		namespace, explicitNamespace, err := f.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return err
		}

		resources := make([]*resource.Info, 0)
		objects := make([]*status.Object, 0)

		resource.NewBuilder(fl.configFlags).
			Unstructured().
			NamespaceParam(namespace).DefaultNamespace().
			ResourceTypeOrNameArgs(true, posArgs...).
			FilenameParam(explicitNamespace, filenameOpts).
			Flatten().
			ContinueOnError().
			Do().
			Visit(func(info *resource.Info, err error) error {
				if err != nil {
					return err
				}
				resources = append(resources, info)

				unst, ok := info.Object.(*unstructured.Unstructured)
				if !ok {
					return fmt.Errorf("expected *unstructured.Unstructured, got %T", info.Object)
				}

				obj, err := status.NewObjectFromUnstructured(unst)
				if err != nil {
					return err
				}
				objects = append(objects, obj)
				return nil
			})

		ctx := cmd.Context()
		ctx, cancelFunc := context.WithCancel(ctx)
		defer cancelFunc()

		evaluator, err := eval.NewEvaluator(ctx, analyze.DefaultAnalyzers(), f)
		if err != nil {
			return err
		}

		poller := eval.NewStatusPoller(2*time.Second, evaluator, objects)
		updatesChan := poller.Start(ctx)

		printer, err := fl.toPrinter()
		if err != nil {
			return fmt.Errorf("Can't create printer: %w", err)
		}

		outStreams := print.OutStreams{
			Std: cmd.OutOrStdout(),
			Err: cmd.ErrOrStderr(),
		}

		wf := waitFunction(fl, cancelFunc)
		print.NewPeriodicPrinter(printer, outStreams, updatesChan, wf).Start()

		return nil
	}
}

// waitFunction decides when to stop waiting for the resources.
// It's used by the PeriodicPrinter to decide when to stop the loop.
func waitFunction(fl *flags, cancelFunc func()) func([]status.ObjectStatus) {
	return func(statuses []status.ObjectStatus) {
		if fl.waitForever {
			return
		}

		finish := func() {
			setExitCode(statuses)
			cancelFunc()
		}

		progressing := false
		if fl.waitProgress || fl.waitOk {
			for _, os := range statuses {
				// Consider the unknown status as progressing as well.
				if os.ObjStatus.Progressing || os.ObjStatus.Result == status.Unknown {
					progressing = true
				}
			}
		}

		if fl.waitProgress {
			if !progressing {
				finish()
			}
			return
		}

		if fl.waitOk {
			if progressing {
				return
			}

			ready := true
			for _, os := range statuses {
				if os.Status().Result != status.Ok {
					ready = false
				}
			}
			if ready {
				finish()
			}
			return
		}

		finish()
	}
}

func setExitCode(statuses []status.ObjectStatus) {
	exitCode = 0
	for _, os := range statuses {
		res := os.Status().Result

		switch res {
		case status.Unknown:
			exitCode = 3
			break
		case status.Error:
			exitCode = max(exitCode, 2)
		case status.Warning:
			exitCode = max(exitCode, 1)
		case status.Ok:
			exitCode = max(exitCode, 0)
		}
	}

	for _, os := range statuses {
		if os.Status().Progressing {
			// Add 4th bit to the exit code if still progressing
			exitCode = exitCode | 0b1000
		}
	}
}

func PrintVersion() {
	fmt.Printf("kube-health %s (commit %s, built at %s)\n", Version, Commit, Date)
}
