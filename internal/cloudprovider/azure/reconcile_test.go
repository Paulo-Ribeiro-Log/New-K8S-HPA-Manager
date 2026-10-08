package azure

import (
	"errors"
	"strings"
	"testing"

	"k8s-hpa-manager/internal/cloudprovider"
)

func TestCheckReconcileState(t *testing.T) {
	for _, s := range []string{"Failed", "Canceled"} {
		if err := checkReconcileState(s); err != nil {
			t.Errorf("%s deveria permitir reconcile: %v", s, err)
		}
	}
	for _, s := range []string{"Succeeded", "Updating", "Scaling", "Upgrading", "Creating", "Deleting", ""} {
		var se *cloudprovider.ReconcileStateError
		if err := checkReconcileState(s); !errors.As(err, &se) || se.State != s {
			t.Errorf("%q deveria recusar com ReconcileStateError, veio %v", s, err)
		}
	}
}

func TestAzureReconcileArgs(t *testing.T) {
	got := strings.Join(azureReconcileArgs("sub-1", "rg", "aks1", "pool1"), " ")
	want := "aks nodepool update --subscription sub-1 --resource-group rg --cluster-name aks1 --name pool1 --no-wait"
	if got != want {
		t.Errorf("args = %q\nwant  %q", got, want)
	}
	// sem nenhuma flag de mudança: é isso que faz o update ser um reconcile
	for _, flag := range []string{"--node-count", "--enable-cluster-autoscaler", "--update-cluster-autoscaler", "--min-count", "--max-count"} {
		if strings.Contains(got, flag) {
			t.Errorf("reconcile não pode ter %s", flag)
		}
	}
	if s := strings.Join(azureNodePoolStateArgs("sub-1", "rg", "aks1", "pool1"), " "); !strings.Contains(s, "--subscription sub-1") || !strings.Contains(s, "--query provisioningState") {
		t.Errorf("state args = %q", s)
	}
}
