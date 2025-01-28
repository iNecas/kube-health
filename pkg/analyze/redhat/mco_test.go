package redhat_test

import (
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
)

func TestMcoAnalyzer(t *testing.T) {
	var os status.ObjectStatus

	e, _, objs := test.TestEvaluator("mcos.yaml")

	os = e.Eval(objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)
	test.AssertConditions(t, `
RenderDegraded   (Ok)
NodeDegraded   (Ok)
Degraded   (Ok)
Updated  All nodes are updated (Unknown)
Updating   (Unknown)
`, os.Conditions)

	os = e.Eval(objs[1])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)

	test.AssertConditions(t, `
RenderDegraded   (Ok)
Updating   (Unknown)
NodeDegraded   (Ok)
Degraded ErrPoolDegraded Pool failed updating (Error)
Updated  All nodes are updated (Unknown)
`, os.Conditions)
}
