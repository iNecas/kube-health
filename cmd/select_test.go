package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// A small fake API server exercises the real kubectl resource builder without
// needing a live cluster. Discovery advertises namespaced Pods/ConfigMaps and
// cluster-scoped Namespaces.
func TestSelectObjects(t *testing.T) {
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		var response any
		switch r.URL.Path {
		case "/api":
			response = metav1.APIVersions{Versions: []string{"v1"}}
		case "/apis":
			response = metav1.APIGroupList{}
		case "/api/v1":
			response = metav1.APIResourceList{GroupVersion: "v1", APIResources: []metav1.APIResource{
				{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: metav1.Verbs{"get", "list"}},
				{Name: "configmaps", Kind: "ConfigMap", Namespaced: true, Verbs: metav1.Verbs{"get", "list"}},
				{Name: "namespaces", Kind: "Namespace", Verbs: metav1.Verbs{"get", "list"}},
			}}
		case "/api/v1/namespaces/test":
			response = map[string]any{"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": "test"}}
		case "/api/v1/namespaces/test/pods/present":
			response = map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{
				"name": "present", "namespace": "test", "uid": "pod-uid",
			}}
		case "/api/v1/namespaces/test/configmaps":
			response = map[string]any{"apiVersion": "v1", "kind": "ConfigMapList", "items": []any{}}
		case "/api/v1/namespaces/test/pods/denied":
			w.WriteHeader(http.StatusForbidden)
			response = apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "denied", nil).Status()
		case "/api/v1/namespaces/test/pods/missing":
			w.WriteHeader(http.StatusNotFound)
			response = apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "missing").Status()
		case "/api/v1/namespaces/missing":
			w.WriteHeader(http.StatusNotFound)
			response = apierrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, "missing").Status()
		default:
			t.Errorf("unexpected API request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
			response = apierrors.NewNotFound(schema.GroupResource{Resource: "unknown"}, "unknown").Status()
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	config := clientcmdapi.NewConfig()
	config.Clusters["test"] = &clientcmdapi.Cluster{Server: server.URL}
	config.AuthInfos["test"] = &clientcmdapi.AuthInfo{}
	config.Contexts["test"] = &clientcmdapi.Context{Cluster: "test", AuthInfo: "test", Namespace: "test"}
	config.CurrentContext = "test"
	kubeconfig := filepath.Join(t.TempDir(), "config")
	if err := clientcmd.WriteToFile(*config, kubeconfig); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		args      []string
		wait      bool
		ignore    bool
		want      []string // kind/name/namespace/UID
		wantCalls []string
		wantErr   bool
	}{
		{
			name: "mixed: missing name then empty list", args: []string{"pod/missing", "configmap"}, wait: true,
			want: []string{"Pod/missing/test/"}, wantCalls: []string{
				"/api/v1/namespaces/test/pods/missing", "/api/v1/namespaces/test/configmaps",
			},
		},
		{
			name: "mixed: empty list then missing name", args: []string{"configmap", "pod/missing"}, wait: true,
			want: []string{"Pod/missing/test/"}, wantCalls: []string{
				"/api/v1/namespaces/test/configmaps", "/api/v1/namespaces/test/pods/missing",
			},
		},
		{
			name: "mixed: present and missing names", args: []string{"pod/present", "pod/missing"}, wait: true,
			want: []string{"Pod/present/test/pod-uid", "Pod/missing/test/"}, wantCalls: []string{
				"/api/v1/namespaces/test/pods/present", "/api/v1/namespaces/test/pods/missing",
			},
		},
		{
			name: "ignore missing name", args: []string{"pod/missing", "configmap"}, wait: true, ignore: true,
			wantCalls: []string{"/api/v1/namespaces/test/pods/missing", "/api/v1/namespaces/test/configmaps"},
		},
		{
			name: "cluster-scoped placeholder", args: []string{"namespace/missing"}, wait: true,
			want: []string{"Namespace/missing//"}, wantCalls: []string{"/api/v1/namespaces/missing"},
		},
		{
			name: "type-only empty list", args: []string{"configmap"}, wait: true,
			wantCalls: []string{"/api/v1/namespaces/test/configmaps"},
		},
		{
			name: "missing name without wait", args: []string{"pod/missing"},
			wantErr: true, wantCalls: []string{"/api/v1/namespaces/test/pods/missing"},
		},
		{
			name: "non-not-found error", args: []string{"pod/denied"}, wait: true,
			wantErr: true, wantCalls: []string{"/api/v1/namespaces/test/pods/denied"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			requests = nil
			mu.Unlock()
			fl := newFlags()
			*fl.configFlags.KubeConfig = kubeconfig
			fl.waitOk = tc.wait
			fl.ignoreNotFound = tc.ignore
			objects, err := selectObjects(fl, tc.args, &resource.FilenameOptions{}, "test", true)
			if (err != nil) != tc.wantErr {
				t.Fatalf("selectObjects error = %v, wantErr = %v", err, tc.wantErr)
			}
			var got []string
			for _, obj := range objects {
				got = append(got, obj.Kind+"/"+obj.Name+"/"+obj.Namespace+"/"+string(obj.UID))
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("objects = %v, want %v", got, tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			var calls []string
			for _, path := range requests {
				if path != "/api" && path != "/apis" && path != "/api/v1" && path != "/api/v1/namespaces/test" {
					calls = append(calls, path)
				}
			}
			if !reflect.DeepEqual(calls, tc.wantCalls) {
				t.Errorf("resource calls = %v, want %v", calls, tc.wantCalls)
			}
		})
	}
}
