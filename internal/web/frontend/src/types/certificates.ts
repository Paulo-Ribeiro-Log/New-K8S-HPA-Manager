// Types for TLS Certificates management

export interface CertificateInfo {
  secretName: string;
  namespace: string;
  cluster: string;

  // x509 data
  subject: string;
  issuer: string;
  serialNumber: string;
  notBefore: string;
  notAfter: string;
  dnsNames: string[];
  isCA: boolean;
  keyAlgorithm: string;
  keySize: number;

  // Status
  status: 'valid' | 'expiring' | 'expired';
  daysRemaining: number;

  // Cross-references
  usedByIngresses: IngressRef[];

  // cert-manager
  certManager?: CertManagerInfo;

  // Chain
  chainLength: number;
  chainDetails?: ChainCertInfo[];
}

export interface IngressRef {
  name: string;
  namespace: string;
  hosts: string[];
}

export interface CertManagerInfo {
  certificateName: string;
  issuerName: string;
  issuerKind: string;
  renewalTime?: string;
  isReady: boolean;
}

export interface ChainCertInfo {
  subject: string;
  issuer: string;
  notAfter: string;
  isCA: boolean;
}

export interface ScanRequest {
  clusters: string[];
  namespaces?: string[];
  filter?: 'all' | 'ingress' | 'common';
}

export interface ScanResult {
  certificates: CertificateInfo[];
  totalScanned: number;
  summary: ScanSummary;
  scannedAt: string;
}

export interface ScanSummary {
  valid: number;
  expiring: number;
  expired: number;
  total: number;
}

export interface CopyRequest {
  sourceCluster: string;
  sourceNamespace: string;
  secretName: string;
  targetClusters: string[];
  targetNamespaces: string[];
}

export interface UploadRequest {
  name: string;
  tlsCrt: string;
  tlsKey: string;
  targetClusters: string[];
  targetNamespaces: string[];
}

// ChainValidationResult — resultado de ValidateCertificateChain (backend, internal/certificates/validate.go).
// snake_case porque vem direto do JSON do backend (não passa por transformação camelCase).
export interface ChainValidationResult {
  valid: boolean;
  key_matches_cert: boolean;
  chain_order_correct: boolean;
  trusted_by_public_ca: boolean; // false é comum e OK pra CA interna/privada
  expiry_ok: boolean;
  errors?: string[];
  warnings?: string[];
  chain_subjects: string[]; // leaf → intermediário(s) → (raiz, se presente)
  openssl_notes?: string[];
}
