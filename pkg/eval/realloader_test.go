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

func TestFilterResources(t *testing.T) {
	inputResources := []schema.GroupVersionResource{
		{
			Group:    "",
			Version:  "v1",
			Resource: "pods",
		},
		{
			Group:    "",
			Version:  "v1",
			Resource: "deployments",
		},
		{
			Group:    "",
			Version:  "v1",
			Resource: "persistentvolumeclaims",
		},
		{
			Group:    "config.openshift.io",
			Version:  "v1",
			Resource: "clusteroperators",
		},
	}

	tests := []struct {
		name              string
		includeAll        bool
		includedGKS       []schema.GroupKind
		excludedGKS       []schema.GroupKind
		expectedResources []schema.GroupVersionResource
	}{
		{
			name:        "Include all resources",
			includeAll:  true,
			includedGKS: nil,
			excludedGKS: nil,
			expectedResources: []schema.GroupVersionResource{
				{
					Group:    "",
					Version:  "v1",
					Resource: "pods",
				},
				{
					Group:    "",
					Version:  "v1",
					Resource: "deployments",
				},
				{
					Group:    "",
					Version:  "v1",
					Resource: "persistentvolumeclaims",
				},
				{
					Group:    "config.openshift.io",
					Version:  "v1",
					Resource: "clusteroperators",
				},
			},
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
			excludedGKS: nil,
			expectedResources: []schema.GroupVersionResource{
				{
					Group:    "",
					Version:  "v1",
					Resource: "pods",
				},
				{
					Group:    "",
					Version:  "v1",
					Resource: "deployments",
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
			expectedResources: []schema.GroupVersionResource{
				{
					Group:    "",
					Version:  "v1",
					Resource: "persistentvolumeclaims",
				},
				{
					Group:    "config.openshift.io",
					Version:  "v1",
					Resource: "clusteroperators",
				},
			},
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
