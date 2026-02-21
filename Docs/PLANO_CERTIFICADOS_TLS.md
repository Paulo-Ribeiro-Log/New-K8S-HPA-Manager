# Plano: Aba Certificados TLS - Gestao Completa

## Contexto

O projeto atualmente nao oferece visibilidade sobre certificados TLS nos clusters Kubernetes. Certificados expirados ou prestes a expirar causam incidentes em producao sem aviso previo. A solucao e criar uma aba dedicada "Certificados TLS" que:

1. **Escaneia** todos os Secrets do tipo `kubernetes.io/tls` e CRDs cert-manager em multiplos clusters
2. **Parseia** dados x509 (validade, expiracao, SANs, issuer, cadeia)
3. **Cruza** referencias com Ingresses que usam cada certificado
4. **Gera relatorios** filtrando por cluster (producao/homologacao/todos), tipo (comuns/ingress/todos) e status (validos/expirando/expirados)
5. **Atualiza** certificados: upload manual + copia entre namespaces/clusters (inclusive cross-cluster)

---

## Arquivos a Criar/Modificar

### Backend (Go) - Novos
1. `internal/certificates/parser.go` - Parse x509 de Secrets TLS (~150 linhas)
2. `internal/certificates/scanner.go` - Scan multi-cluster + cross-ref Ingress (~300 linhas)
3. `internal/certificates/models.go` - Structs: CertificateInfo, ScanResult, etc (~100 linhas)
4. `internal/web/handlers/certificates.go` - REST API handler (~400 linhas)

### Backend (Go) - Modificar
5. `internal/web/server.go` - Registrar rotas `/api/v1/certificates/*`
6. `internal/kubernetes/client.go` - Metodos auxiliares se necessario (ListIngresses ja existe)

### Frontend (React/TypeScript) - Novos
7. `internal/web/frontend/src/components/CertificatesTab.tsx` - Aba principal (~800 linhas)
8. `internal/web/frontend/src/hooks/useCertificates.ts` - Hook React Query (~100 linhas)
9. `internal/web/frontend/src/types/certificates.ts` - Tipos TypeScript (~60 linhas)

### Frontend - Modificar
10. `internal/web/frontend/src/pages/Index.tsx` - Adicionar aba no TabsList

---

## Fase 1: Backend - Models (`internal/certificates/models.go`)

```go
package certificates

import "time"

type CertificateInfo struct {
    // Identificacao
    SecretName    string `json:"secretName"`
    Namespace     string `json:"namespace"`
    Cluster       string `json:"cluster"`

    // Dados x509
    Subject       string    `json:"subject"`
    Issuer        string    `json:"issuer"`
    SerialNumber  string    `json:"serialNumber"`
    NotBefore     time.Time `json:"notBefore"`
    NotAfter      time.Time `json:"notAfter"`
    DNSNames      []string  `json:"dnsNames"`      // SANs
    IsCA          bool      `json:"isCA"`
    KeyAlgorithm  string    `json:"keyAlgorithm"`
    KeySize       int       `json:"keySize"`

    // Status calculado
    Status        string `json:"status"`        // "valid", "expiring", "expired"
    DaysRemaining int    `json:"daysRemaining"` // Negativo se expirado

    // Cross-references
    UsedByIngresses []IngressRef `json:"usedByIngresses"`

    // cert-manager (se disponivel)
    CertManager *CertManagerInfo `json:"certManager,omitempty"`

    // Chain
    ChainLength   int              `json:"chainLength"`
    ChainDetails  []ChainCertInfo  `json:"chainDetails,omitempty"`
}

type IngressRef struct {
    Name      string   `json:"name"`
    Namespace string   `json:"namespace"`
    Hosts     []string `json:"hosts"`
}

type CertManagerInfo struct {
    CertificateName string `json:"certificateName"`
    IssuerName      string `json:"issuerName"`
    IssuerKind      string `json:"issuerKind"` // Issuer ou ClusterIssuer
    RenewalTime     string `json:"renewalTime,omitempty"`
    IsReady         bool   `json:"isReady"`
}

type ChainCertInfo struct {
    Subject  string    `json:"subject"`
    Issuer   string    `json:"issuer"`
    NotAfter time.Time `json:"notAfter"`
    IsCA     bool      `json:"isCA"`
}

type ScanRequest struct {
    Clusters   []string `json:"clusters"`            // Lista de clusters ou vazio = todos
    Namespaces []string `json:"namespaces,omitempty"` // Filtro opcional
    Filter     string   `json:"filter,omitempty"`     // "all", "ingress", "common"
}

type ScanResult struct {
    Certificates []CertificateInfo `json:"certificates"`
    TotalScanned int               `json:"totalScanned"`
    Summary      ScanSummary       `json:"summary"`
    ScannedAt    time.Time         `json:"scannedAt"`
}

type ScanSummary struct {
    Valid    int `json:"valid"`
    Expiring int `json:"expiring"` // < 30 dias
    Expired  int `json:"expired"`
    Total    int `json:"total"`
}

type CopyRequest struct {
    SourceCluster    string   `json:"sourceCluster"`
    SourceNamespace  string   `json:"sourceNamespace"`
    SecretName       string   `json:"secretName"`
    TargetClusters   []string `json:"targetClusters"`   // Pode ser cross-cluster
    TargetNamespaces []string `json:"targetNamespaces"`
}

type UploadRequest struct {
    Name             string   `json:"name"`
    TLSCrt           string   `json:"tlsCrt"`           // Base64 PEM
    TLSKey           string   `json:"tlsKey"`           // Base64 PEM
    TargetClusters   []string `json:"targetClusters"`
    TargetNamespaces []string `json:"targetNamespaces"`
}
```

