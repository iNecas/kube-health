package analyze_test

import (
	"context"
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
)

func TestPvcAnalyzer(t *testing.T) {
	var os status.ObjectStatus
	ctx := context.Background()
	e, _, objs := test.TestEvaluator("pvcs.yaml")

	os = e.Eval(ctx, objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)
	test.AssertConditions(t, `Bound  PVC is bound. (Ok)`, os.Conditions)

	os = e.Eval(ctx, objs[1])
	assert.True(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Unknown)

	test.AssertConditions(t, `NotBound Available PVC is not bound. (Unknown)`, os.Conditions)
}
