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