## Fase 2: Backend - Parser x509 (`internal/certificates/parser.go`)

Usar `crypto/x509` e `encoding/pem` do Go stdlib:

```go
func ParseTLSSecret(secret *corev1.Secret) (*CertificateInfo, error)
```

- Decodificar `tls.crt` (PEM -> x509.Certificate)
- Parsear chain completo (multiplos blocos PEM)
- Extrair: Subject, Issuer, SANs, NotBefore/NotAfter, KeyAlgorithm, SerialNumber
- Calcular status: "expired" (NotAfter < now), "expiring" (< 30 dias), "valid"
- Calcular DaysRemaining

## Fase 3: Backend - Scanner (`internal/certificates/scanner.go`)

```go
func (s *Scanner) ScanClusters(ctx context.Context, req ScanRequest) (*ScanResult, error)
```

Fluxo:
1. Para cada cluster: `ListSecrets` filtrando `type=kubernetes.io/tls`
2. Para cada Secret TLS: chamar `ParseTLSSecret()`
3. `ListIngresses` e cruzar `spec.tls[].secretName` com Secrets encontrados
4. Se cert-manager disponivel: usar dynamic client para buscar `Certificate` CRDs
   - Verificar se CRD existe: `kubectl api-resources | grep certificates.cert-manager.io`
   - Se sim: buscar annotations `cert-manager.io/certificate-name` e `cert-manager.io/issuer-name` no Secret
   - Buscar recurso `Certificate` correspondente para obter issuer e renewal info
5. Aplicar filtro: "ingress" (so certificados usados por Ingresses), "common" (usados por >1 namespace/ingress), "all"
6. Gerar resumo (valid/expiring/expired)

**Deteccao cert-manager**: Verificar annotations no Secret antes de tentar CRDs:
- `cert-manager.io/certificate-name`
- `cert-manager.io/issuer-name` / `cert-manager.io/issuer-kind`
- `cert-manager.io/common-name`

## Fase 4: Backend - Handler REST (`internal/web/handlers/certificates.go`)

Endpoints:

| Metodo | Rota | Descricao |
|--------|------|-----------|
| `POST` | `/api/v1/certificates/scan` | Escanear certificados (body: ScanRequest) |
| `GET` | `/api/v1/certificates/:cluster/:namespace/:name` | Detalhes de um certificado |
| `POST` | `/api/v1/certificates/copy` | Copiar certificado (body: CopyRequest) - RBAC SRE |
| `POST` | `/api/v1/certificates/upload` | Upload certificado (body: UploadRequest) - RBAC SRE |
| `GET` | `/api/v1/certificates/report` | Gerar relatorio (query: clusters, filter, status) |

- Operacoes de escrita (copy, upload) protegidas por `rbacMiddleware.RequireSREGroup()`
- Scan e leitura - sem RBAC

## Fase 5: Frontend - Tipos (`internal/web/frontend/src/types/certificates.ts`)

Interfaces TypeScript espelhando os models Go (CertificateInfo, ScanResult, ScanSummary, etc).

## Fase 6: Frontend - Hook (`internal/web/frontend/src/hooks/useCertificates.ts`)

- `useCertificateScan(request)` - React Query mutation para scan
- `useCertificateDetails(cluster, namespace, name)` - Detalhes
- `copyCertificate(request)` / `uploadCertificate(request)` - Operacoes de escrita

## Fase 7: Frontend - Aba (`internal/web/frontend/src/components/CertificatesTab.tsx`)

Layout da aba:

