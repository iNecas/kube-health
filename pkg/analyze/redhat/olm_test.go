package redhat_test

import (
	"context"
	"strings"
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
	"github.com/inecas/kube-health/pkg/print"
)

func TestOlmAnalyzer(t *testing.T) {
	var os status.ObjectStatus
	p := print.NewTreePrinter(print.PrintOptions{ShowOk: true})
	ctx := context.Background()

	e, _, objs := test.TestEvaluator("olm_subscriptions.yaml", "olm_install_plans.yaml", "olm_csvs.yaml")

	os = e.Eval(ctx, objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)
	test.AssertConditions(t, `
CatalogSourcesUnhealthy AllCatalogSourcesHealthy all available catalogsources are healthy (Ok)
`, os.Conditions)

	os = e.Eval(ctx, objs[1])
	assert.True(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)
	test.AssertConditions(t, `
CatalogSourcesUnhealthy AllCatalogSourcesHealthy all available catalogsources are healthy (Ok)
InstallPlan InstallPlanMissing Install plan not found (Unknown)
`, os.Conditions)

	os = e.Eval(ctx, objs[2])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)
	test.AssertConditions(t, `
CatalogSourcesUnhealthy AllCatalogSourcesHealthy all available catalogsources are healthy (Ok)
`, os.Conditions)

	sb := &strings.Builder{}
	p.PrintStatuses([]status.ObjectStatus{os}, sb)
	test.AssertStr(t, `
OBJECT           CONDITION                       AGE    REASON
Error openshift-operators/Subscription/op3
│                CatalogSourcesUnhealthy=False   24h    AllCatalogSourcesHealthy
├─ Error ClusterServiceVersion/op3.0.4.1
│                (Error) Failed=                 24h    ComponentUnhealthy
│                  installing: waiting for deployment to become ready
└─ Ok InstallPlan/install-zvmlq
                 Installed=True                  24h
`, sb.String())
}
