package status

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// Object is a common representation of a Kubernetes object with the most
// common fields across all objects. It also allows access to the raw object.
type Object struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	Unstructured *unstructured.Unstructured
}

func NewObjectFromUnstructured(unst *unstructured.Unstructured) (*Object, error) {
	obj := &Object{Unstructured: unst}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(unst.Object, &obj.TypeMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to read object type data: %w", err)
	}

	meta, found, err := unstructured.NestedMap(unst.Object, "metadata")
	if !found || err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(meta, &obj.ObjectMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to read object metadata: %w", err)
	}

	return obj, nil
}
