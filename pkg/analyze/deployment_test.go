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

	e, l, objs := test.TestEvaluator("deployments.yaml", "pods.yaml", "replicasets.yaml")

	os = e.Eval(objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)

	sb := &strings.Builder{}
	p.PrintStatuses([]status.ObjectStatus{os}, sb)
	test.AssertStr(t, `
OBJECT           CONDITION                       AGE    REASON
Ok default/Deployment/dp1
│                Available=True                  24h    MinimumReplicasAvailable
│                Progressing=True                24h    NewReplicaSetAvailable
└─ Ok ReplicaSet/rs1
   │             ReplicasReady=True                     Ready
   └─ Ok Pod/p1
      │          PodReadyToStartContainers=True  24h
      │          Initialized=True                24h
      │          Ready=True                      24h
      │          ContainersReady=True            24h
      │          PodScheduled=True               24h
      └─ Ok Container/p1c
                 Running=True                    24h
	`, sb.String())

	l.RegisterPodLogs("default", "p2", "p2c", "Line 1\nLine 2\nLine 3\n")
	os = e.Eval(objs[1])
	assert.True(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)

	sb = &strings.Builder{}
	p.PrintStatuses([]status.ObjectStatus{os}, sb)

	test.AssertStr(t, `
OBJECT           CONDITION                       AGE    REASON
Progressing default/Deployment/dp2
│                Available=True                  24h    MinimumReplicasAvailable
│                Progressing=True                24h    NewReplicaSetAvailable
│                  zorg
└─ Error ReplicaSet/rs2
   │             (Error) ReplicasLabeled=False          Unlabeled
   │               Labeled: 0/2
   │             (Error) ReplicasAvailable=Fals         Unavailable
   │               Available: 0/2
   │             (Error) ReplicasReady=False            NotReady
   │               Ready: 0/2
   └─ Error Pod/p2
      │          PodReadyToStartContainers=True  24h
      │          Initialized=True                24h
      │          (Error) Ready=False             24h    ContainersNotReady
      │            containers with unready status: [p2c]
      │          ContainersReady=False           24h    ContainersNotReady
      │          PodScheduled=True               24h
      └─ Error Container/p2c
                 (Error) Ready=True                     NotReady
                   Logs:
                   Line 1
                   Line 2
                   Line 3
`, sb.String())
}
