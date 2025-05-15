package eval

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	podGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}
	deploymentGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "deployments",
	}
	pvcGVR = schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "persistentvolumeclaims",
	}
	coGVR = schema.GroupVersionResource{
		Group:    "config.openshift.io",
		Version:  "v1",
		Resource: "clusteroperators",
	}
)

func TestFilterResources(t *testing.T) {
	inputResources := []schema.GroupVersionResource{podGVR, deploymentGVR, pvcGVR, coGVR}

	tests := []struct {
		name              string
		includeAll        bool
		includedGKS       []schema.GroupKind
		excludedGKS       []schema.GroupKind
		expectedResources []schema.GroupVersionResource
	}{
		{
			name:              "Include all resources",
			includeAll:        true,
			includedGKS:       nil,
			excludedGKS:       nil,
			expectedResources: []schema.GroupVersionResource{podGVR, deploymentGVR, pvcGVR, coGVR},
		},
		{
			name:              "Include nothing",
			includeAll:        false,
			includedGKS:       nil,
			excludedGKS:       nil,
			expectedResources: nil,
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
			excludedGKS:       nil,
			expectedResources: []schema.GroupVersionResource{podGVR, deploymentGVR},
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
			expectedResources: []schema.GroupVersionResource{pvcGVR, coGVR},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient, err := newGenericClient(createTestConfigFlags())
			assert.NoError(t, err)
			filteredResources := testClient.filterResources(inputResources, tt.includeAll, tt.includedGKS, tt.excludedGKS)
			assert.ElementsMatch(t, filteredResources, tt.expectedResources)
		})
	}
}

func TestCompileGroupKindMatcher(t *testing.T) {
	tests := []struct {
		name             string
		gkMatcher        GroupKindMatcher
		namespace        string
		expectedResource []schema.GroupVersionResource
	}{
		{
			name: "Include all GroupKindMatcher with no namespace",
			gkMatcher: GroupKindMatcher{
				IncludeAll: true,
			},
			namespace:        NamespaceNone,
			expectedResource: []schema.GroupVersionResource{coGVR},
		},
		{
			name: "Include all GroupKindMatcher with all namespaces",
			gkMatcher: GroupKindMatcher{
				IncludeAll: true,
			},
			namespace:        NamespaceAll,
			expectedResource: []schema.GroupVersionResource{podGVR, deploymentGVR, pvcGVR, coGVR},
		},
		{
			name: "Include all GroupKindMatcher with particular namespaces",
			gkMatcher: GroupKindMatcher{
				IncludeAll: true,
			},
			namespace:        "test-namespace",
			expectedResource: []schema.GroupVersionResource{podGVR, deploymentGVR, pvcGVR},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testClient, err := newGenericClient(createTestConfigFlags())
			assert.NoError(t, err)
			resources := testClient.compileGroupKindMatcher(tt.gkMatcher, tt.namespace)
			assert.ElementsMatch(t, resources, tt.expectedResource)
		})
	}
}

func createTestConfigFlags() *genericclioptions.TestConfigFlags {
	fakeClientset := fake.NewSimpleClientset()
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
		WithDiscoveryClient(cachedDiscovery).WithClientConfig(&clientcmd.DefaultClientConfig)
}
