package test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/inecas/kube-health/pkg/analyze"
	"github.com/inecas/kube-health/pkg/status"
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

	evaluator := eval.NewEvaluator(context.Background(), analyze.DefaultAnalyzers(), loader)
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
