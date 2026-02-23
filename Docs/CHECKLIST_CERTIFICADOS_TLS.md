# Checklist: Implementacao Aba Certificados TLS

Data de criacao: 06/02/2026
Status: Pendente

---

## Fase 1: Backend - Models
- [ ] Criar diretorio `internal/certificates/`
- [ ] Criar `internal/certificates/models.go`
  - [ ] Struct `CertificateInfo` (SecretName, Namespace, Cluster, dados x509, status, cross-refs)
  - [ ] Struct `IngressRef` (Name, Namespace, Hosts)
  - [ ] Struct `CertManagerInfo` (CertificateName, IssuerName, IssuerKind, RenewalTime, IsReady)
  - [ ] Struct `ChainCertInfo` (Subject, Issuer, NotAfter, IsCA)
  - [ ] Struct `ScanRequest` (Clusters, Namespaces, Filter)
  - [ ] Struct `ScanResult` (Certificates, TotalScanned, Summary, ScannedAt)
  - [ ] Struct `ScanSummary` (Valid, Expiring, Expired, Total)
  - [ ] Struct `CopyRequest` (SourceCluster, SourceNamespace, SecretName, TargetClusters, TargetNamespaces)
  - [ ] Struct `UploadRequest` (Name, TLSCrt, TLSKey, TargetClusters, TargetNamespaces)

## Fase 2: Backend - Parser x509
- [ ] Criar `internal/certificates/parser.go`
  - [ ] Funcao `ParseTLSSecret(secret) -> CertificateInfo`
  - [ ] Decodificar PEM (`encoding/pem`)
  - [ ] Parsear x509 (`crypto/x509`)
  - [ ] Extrair chain completo (multiplos blocos PEM)
  - [ ] Extrair Subject, Issuer, SANs, SerialNumber, KeyAlgorithm, KeySize
  - [ ] Calcular status: "valid", "expiring" (<30d), "expired"
  - [ ] Calcular DaysRemaining (negativo se expirado)

## Fase 3: Backend - Scanner Multi-Cluster
- [ ] Criar `internal/certificates/scanner.go`
  - [ ] Struct `Scanner` com dependencias (K8s client cache, logger)
  - [ ] Funcao `ScanClusters(ctx, ScanRequest) -> ScanResult`
  - [ ] Para cada cluster: listar Secrets tipo `kubernetes.io/tls`
  - [ ] Para cada Secret: chamar `ParseTLSSecret()`
  - [ ] Listar Ingresses e cruzar `spec.tls[].secretName`
  - [ ] Preencher `UsedByIngresses` em cada certificado
  - [ ] Detectar cert-manager via annotations no Secret:
    - [ ] `cert-manager.io/certificate-name`
    - [ ] `cert-manager.io/issuer-name`
    - [ ] `cert-manager.io/issuer-kind`
  - [ ] Aplicar filtro: "all", "ingress", "common"
  - [ ] Gerar ScanSummary (contadores valid/expiring/expired)
  - [ ] Funcao `CopyCertificate(ctx, CopyRequest) -> error`
    - [ ] Ler Secret do cluster/namespace origem
    - [ ] Criar/atualizar Secret em cada cluster/namespace destino (cross-cluster)
  - [ ] Funcao `UploadCertificate(ctx, UploadRequest) -> error`
    - [ ] Validar PEM (cert + key)
    - [ ] Criar Secret tipo `kubernetes.io/tls` em cada cluster/namespace destino

## Fase 4: Backend - Handler REST
- [ ] Criar `internal/web/handlers/certificates.go`
  - [ ] Struct `CertificatesHandler` com Scanner e logger
  - [ ] Handler `Scan` - POST `/api/v1/certificates/scan`
  - [ ] Handler `GetDetails` - GET `/api/v1/certificates/:cluster/:namespace/:name`
  - [ ] Handler `Copy` - POST `/api/v1/certificates/copy` (RBAC SRE)
  - [ ] Handler `Upload` - POST `/api/v1/certificates/upload` (RBAC SRE)
  - [ ] Handler `Report` - GET `/api/v1/certificates/report`
- [ ] Registrar rotas em `internal/web/server.go`
  - [ ] Grupo `/api/v1/certificates`
  - [ ] RBAC `RequireSREGroup()` em copy e upload
- [ ] Compilar backend: `go build -mod=vendor ./...`

## Fase 5: Frontend - Tipos TypeScript
- [ ] Criar `internal/web/frontend/src/types/certificates.ts`
  - [ ] Interface `CertificateInfo`
  - [ ] Interface `IngressRef`
  - [ ] Interface `CertManagerInfo`
  - [ ] Interface `ChainCertInfo`
  - [ ] Interface `ScanRequest`
  - [ ] Interface `ScanResult`
  - [ ] Interface `ScanSummary`
  - [ ] Interface `CopyRequest`
  - [ ] Interface `UploadRequest`

