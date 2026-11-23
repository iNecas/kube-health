package cmd

import (
	"reflect"
	"testing"

	"github.com/rhobs/kube-health/pkg/status"
)

func TestResourceRequests(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want [][]string
	}{
		{nil, [][]string{{}}}, // file or stdin input
		{[]string{"deployment/foo", "deployment/bar"}, [][]string{{"deployment", "foo"}, {"deployment", "bar"}}},
		{[]string{"deployment", "foo", "bar"}, [][]string{{"deployment", "foo"}, {"deployment", "bar"}}},
		{[]string{"deployment"}, [][]string{{"deployment"}}},
		{[]string{"pod/a", "configmap"}, [][]string{{"pod", "a"}, {"configmap"}}},
		{[]string{"configmap", "pod/a", "service", "pod/b"}, [][]string{
			{"configmap"}, {"pod", "a"}, {"service"}, {"pod", "b"},
		}},
		{[]string{"pod/a/b", "pod/", "/a"}, [][]string{{"pod/a/b"}, {"pod/"}, {"/a"}}},
	} {
		if got := resourceRequests(tc.args); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%v: got %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestWaitForMissingResource(t *testing.T) {
	missing := []status.ObjectStatus{{ObjStatus: status.Status{Result: status.Unknown}}}
	healthy := []status.ObjectStatus{{ObjStatus: status.Status{Result: status.Ok}}}
	for _, fl := range []*flags{{waitProgress: true}, {waitOk: true}} {
		stops := 0
		wait := waitFunction(fl, func() { stops++ })
		wait(missing)
		if stops != 0 {
			t.Fatalf("%+v: stopped before object exists", fl)
		}
		wait(healthy)
		if stops != 1 {
			t.Fatalf("%+v: did not stop after object became healthy", fl)
		}
	}
}
