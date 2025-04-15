package analyze_test

import (
	"context"
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
)

func TestPodAnalyzer(t *testing.T) {
	var os status.ObjectStatus
	ctx := context.Background()
	e, l, objs := test.TestEvaluator("pods.yaml")

	os = e.Eval(ctx, objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)

	l.RegisterPodLogs("default", "p2", "p2c", "Line 1\nLine 2\nLine 3\n")
	os = e.Eval(ctx, objs[1])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)

	test.AssertConditions(t, `PodReadyToStartContainers   (Unknown)
Initialized   (Unknown)
Ready ContainersNotReady containers with unready status: [p2c] (Error)
ContainersReady ContainersNotReady containers with unready status: [p2c] (Unknown)
PodScheduled   (Unknown)`, os.Conditions)

	test.AssertConditions(t, `Ready NotReady Logs:
Line 1
Line 2
Line 3
 (Error)`, os.SubStatuses[0].Conditions)
}