### Painel Superior - Configuracao de Scan
- **Multi-select de clusters**: Checkboxes para selecionar clusters (incluir opcao "Todos", "Producao", "Homologacao")
- **Filtro por tipo**: Radio buttons - "Todos", "Usados por Ingress", "Comuns (multi-namespace)"
- **Filtro por status**: Checkboxes - "Validos", "Expirando (<30d)", "Expirados"
- **Botao "Escanear"**: Executa o scan

### Painel Central - Tabela de Resultados
- Colunas: Status (badge colorido), Secret Name, Namespace, Cluster, Subject/CN, Expira Em, Dias Restantes, Ingresses
- Ordenacao por coluna (default: dias restantes ASC - mais urgentes primeiro)
- Filtro/busca por texto (nome, namespace, subject)
- Badges: verde (valid), amarelo (expiring <30d), vermelho (expired)

### Painel Lateral (ao selecionar certificado) - Detalhes
- Dados x509 completos (Subject, Issuer, SANs, Serial, Algorithm)
- Timeline visual: emissao -> agora -> expiracao
- Lista de Ingresses que usam este certificado
- Info cert-manager (se disponivel)
- Chain de certificados (intermediarios + root)
- Botoes de acao:
  - "Copiar para..." -> Modal com selecao de clusters/namespaces destino
  - "Upload novo certificado" -> Modal com inputs para PEM cert + key
  - Menu (3 pontos): "Exportar PEM", "Ver YAML"

### Modal de Copia
- Select de clusters destino (multi-select, inclui clusters diferentes do origem)
- Select de namespaces destino por cluster
- Preview: lista de destinos selecionados
- Confirmacao com lista de acoes que serao executadas

### Modal de Upload
- Textarea para PEM do certificado (tls.crt)
- Textarea para PEM da chave (tls.key)
- Input para nome do Secret
- Multi-select de clusters e namespaces destino
- Validacao client-side: verifica se PEM e valido (formato)
- Preview do certificado parseado antes de confirmar

### Relatorios
- Botao "Exportar Relatorio" no header da tabela
- Formatos: PDF e Markdown (reusar padrao de `lib/reportGenerator.ts`)
- Conteudo: resumo executivo + tabela detalhada de certificados + alertas

---

## Fase 8: Registrar Rotas e Aba

### `internal/web/server.go`
```go
certGroup := v1.Group("/certificates")
{
    certGroup.POST("/scan", certHandler.Scan)
    certGroup.GET("/:cluster/:namespace/:name", certHandler.GetDetails)
    certGroup.POST("/copy", rbacMiddleware.RequireSREGroup(), certHandler.Copy)
    certGroup.POST("/upload", rbacMiddleware.RequireSREGroup(), certHandler.Upload)
    certGroup.GET("/report", certHandler.Report)
}
```

### `internal/web/frontend/src/pages/Index.tsx`
- Adicionar `<TabsTrigger value="certificates">Certificados TLS</TabsTrigger>`
- Adicionar `<TabsContent value="certificates"><CertificatesTab /></TabsContent>`

---

## Padroes Existentes a Reutilizar

| Padrao | Arquivo Referencia | Uso |
|--------|-------------------|-----|
| Handler REST com K8s client | `handlers/secrets.go` | Padrao de handler |
| ListSecrets/GetSecret | `kubernetes/client.go` | Ja existem |
| ListIngresses | `kubernetes/client.go` | Ja existe |
| ProtectedAction RBAC | `components/rbac/ProtectedAction` | Botoes protegidos |
| Report Generator | `lib/reportGenerator.ts` | PDF/Markdown |
| Multi-cluster tabs | `HealthCheckingTab.tsx` | Selecao de clusters |
| Monaco YAML Editor | Varias abas | Visualizacao YAML |
| React Query mutations | `hooks/useHealthChecking.ts` | Padrao de mutation |
| Toast notifications | `sonner` | Feedback de operacoes |
| Badge coloridos | Varias abas | Status visual |

---

## Verificacao

1. `go build -mod=vendor ./...` - Compila sem erros
2. `./rebuild-web.sh -b` - Frontend compila
3. Abrir aba "Certificados TLS", selecionar cluster, clicar "Escanear"
4. Tabela exibe certificados com status colorido (verde/amarelo/vermelho)
5. Clicar em certificado -> painel lateral com detalhes x509
6. Filtrar por "Expirados" -> mostra apenas expirados
7. Filtrar por "Usados por Ingress" -> mostra apenas certs referenciados
8. Copiar certificado para outro namespace/cluster (RBAC SRE)
9. Upload de novo certificado com validacao PEM
10. Exportar relatorio PDF/Markdown
11. `go test -v ./internal/certificates/... -race` - Testes passam
