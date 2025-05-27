package redhat_test

import (
	"context"
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
)

func TestClusterOperatorAnalyzer(t *testing.T) {
	var os status.ObjectStatus

	e, _, objs := test.TestEvaluator("clusteroperators.yaml", "authentication.yaml")

	os = e.Eval(context.Background(), objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)
	test.AssertConditions(t, `
Progressing WaitingForProvisioningCR  (Ok)
Degraded   (Ok)
Available WaitingForProvisioningCR Waiting for Provisioning CR (Ok)
Upgradeable   (Unknown)
Disabled   (Unknown)`, os.Conditions)

	os = e.Eval(context.Background(), objs[1])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)

	test.AssertConditions(t, `
Degraded OAuthRouteCheckEndpointAccessibleController_SyncError OAuthRouteCheckEndpointAccessibleControllerDegraded (Error)
Progressing AsExpected AuthenticatorCertKeyProgressing: All is well (Ok)
Available NotAvailable The service is not available (Error)
Upgradeable AsExpected All is well (Unknown)
EvaluationConditionsDetected NoData  (Unknown)
`, os.Conditions)

	// check status of related objects
	assert.Len(t, os.SubStatuses, 1)
	test.AssertConditions(t, `
OAuthServiceDegraded   (Ok)
APIServerDeploymentAvailable AsExpected  (Unknown)
APIServerDeploymentDegraded AsExpected  (Ok)
	`, os.SubStatuses[0].Conditions)
}
