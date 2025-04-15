package analyze_test

import (
	"context"
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
)

func TestReplicaSetAnalyzer(t *testing.T) {
	var os status.ObjectStatus
	ctx := context.Background()
	e, _, objs := test.TestEvaluator("replicasets.yaml", "pods.yaml")

	os = e.Eval(ctx, objs[1])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)

	test.AssertConditions(t, `
ReplicasLabeled Unlabeled Labeled: 0/2 (Error)
ReplicasAvailable Unavailable Available: 0/2 (Error)
ReplicasReady NotReady Ready: 0/2 (Error)`, os.Conditions)
}
