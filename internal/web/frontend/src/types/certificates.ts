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
  usedByGateways?: GatewayRef[];

  // hasConfigIssues é derivado de usedByIngresses[].hostIssues + usedByGateways[].hostIssues —
  // só pra badge/filtro da listagem (CertificatesTab.tsx); o texto das mensagens só existe nos
  // hostIssues de cada referência.
  hasConfigIssues?: boolean;

  // cert-manager
  certManager?: CertManagerInfo;

  // Chain
  chainLength: number;
  chainDetails?: ChainCertInfo[];

  // chainValidation — ValidateCertificateChain (Fase 1) já calculado durante o scan (sem
  // LivePropagation/Prometheus, que continua só sob demanda via "Validar Cadeia").
  chainValidation?: ChainValidationResult;
}

export interface IngressRef {
  name: string;
  namespace: string;
  hosts: string[];
  ingressClass?: string;
  // backendTLS: este Ingress usa nginx.ingress.kubernetes.io/backend-protocol HTTPS/GRPCS ou
  // ssl-passthrough (re-encryption entre o ingress-controller e o pod backend) — condiciona o
  // botão "Diagnóstico Avançado" (backend-tls-check).
  backendTLS?: boolean;
  hostIssues?: HostIssue[];
}

// GatewayRef é o equivalente de IngressRef para Gateway API (Gateway+HTTPRoute).
export interface GatewayRef {
  name: string;
  namespace: string;
  hosts: string[];
  hostIssues?: HostIssue[];
}

// HostIssue descreve um problema de configuração associado a um host específico servido por um
// Ingress/Gateway — não ao certificado em si.
export interface HostIssue {
  host: string;
  severity: "error" | "warning";
  message: string;
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

// LivePropagationResult — resultado de EnrichWithPrometheus (Fase 3) ou EnrichWithTLSDial (Fase 4,
// CERT-ROLLBACK-VALIDATION-PLAN.md): cruza o serial do leaf cert com o que está sendo servido de
// fato agora — via métricas do ingress-nginx (method="prometheus-nginx") ou, quando isso não é
// possível (cluster sem ingress-nginx, ex: Gateway API), via handshake TLS direto contra os hosts
// que servem o Secret (method="tls-dial"). checked=false não é erro — só significa "não deu pra
// checar" (Prometheus indisponível e nenhum host resolvido para o handshake, etc.) — nesse caso
// `notes` explica o motivo.
export interface LivePropagationResult {
  checked: boolean;
  method?: "prometheus-nginx" | "tls-dial";
  total_replicas_found: number;
  replicas_current: number;
  replicas_stale?: string[]; // kubernetes_pod_name (nginx) ou hostname (tls-dial) com serial diferente do atual
  live_issuer_cn?: string;
  live_expires_at?: string;
  notes?: string[];
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
  live_propagation?: LivePropagationResult;
}

// BackendTLSCheckResult — resultado de CheckIngressBackendTLS (backend_tls_check.go, Diagnóstico
// Avançado sob demanda). Nunca confirma "sem erro" — só reporta se achou sinal (signals) na
// janela de log analisada ou não (o que não significa ausência do problema).
export interface BackendTLSCheckResult {
  checked: boolean;
  controller_pods?: string[];
  signals?: string[];
  notes?: string[];
}

// RollbackBackupInfo — backup de um Secret TLS salvo antes de ser sobrescrito (Fase 2 do
// CERT-ROLLBACK-VALIDATION-PLAN.md). snake_case, mesmo motivo de ChainValidationResult.
export interface RollbackBackupInfo {
  backup_id: string;
  cluster: string;
  namespace: string;
  secret_name: string;
  backed_up_at: string;
  subject: string;
  serial_number: string;
  not_after: string;
}

// ManualBackupInfo — backup separado do Rollback, disparado sob demanda pelo usuário (botão
// "Copiar para Backup"), com comentário opcional. snake_case, mesmo motivo de RollbackBackupInfo.
export interface ManualBackupInfo {
  backup_id: string;
  cluster: string;
  namespace: string;
  secret_name: string;
  backed_up_at: string;
  subject: string;
  serial_number: string;
  not_after: string;
  comment?: string;
}
