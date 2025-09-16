package eval

import (
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery/cached/memory"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var (
	podGR = schema.GroupResource{
		Group:    "",
		Resource: "pods",
	}
	deploymentGR = schema.GroupResource{
		Group:    "",
		Resource: "deployments",
	}
	pvcGR = schema.GroupResource{
		Group:    "",
		Resource: "persistentvolumeclaims",
	}
	coGR = schema.GroupResource{
		Group:    "config.openshift.io",
		Resource: "clusteroperators",
	}
	podGVK = schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}
	deploymentGVK = schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Deployment",
	}
	pvcGVK = schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "PersistentVolumeClaim",
	}
	coGVK = schema.GroupVersionKind{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "ClusterOperator",
	}
	allTestResources = resourcesMap{
		podGR: groupVersionKindNamespaced{
			GroupVersionKind: podGVK,
			namespaced:       true,
		},
		deploymentGR: groupVersionKindNamespaced{
			GroupVersionKind: deploymentGVK,
			namespaced:       true,
		},
		pvcGR: groupVersionKindNamespaced{
			GroupVersionKind: pvcGVK,
			namespaced:       true,
		},
		coGR: groupVersionKindNamespaced{
			GroupVersionKind: coGVK,
			namespaced:       false,
		},
	}
	testNS    = "test-ns"
	test1Name = "test-1"
)

