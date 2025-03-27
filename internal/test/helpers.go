package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/inecas/kube-health/pkg/eval"
)

func LoadObject[T any](p string) (*T, error) {
	bb, err := os.ReadFile(filepath.Join("testdata", p))
	if err != nil {
		return nil, err
	}
	var l T
	if err := yaml.Unmarshal(bb, &l); err != nil {
		return nil, err
	}

	return &l, nil
}

func TestEvaluator(testdata ...string) (*eval.Evaluator, *eval.FakeLoader, []*status.Object) {
	loader := eval.NewFakeLoader()
	var objs []*status.Object
	for _, t := range testdata {
		objs = append(objs, RegisterTestData(loader, t)...)
	}

	evaluator := eval.NewEvaluator(analyze.DefaultAnalyzers(), loader)
	return evaluator, loader, objs
}

func RegisterTestData(loader *eval.FakeLoader, file string) []*status.Object {
	data, err := LoadObject[unstructured.UnstructuredList](file)
	if err != nil {
		panic(err)
	}

	objs, err := loader.Register(data.Items...)
	if err != nil {
		panic(err)
	}
	return objs
}

func AssertConditions(t *testing.T, expected string, conditions []status.ConditionStatus) {
	msgs := ""
	for _, c := range conditions {
		msgs += fmt.Sprintf("%s %s %s (%s)\n", c.Type, c.Reason, c.Message, c.CondStatus.Result.String())
	}
	assert.Equal(t, strings.TrimSpace(expected), strings.TrimSpace(msgs))
}

func AssertStr(t *testing.T, expected, actual string) {
	assert.Equal(t, trimLines(expected), trimLines(actual))
}

func trimLines(str string) string {
	var lines []string
	for _, l := range strings.Split(str, "\n") {
		lines = append(lines, strings.TrimRight(l, " "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
