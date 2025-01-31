package analyze_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
	"github.com/inecas/kube-health/pkg/print"
	"github.com/inecas/kube-health/pkg/status"
)

func TestDeploymentAnalyzer(t *testing.T) {
	var os status.ObjectStatus
	p := print.NewTreePrinter(print.PrintOptions{ShowOk: true})

	e, l, objs := test.TestEvaluator("pods.yaml", "replicasets.yaml", "deployments.yaml")

	os = e.Eval(objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)

	// TODO: fix issues with AGE
	sb := &strings.Builder{}
	p.PrintStatuses([]status.ObjectStatus{os}, sb)
	test.AssertStr(t, `
OBJECT           CONDITION                       AGE    REASON
Ok default/Pod/p1
│                PodReadyToStartContainers=True  1232h
│                Initialized=True                2880h
│                Ready=True                      1232h
│                ContainersReady=True            1232h
│                PodScheduled=True               2880h
└─ Ok Container/p1c
                 Running=True                    1232h`, sb.String())

	l.RegisterPodLogs("default", "p2", "p2c", "Line 1\nLine 2\nLine 3\n")
	os = e.Eval(objs[1])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)

	sb = &strings.Builder{}
	p.PrintStatuses([]status.ObjectStatus{os}, sb)

	test.AssertStr(t, `
OBJECT           CONDITION                       AGE    REASON
Error default/Pod/p2
│                PodReadyToStartContainers=True  77h
│                Initialized=True                77h
│                (Error) Ready=False             77h    ContainersNotReady
│                  containers with unready status: [p2c]
│                ContainersReady=False           77h    ContainersNotReady
│                PodScheduled=True               77h
└─ Error Container/p2c
                 (Error) Ready=True                     NotReady
                   Logs:
                   Line 1
                   Line 2
                   Line 3`, sb.String())
}
