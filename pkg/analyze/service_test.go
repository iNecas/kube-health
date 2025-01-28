package analyze_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
	"github.com/inecas/kube-health/pkg/print"
	"github.com/inecas/kube-health/pkg/status"
)

func TestServiceAnalyzer(t *testing.T) {
	var os status.ObjectStatus
	p := print.NewTreePrinter(print.PrintOptions{ShowOk: true})

	e, _, objs := test.TestEvaluator("services.yaml", "pods.yaml")

	os = e.Eval(objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)

	sb := &strings.Builder{}
	p.PrintStatuses([]status.ObjectStatus{os}, sb)
	test.AssertStr(t, `
OBJECT           CONDITION                       AGE    REASON
Ok default/Service/s1
└─ Ok Pod/p1
   │             PodReadyToStartContainers=True  24h
   │             Initialized=True                24h
   │             Ready=True                      24h
   │             ContainersReady=True            24h
   │             PodScheduled=True               24h
   └─ Ok Container/p1c
                 Running=True                    24h
`, sb.String())

	os = e.Eval(objs[1])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)

	sb = &strings.Builder{}
	p.PrintStatuses([]status.ObjectStatus{os}, sb)
	test.AssertStr(t, `
OBJECT           CONDITION                       AGE    REASON
Error default/Service/s2
└─ Error Pod/p2
   │             PodReadyToStartContainers=True  24h
   │             Initialized=True                24h
   │             (Error) Ready=False             24h    ContainersNotReady
   │               containers with unready status: [p2c]
   │             ContainersReady=False           24h    ContainersNotReady
   │             PodScheduled=True               24h
   └─ Error Container/p2c
                 (Error) Ready=True                     NotReady
`, sb.String())
}
