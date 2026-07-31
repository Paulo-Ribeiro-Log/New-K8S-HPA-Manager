package certificates

import "time"

// CertificateInfo representa informações completas de um certificado TLS
type CertificateInfo struct {
	// Identificação
	SecretName string `json:"secretName"`
	Namespace  string `json:"namespace"`
	Cluster    string `json:"cluster"`

	// Dados x509
	Subject      string    `json:"subject"`
	Issuer       string    `json:"issuer"`
	SerialNumber string    `json:"serialNumber"`
	NotBefore    time.Time `json:"notBefore"`
	NotAfter     time.Time `json:"notAfter"`
	DNSNames     []string  `json:"dnsNames"`
	IsCA         bool      `json:"isCA"`
	KeyAlgorithm string    `json:"keyAlgorithm"`
	KeySize      int       `json:"keySize"`

	// Status calculado
	Status        string `json:"status"`        // "valid", "expiring", "expired"
	DaysRemaining int    `json:"daysRemaining"` // Negativo se expirado

	// Cross-references
	UsedByIngresses []IngressRef `json:"usedByIngresses"`
	// UsedByGateways — equivalente de UsedByIngresses para Gateway API (Gateway+HTTPRoute).
	UsedByGateways []GatewayRef `json:"usedByGateways,omitempty"`

	// HasConfigIssues é derivado de UsedByIngresses[].HostIssues + UsedByGateways[].HostIssues —
	// só para alimentar badge/filtro da listagem (CertificatesTab.tsx), nunca fonte de verdade (o
	// texto das mensagens só existe nos HostIssues de cada referência).
	HasConfigIssues bool `json:"hasConfigIssues,omitempty"`

	// cert-manager (se disponível)
	CertManager *CertManagerInfo `json:"certManager,omitempty"`

	// Chain
	ChainLength  int             `json:"chainLength"`
	ChainDetails []ChainCertInfo `json:"chainDetails,omitempty"`

	// ChainValidation — resultado de ValidateCertificateChain (validate.go, Fase 1 do
	// CERT-ROLLBACK-VALIDATION-PLAN.md) já calculado durante o scan, sem precisar clicar em
	// "Validar Cadeia" pra cada certificado. Não inclui LivePropagation (Fase 3, Prometheus) — isso
	// continua só sob demanda, pra não multiplicar consultas ao Prometheus por certificado escaneado.
	ChainValidation *ChainValidationResult `json:"chainValidation,omitempty"`
}

// IngressRef representa uma referência de Ingress que usa o certificado
type IngressRef struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace"`
	Hosts     []string `json:"hosts"`

	IngressClass string `json:"ingressClass,omitempty"`
	// BackendTLS é true quando o Ingress usa nginx.ingress.kubernetes.io/backend-protocol
	// HTTPS/GRPCS ou ssl-passthrough — re-encryption entre o ingress-controller e o pod backend,
	// superfície de risco que a Fase 8 (Diagnóstico Avançado, backend_tls_check.go) cobre.
	BackendTLS bool `json:"backendTLS,omitempty"`
	// HostIssues — um item por host problemático desta referência (SAN não cobre o host,
	// conflito de host com outro Ingress/Gateway, ou aviso de re-encryption).
	HostIssues []HostIssue `json:"hostIssues,omitempty"`
}

// GatewayRef é o equivalente de IngressRef para Gateway API (Gateway+HTTPRoute) — ver
// scanner.go/gateway_hosts.go para como é populado durante o scan.
type GatewayRef struct {
	Name       string      `json:"name"`
	Namespace  string      `json:"namespace"`
	Hosts      []string    `json:"hosts"`
	HostIssues []HostIssue `json:"hostIssues,omitempty"`
}

// HostIssue descreve um problema de configuração associado a um host específico servido por um
// Ingress/Gateway — não ao certificado em si (um Secret pode ser usado por vários
// Ingresses/Gateways, com só um host problemático num deles).
type HostIssue struct {
	Host     string `json:"host"`
	Severity string `json:"severity"` // "error" | "warning"
	Message  string `json:"message"`
}

// CertManagerInfo representa informações do cert-manager
type CertManagerInfo struct {
	CertificateName string `json:"certificateName"`
	IssuerName      string `json:"issuerName"`
	IssuerKind      string `json:"issuerKind"` // Issuer ou ClusterIssuer
	RenewalTime     string `json:"renewalTime,omitempty"`
	IsReady         bool   `json:"isReady"`
}

// ChainCertInfo representa informações de um certificado na chain
type ChainCertInfo struct {
	Subject  string    `json:"subject"`
	Issuer   string    `json:"issuer"`
	NotAfter time.Time `json:"notAfter"`
	IsCA     bool      `json:"isCA"`
}

// ScanRequest representa uma requisição de scan de certificados
type ScanRequest struct {
	Clusters   []string `json:"clusters"`
	Namespaces []string `json:"namespaces,omitempty"`
	Filter     string   `json:"filter,omitempty"` // "all", "ingress", "common"
}

// ScanResult representa o resultado de um scan
type ScanResult struct {
	Certificates []CertificateInfo `json:"certificates"`
	TotalScanned int               `json:"totalScanned"`
	Summary      ScanSummary       `json:"summary"`
	ScannedAt    time.Time         `json:"scannedAt"`
}

// ScanSummary representa o resumo de um scan
type ScanSummary struct {
	Valid    int `json:"valid"`
	Expiring int `json:"expiring"`
	Expired  int `json:"expired"`
	Total    int `json:"total"`
}

// CopyRequest representa uma requisição de cópia de certificado
type CopyRequest struct {
	SourceCluster    string   `json:"sourceCluster"`
	SourceNamespace  string   `json:"sourceNamespace"`
	SecretName       string   `json:"secretName"`
	TargetClusters   []string `json:"targetClusters"`
	TargetNamespaces []string `json:"targetNamespaces"`
}

// UploadRequest representa uma requisição de upload de certificado
type UploadRequest struct {
	Name             string   `json:"name"`
	TLSCrt           string   `json:"tlsCrt"`
	TLSKey           string   `json:"tlsKey"`
	TargetClusters   []string `json:"targetClusters"`
	TargetNamespaces []string `json:"targetNamespaces"`
}