func TestFilterResources(t *testing.T) {
	tests := []struct {
		name              string
		includeAll        bool
		includedGKS       []schema.GroupKind
		excludedGKS       []schema.GroupKind
		expectedResources resourcesMap
	}{
		{
			name:              "Include all resources",
			includeAll:        true,
			includedGKS:       nil,
			excludedGKS:       nil,
			expectedResources: allTestResources,
		},
		{
			name:              "Include nothing",
			includeAll:        false,
			includedGKS:       nil,
			excludedGKS:       nil,
			expectedResources: resourcesMap{},
		},
		{
			name:       "Include only some resources",
			includeAll: false,
			includedGKS: []schema.GroupKind{
				{
					Group: "",
					Kind:  "Pod",
				},
				{
					Group: "",
					Kind:  "Deployment",
				},
			},
			excludedGKS: nil,
			expectedResources: resourcesMap{
				podGR: groupVersionKindNamespaced{
					GroupVersionKind: podGVK,
					namespaced:       true,
				},
				deploymentGR: groupVersionKindNamespaced{
					GroupVersionKind: deploymentGVK,
					namespaced:       true,
				},
			},
		},
		{
			name:        "Exclude some resources",
			includeAll:  true,
			includedGKS: nil,
			excludedGKS: []schema.GroupKind{
				{
					Group: "",
					Kind:  "Pod",
				},
				{
					Group: "",
					Kind:  "Deployment",
				},
			},
			expectedResources: resourcesMap{
				pvcGR: groupVersionKindNamespaced{
					GroupVersionKind: pvcGVK,
					namespaced:       true,
				},
				coGR: groupVersionKindNamespaced{
					GroupVersionKind: coGVK,
					namespaced:       false,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient, err := newGenericClient(createTestConfigFlags())
			assert.NoError(t, err)
			filteredResources := testClient.filterResources(allTestResources, tt.includeAll, tt.includedGKS, tt.excludedGKS)
			assert.Equal(t, filteredResources, tt.expectedResources)
		})
	}
}

func TestCompileGroupKindMatcher(t *testing.T) {
	tests := []struct {
		name              string
		gkMatcher         GroupKindMatcher
		namespace         string
		expectedResources resourcesMap
	}{
		{
			name: "Include all GroupKindMatcher with no namespace",
			gkMatcher: GroupKindMatcher{
				IncludeAll: true,
			},
			namespace: NamespaceNone,
			expectedResources: resourcesMap{
				coGR: groupVersionKindNamespaced{
					GroupVersionKind: coGVK,
					namespaced:       false,
				},
			},
		},
		{
			name: "Include all GroupKindMatcher with all namespaces",
			gkMatcher: GroupKindMatcher{
				IncludeAll: true,
			},
			namespace:         NamespaceAll,
			expectedResources: allTestResources,
		},
		{
			name: "Include all GroupKindMatcher with particular namespaces",
			gkMatcher: GroupKindMatcher{
				IncludeAll: true,
			},
			namespace: "test-namespace",
			expectedResources: resourcesMap{
				podGR: groupVersionKindNamespaced{
					GroupVersionKind: podGVK,
					namespaced:       true,
				},
				deploymentGR: groupVersionKindNamespaced{
					GroupVersionKind: deploymentGVK,
					namespaced:       true,
				},
				pvcGR: groupVersionKindNamespaced{
					GroupVersionKind: pvcGVK,
					namespaced:       true,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient, err := newGenericClient(createTestConfigFlags())
			assert.NoError(t, err)
			resources := testClient.compileGroupKindMatcher(tt.gkMatcher, tt.namespace)
			assert.Equal(t, resources, tt.expectedResources)
		})
	}
}

func TestLoadResource(t *testing.T) {
	type testReource struct {
		name, namespace string
		gr              schema.GroupResource
	}

	tests := []struct {
		name                 string
		objects              []runtime.Object
		testResource         testReource
		expectedStatusObject []*status.Object
	}{
		{
			name: "Load Pod by name",
			testResource: testReource{
				name:      test1Name,
				namespace: testNS,
				gr:        podGR,
			},
			objects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      test1Name,
						Namespace: testNS,
					},
				},
			},
			expectedStatusObject: []*status.Object{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      test1Name,
						Namespace: testNS,
					},
					TypeMeta: metav1.TypeMeta{
						Kind:       "Pod",
						APIVersion: "v1",
					},
					Unstructured: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "v1",
							"kind":       "Pod",
							"metadata": map[string]interface{}{
								"name":              test1Name,
								"namespace":         testNS,
								"creationTimestamp": nil,
							},
							"spec": map[string]interface{}{
								"containers": nil,
							},
							"status": map[string]interface{}{},
						},
					},
				},
			},
		},
		{
			name: "Load Pods in namespace",
			testResource: testReource{
				namespace: testNS,
				gr:        podGR,
			},
			objects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      test1Name,
						Namespace: testNS,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-2",
						Namespace: testNS,
					},
				},
			},
			expectedStatusObject: []*status.Object{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      test1Name,
						Namespace: testNS,
					},
					Unstructured: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"metadata": map[string]interface{}{
								"name":              test1Name,
								"namespace":         testNS,
								"creationTimestamp": nil,
							},
							"spec": map[string]interface{}{
								"containers": nil,
							},
							"status": map[string]interface{}{},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-2",
						Namespace: testNS,
					},
					Unstructured: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"metadata": map[string]interface{}{
								"name":              "test-2",
								"namespace":         testNS,
								"creationTimestamp": nil,
							},
							"spec": map[string]interface{}{
								"containers": nil,
							},
							"status": map[string]interface{}{},
						},
					},
				},
			},
		},
		{
			name: "Load ClusterOperator by name",
			testResource: testReource{
				name: "test-co",
				gr:   coGR,
			},
			objects: []runtime.Object{
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "config.openshift.io/v1",
						"kind":       "ClusterOperator",
						"metadata": map[string]interface{}{
							"name": "test-co",
						},
						"spec": map[string]interface{}{},
					},
				},
			},
			expectedStatusObject: []*status.Object{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-co",
					},
					TypeMeta: metav1.TypeMeta{
						Kind:       "ClusterOperator",
						APIVersion: "config.openshift.io/v1",
					},
					Unstructured: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"apiVersion": "config.openshift.io/v1",
							"kind":       "ClusterOperator",
							"metadata": map[string]interface{}{
								"name": "test-co",
							},
							"spec": map[string]interface{}{},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{
				dynamic:   createDynamicFakeClientWithObjects(tt.objects...),
				resources: allTestResources,
			}
			rl := RealLoader{client: c}
			statusObjects, err := rl.LoadResource(t.Context(),
				tt.testResource.gr, tt.testResource.namespace, tt.testResource.name)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatusObject, statusObjects)
		})
	}
}

