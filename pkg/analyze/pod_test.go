package analyze

import (
	// "context"
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
)

func TestPodAnalyzer(t *testing.T) {
	e, _, objs := test.TestEvaluator("pods.yaml")
	os := e.Eval(objs[0])

	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)
}
