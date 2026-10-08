package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"k8s-hpa-manager/internal/cloudprovider"
	"k8s-hpa-manager/internal/models"
)

type fakeReconcileProvider struct{ err error }

func (f fakeReconcileProvider) ListNodeGroups(context.Context, string) ([]models.NodePool, error) {
	return nil, nil
}
func (f fakeReconcileProvider) ScaleNodeGroup(context.Context, string, string, int) error { return nil }
func (f fakeReconcileProvider) SetAutoscaling(context.Context, string, string, bool, int, int) error {
	return nil
}
func (f fakeReconcileProvider) AbortOperation(context.Context, string, string) error { return nil }
func (f fakeReconcileProvider) ReconcileNodeGroup(context.Context, string, string) error {
	return f.err
}
func (f fakeReconcileProvider) ValidateAuth(context.Context) error { return nil }

func TestReconcileNodePool(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		code  int
		state string
	}{
		{"aceito", nil, http.StatusAccepted, ""},
		{"eks/gke", cloudprovider.ErrNotSupported, http.StatusNotImplemented, ""},
		{"em andamento", &cloudprovider.ReconcileStateError{State: "Updating", Reason: "em andamento"}, http.StatusConflict, "Updating"},
		{"saudável", &cloudprovider.ReconcileStateError{State: "Succeeded", Reason: "nada a fazer"}, http.StatusConflict, "Succeeded"},
		{"falha do az", errors.New("boom"), http.StatusInternalServerError, ""},
	}
	for _, c := range cases {
		code, body := reconcileNodePool(context.Background(), fakeReconcileProvider{err: c.err}, "cl", "pool1")
		if code != c.code {
			t.Errorf("%s: código %d, esperado %d (%v)", c.name, code, c.code, body)
		}
		if c.state != "" && body["state"] != c.state {
			t.Errorf("%s: state %v", c.name, body["state"])
		}
		if _, ok := body["message"].(string); !ok {
			t.Errorf("%s: sem message: %v", c.name, body)
		}
	}
}

func TestNodePoolCacheExpiry(t *testing.T) {
	stable := []models.NodePool{{Name: "a", Status: "Succeeded"}, {Name: "b", Status: "Failed"}}
	if d := time.Until(nodePoolCacheExpiry(stable)); d < time.Minute {
		t.Errorf("pools estáveis deveriam usar o TTL longo, veio %v", d)
	}
	for _, st := range []string{"Updating", "Scaling", "UPDATING", "RECONCILING"} {
		moving := []models.NodePool{{Name: "a", Status: "Succeeded"}, {Name: "b", Status: st}}
		if d := time.Until(nodePoolCacheExpiry(moving)); d > 20*time.Second {
			t.Errorf("%s deveria usar o TTL curto, veio %v", st, d)
		}
	}
}
