package print

import (
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/printers"

	"github.com/inecas/kube-health/pkg/status"
)

// Genric printer as a wrapper around kubectl standard printers, to produce
// json, yaml and other standard printing capabilities.

type KubectlPrinter struct {
	Printer printers.ResourcePrinter
}

type objectWrapper struct {
	Object     corev1.ObjectReference   `json:"object"`
	Status     status.Status            `json:"health"`
	Conditions []status.ConditionStatus `json:"conditions,omitempty"`
	Subobjects []*objectWrapper         `json:"subobjects,omitempty"`
}

// objectWrapper implements runtime.Object interface
var _ runtime.Object = &objectWrapper{}

func (ow *objectWrapper) GetObjectKind() schema.ObjectKind {
	return schema.EmptyObjectKind

}

func (ow *objectWrapper) DeepCopyObject() runtime.Object {
	return ow.DeepCopy()
}

func (ow *objectWrapper) DeepCopy() *objectWrapper {
	var conditions []status.ConditionStatus
	for _, c := range ow.Conditions {
		conditions = append(conditions, *c.DeepCopy())
	}

	var subobjects []*objectWrapper
	for _, o := range ow.Subobjects {
		subobjects = append(subobjects, o.DeepCopy())
	}

	return &objectWrapper{
		Object:     *ow.Object.DeepCopy(),
		Status:     *ow.Status.DeepCopy(),
		Conditions: conditions,
		Subobjects: subobjects,
	}
}

func wrapObjectStatus(s status.ObjectStatus) *objectWrapper {
	ret := objectWrapper{
		Object: corev1.ObjectReference{
			APIVersion: s.Object.APIVersion,
			Kind:       s.Object.Kind,
			Name:       s.Object.Name,
			Namespace:  s.Object.Namespace,
			UID:        s.Object.UID,
		},
		Status:     s.ObjStatus,
		Conditions: s.Conditions,
	}

	for _, ss := range s.SubStatuses {
		ret.Subobjects = append(ret.Subobjects, wrapObjectStatus(ss))
	}

	return &ret
}

func (p KubectlPrinter) PrintStatuses(statuses []status.ObjectStatus, w io.Writer) {
	objects := make([]runtime.Object, 0, len(statuses))
	for _, s := range statuses {
		objects = append(objects, wrapObjectStatus(s))
	}

	list := &corev1.List{
		TypeMeta: metav1.TypeMeta{
			Kind:       "List",
			APIVersion: "v1",
		},
		ListMeta: metav1.ListMeta{},
	}
	if err := meta.SetList(list, objects); err != nil {
		panic(err)
	}

	p.Printer.PrintObj(list, w)
}