## Fase 6: Frontend - Hook React Query
- [ ] Criar `internal/web/frontend/src/hooks/useCertificates.ts`
  - [ ] `useCertificateScan()` - mutation para scan
  - [ ] `useCertificateDetails(cluster, namespace, name)` - query para detalhes
  - [ ] `copyCertificate(request)` - funcao async para copia
  - [ ] `uploadCertificate(request)` - funcao async para upload

## Fase 7: Frontend - Aba CertificatesTab
- [ ] Criar `internal/web/frontend/src/components/CertificatesTab.tsx`
  - [ ] **Painel Superior - Configuracao de Scan**
    - [ ] Multi-select de clusters (checkboxes: Todos, Producao, Homologacao, individual)
    - [ ] Filtro por tipo: Radio (Todos, Usados por Ingress, Comuns multi-namespace)
    - [ ] Filtro por status: Checkboxes (Validos, Expirando <30d, Expirados)
    - [ ] Botao "Escanear" com loading state
  - [ ] **Painel Central - Tabela de Resultados**
    - [ ] Colunas: Status, Secret Name, Namespace, Cluster, Subject/CN, Expira Em, Dias Restantes, Ingresses
    - [ ] Ordenacao por coluna (default: dias restantes ASC)
    - [ ] Busca por texto (nome, namespace, subject)
    - [ ] Badges coloridos: verde (valid), amarelo (expiring), vermelho (expired)
    - [ ] Botao "Exportar Relatorio"
  - [ ] **Painel Lateral - Detalhes do Certificado**
    - [ ] Dados x509 completos
    - [ ] Timeline visual: emissao -> agora -> expiracao
    - [ ] Lista de Ingresses referenciando o certificado
    - [ ] Info cert-manager (se disponivel)
    - [ ] Chain de certificados
    - [ ] Botao "Copiar para..." (ProtectedAction RBAC)
    - [ ] Botao "Upload novo" (ProtectedAction RBAC)
    - [ ] Menu (3 pontos): Exportar PEM, Ver YAML
  - [ ] **Modal de Copia**
    - [ ] Multi-select clusters destino (incluindo cross-cluster)
    - [ ] Multi-select namespaces destino por cluster
    - [ ] Preview de destinos selecionados
    - [ ] Confirmacao com lista de acoes
  - [ ] **Modal de Upload**
    - [ ] Textarea PEM certificado (tls.crt)
    - [ ] Textarea PEM chave (tls.key)
    - [ ] Input nome do Secret
    - [ ] Multi-select clusters e namespaces destino
    - [ ] Validacao client-side do formato PEM
    - [ ] Preview do certificado parseado
  - [ ] **Relatorios**
    - [ ] Exportar PDF (reusar lib/reportGenerator.ts)
    - [ ] Exportar Markdown
    - [ ] Resumo executivo + tabela + alertas

## Fase 8: Registrar Aba
- [ ] Modificar `internal/web/frontend/src/pages/Index.tsx`
  - [ ] Adicionar TabsTrigger "Certificados TLS"
  - [ ] Adicionar TabsContent com CertificatesTab

## Fase 9: Build e Verificacao Final
- [ ] `go build -mod=vendor ./...` - Backend compila
- [ ] `./rebuild-web.sh -b` - Frontend compila
- [ ] Testar scan em cluster real
- [ ] Testar filtros (tipo, status)
- [ ] Testar detalhes de certificado
- [ ] Testar copia entre namespaces
- [ ] Testar copia cross-cluster
- [ ] Testar upload de certificado
- [ ] Testar exportacao de relatorio
- [ ] `go test -v ./internal/certificates/... -race` - Testes passam

---

## Notas de Implementacao

### Padroes a seguir (arquivos de referencia):
- Handler REST: `internal/web/handlers/secrets.go`
- K8s client: `internal/kubernetes/client.go` (ListSecrets, GetSecret, ListIngresses ja existem)
- RBAC: `internal/web/middleware/rbac.go` + `components/rbac/ProtectedAction`
- React Query: `internal/web/frontend/src/hooks/useHealthChecking.ts`
- Relatorios: `internal/web/frontend/src/lib/reportGenerator.ts`
- Multi-cluster: `internal/web/frontend/src/components/HealthCheckingTab.tsx`

### Dependencias Go (stdlib - sem vendor novo):
- `crypto/x509` - Parse de certificados
- `encoding/pem` - Decode PEM blocks
- `math/big` - Serial number formatting

### Classificacao de clusters:
- Producao: clusters com sufixo `-prd` ou `-prod`
- Homologacao: clusters com sufixo `-hlg` ou `-uat` ou `-sit`
- Desenvolvimento: clusters com sufixo `-dev`

### Thresholds de status:
- **Expirado**: `NotAfter < time.Now()`
- **Expirando**: `NotAfter < time.Now() + 30 dias`
- **Valido**: `NotAfter >= time.Now() + 30 dias`