func TestLoadResourceBySelector(t *testing.T) {
	type testReource struct {
		label, namespace string
		gr               schema.GroupResource
	}
	tests := []struct {
		name                 string
		objects              []runtime.Object
		testResource         testReource
		expectedStatusObject []*status.Object
	}{
		{
			name: "Load Pods by selector",
			objects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name: test1Name,
						Labels: map[string]string{
							"test-label": "foo",
						},
						Namespace: testNS,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-2",
						Namespace: testNS,
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-3",
						Namespace: "another-ns",
						Labels: map[string]string{
							"test-label": "foo",
						},
					},
				},
			},
			testResource: testReource{
				label:     "test-label=foo",
				namespace: testNS,
				gr:        podGR,
			},
			expectedStatusObject: []*status.Object{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      test1Name,
						Namespace: testNS,
						Labels: map[string]string{
							"test-label": "foo",
						},
					},
					Unstructured: &unstructured.Unstructured{
						Object: map[string]interface{}{
							"metadata": map[string]interface{}{
								"name":              test1Name,
								"namespace":         testNS,
								"creationTimestamp": nil,
								"labels": map[string]interface{}{
									"test-label": "foo",
								},
							},
							"spec": map[string]interface{}{
								"containers": nil,
							},
							"status": map[string]interface{}{},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{
				dynamic:   createDynamicFakeClientWithObjects(tt.objects...),
				resources: allTestResources,
			}
			rl := RealLoader{client: c}
			statusObjects, err := rl.LoadResourceBySelector(t.Context(),
				tt.testResource.gr, tt.testResource.namespace, tt.testResource.label)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatusObject, statusObjects)
		})
	}
}

func createDynamicFakeClientWithObjects(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	podGvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	covr := schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1", Resource: "clusteroperators"}
	fakeCli := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		podGvr: "PodList",
		covr:   "ClusterOperatorList",
	})
	for _, o := range objects {
		fakeCli.Tracker().Add(o)
	}
	return fakeCli
}

func createTestConfigFlags(objects ...runtime.Object) *genericclioptions.TestConfigFlags {
	fakeClientset := fake.NewSimpleClientset(objects...)
	fakeClientset.Resources = append(fakeClientset.Resources, &metav1.APIResourceList{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{
			{
				Name:       "pods",
				Namespaced: true,
				Verbs:      metav1.Verbs{"get", "list"},
				Kind:       "Pod",
			},
			{
				Name:       "deployments",
				Namespaced: true,
				Verbs:      metav1.Verbs{"get", "list"},
				Kind:       "Deployment",
			},
			{
				Name:       "persistentvolumeclaims",
				Namespaced: true,
				Verbs:      metav1.Verbs{"get", "list"},
				Kind:       "PersistentVolumeClaim",
			},
		},
	}, &metav1.APIResourceList{
		GroupVersion: "config.openshift.io/v1",
		APIResources: []metav1.APIResource{
			{
				Name:       "clusteroperators",
				Namespaced: false,
				Verbs:      metav1.Verbs{"get", "list"},
				Kind:       "ClusterOperator",
			},
		},
	})

	cachedDiscovery := memory.NewMemCacheClient(fakeClientset.Discovery())
	return genericclioptions.NewTestConfigFlags().
		WithDiscoveryClient(cachedDiscovery).WithClientConfig(&MockClientConfig{})
}

type MockClientConfig struct {
}

func (m *MockClientConfig) RawConfig() (clientcmdapi.Config, error) {
	return clientcmdapi.Config{}, nil
}

func (m *MockClientConfig) ClientConfig() (*restclient.Config, error) {
	return &restclient.Config{}, nil
}

func (m *MockClientConfig) Namespace() (string, bool, error) {
	return "", false, nil
}

func (m *MockClientConfig) ConfigAccess() clientcmd.ConfigAccess {
	return nil
}
