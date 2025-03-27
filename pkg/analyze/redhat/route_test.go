package redhat_test

import (
	"context"
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
)

func TestRouteAnalyzer(t *testing.T) {
	var os status.ObjectStatus

	ctx := context.Background()
	e, _, objs := test.TestEvaluator("routes.yaml")

	os = e.Eval(ctx, objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)
	test.AssertConditions(t, `Admitted   (Ok)`, os.Conditions)

	os = e.Eval(ctx, objs[1])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)

	test.AssertConditions(t, `Admitted   (Error)`, os.Conditions)
}
