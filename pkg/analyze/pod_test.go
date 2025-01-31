package analyze_test

import (
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
)

func TestPodAnalyzer(t *testing.T) {
	e, _, objs := test.TestEvaluator("pods.yaml")
	var os status.ObjectStatus

	os = e.Eval(objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)

	os = e.Eval(objs[1])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)

	test.AssertConditions(t, `PodReadyToStartContainers   (Ok)
Initialized   (Ok)
Ready ContainersNotReady containers with unready status: [p2c] (Error)
ContainersReady ContainersNotReady containers with unready status: [p2c] (Unknown)
PodScheduled   (Ok)`, os.Conditions)
}
