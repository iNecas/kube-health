package analyze_test

import (
	"fmt"
	"testing"

	"github.com/inecas/kube-health/internal/test"
	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"
)

const healthyNodePressureConditions = `MemoryPressure KubeletHasSufficientMemory kubelet has sufficient memory available (Ok)
DiskPressure KubeletHasNoDiskPressure kubelet has no disk pressure (Ok)
PIDPressure KubeletHasSufficientPID kubelet has sufficient PID available (Ok)`

func TestNodeAnalyzer(t *testing.T) {
	var os status.ObjectStatus
	e, _, objs := test.TestEvaluator("nodes.yaml")

	os = e.Eval(t.Context(), objs[0])
	assert.Equal(t, os.Status().Result, status.Error)
	expectedConditions := fmt.Sprintf("%s\n%s\n%s\n%s", healthyNodePressureConditions,
		"Ready KubeletNotReady test error message (Error)",
		"Terminating TerminationRequested The cloud provider has marked this instance for termination (Error)",
		"Unschedulable Unschedulable Node is marked as unschedulable (Error)",
	)
	test.AssertConditions(t, expectedConditions, os.Conditions)

	os = e.Eval(t.Context(), objs[1])
	assert.Equal(t, os.Status().Result, status.Ok)
	expectedConditions = fmt.Sprintf("%s\n%s", healthyNodePressureConditions, "Ready KubeletReady  (Ok)")
	test.AssertConditions(t, expectedConditions, os.Conditions)
}
