package finops

// CloudPricer é a interface para busca de preços de VM por cloud provider.
// AzurePricer e AWSPricer implementam esta interface.
type CloudPricer interface {
	// GetPrice retorna o preço USD/hora de um tipo de VM.
	// source indica a origem: "api" ou "fallback".
	GetPrice(vmSize string) (price float64, source string, err error)

	// GetVMSpecs retorna (vCPU, RAM GB) para um tipo de VM.
	// Retorna (0, 0) se desconhecido.
	GetVMSpecs(vmSize string) (cpuCores int, memGB int)
}
