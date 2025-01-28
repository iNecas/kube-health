package redhat_test

import (
	"testing"

	"github.com/inecas/kube-health/pkg/status"
	"github.com/stretchr/testify/assert"

	"github.com/inecas/kube-health/internal/test"
)

func TestClusterOperatorAnalyzer(t *testing.T) {
	var os status.ObjectStatus

	e, _, objs := test.TestEvaluator("clusteroperators.yaml")

	os = e.Eval(objs[0])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Ok)
	test.AssertConditions(t, `
Progressing WaitingForProvisioningCR  (Ok)
Degraded   (Ok)
Available WaitingForProvisioningCR Waiting for Provisioning CR (Ok)
Upgradeable   (Unknown)
Disabled   (Unknown)`, os.Conditions)

	os = e.Eval(objs[1])
	assert.False(t, os.Status().Progressing)
	assert.Equal(t, os.Status().Result, status.Error)

	test.AssertConditions(t, `
Degraded OAuthRouteCheckEndpointAccessibleController_SyncError OAuthRouteCheckEndpointAccessibleControllerDegraded (Error)
Progressing AsExpected AuthenticatorCertKeyProgressing: All is well (Ok)
Available NotAvailable The service is not available (Error)
Upgradeable AsExpected All is well (Unknown)
EvaluationConditionsDetected NoData  (Unknown)
`, os.Conditions)
}
