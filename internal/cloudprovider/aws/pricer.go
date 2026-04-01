package aws

import (
	"fmt"

	"k8s-hpa-manager/internal/finops"
)

// AWSPricer implementa finops.CloudPricer para clusters EKS.
// Fase 5 do plano multi-cloud implementará consulta à AWS Pricing API.
type AWSPricer struct{}

func (p *AWSPricer) GetPrice(vmSize string) (float64, string, error) {
	return 0, "unknown", fmt.Errorf("AWS Pricer não implementado: %s", vmSize)
}

func (p *AWSPricer) GetVMSpecs(_ string) (int, int) {
	return 0, 0
}

// compile-time check
var _ finops.CloudPricer = (*AWSPricer)(nil)
