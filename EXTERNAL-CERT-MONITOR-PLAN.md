# Monitor de Certificados Externos (endpoints on-prem, estilo blackbox_exporter)

Plano de implementação. Continuar de qualquer chat lendo este arquivo + `CLAUDE.md` (seção
"Certificates" e o histórico do `CERT-ROLLBACK-VALIDATION-PLAN.md`, Fase 7 — handshake TLS
direto, mecanismo reaproveitado aqui).

**Contexto:** o usuário quer, na aplicação, o mesmo tipo de checagem que o job `blackbox` do
Prometheus faz hoje: cadastrar uma lista de rotas (`host:porta`, tipicamente HTTPS) e, sem
precisar montar scrape configs, Prometheus, nem acesso K8s ao alvo, disparar um handshake TLS
real contra cada uma pra saber se o certificado servido está válido e quanto tempo falta pra
expirar. O caso de uso principal é servidor on-prem Windows/Linux fora de qualquer cluster — hoje
a única forma de monitorar certificado TLS na app é via Secret de um cluster K8s (aba
"Certificados TLS"), o que não cobre esse cenário.

A app já tem a peça de baixo nível pronta e reaproveitável: `internal/certificates/
tls_dial_enrich.go` (`dialHostForCert`) já faz `tls.DialWithDialer` com SNI e lê o certificado
real servido — foi construído pra outro fim (Fase 7 do `CERT-ROLLBACK-VALIDATION-PLAN.md`,
comparar o cert de um Ingress/Gateway com o Secret K8s esperado), mas o handshake em si é
genérico e não depende de K8s. Este plano generaliza essa capacidade pra um endpoint cadastrado
livremente pelo usuário, sem nenhum Secret/Ingress/Gateway associado.

**Decisões já confirmadas com o usuário (não reabrir sem motivo forte):**
1. Fica como **sub-aba dentro de "Certificados TLS"** (`CertificatesTab.tsx`) — não uma
   ferramenta nova no Tools Menu.
2. Verificação **sob demanda** (botão "Verificar agora") + **histórico leve** por endpoint — sem
   ticker/cron rodando sozinho em background.
3. **Só badge/cor** na listagem (verde/âmbar/vermelho) — sem integração com o sistema de
   Notificações in-app nesta v1.

**Fora de escopo desta v1 (explícito):**
- Sem ticker/cron em background.
- Sem integração com Notificações in-app.
- Sem probe HTTP/TCP genérico (`tcp_connect`/`http_2xx` do blackbox) — escopo é estritamente
  handshake TLS + validade de certificado.
- Sem import em lote (colar CSV/YAML) — cadastro é um-a-um via formulário.

---

## Fase 1 — Checagem TLS genérica (backend, sem storage ainda)

**Arquivo:** `internal/certificates/endpoint_check.go` ← CRIAR

```go
type EndpointCheckResult struct {
    Success           bool
    ErrorMessage      string
    Subject           string
    Issuer            string
    SerialNumber      string
    NotBefore         time.Time
    NotAfter          time.Time
    DNSNames          []string
    ChainLength       int
    Status            string // "valid" | "expiring" | "expired"
    DaysRemaining     int
    TrustedByPublicCA bool
}

func CheckEndpointTLS(ctx context.Context, host string, port int, sni string) EndpointCheckResult
```

- Dial: `tls.DialWithDialer` com `net.JoinHostPort(host, strconv.Itoa(port))`, `ServerName:
  sni_ou_host`, `InsecureSkipVerify: true` (mesmo racional de `dialHostForCert`: ler o cert real
  mesmo autoassinado — comum em servidor interno on-prem; confiança é reportada à parte via
  `TrustedByPublicCA`). Timeout por host: 5s (mais folga que os 3s de `tlsDialTimeoutPerHost`,
  pra cobrir link on-prem mais lento que um cluster K8s).
- Status/DaysRemaining: **extrair** a lógica hoje duplicável em `parser.go:ParseTLSSecret`
  (usa `certificates.ExpiringThresholdDays = 30`) para uma função pura `classifyExpiry(notAfter
  time.Time) (status string, daysRemaining int)`, chamada pelos dois caminhos (Secret K8s e
  endpoint externo) — evita duplicar o limiar em dois lugares.
- `TrustedByPublicCA`: `leaf.Verify(x509.VerifyOptions{Intermediates: pool_da_chain, DNSName:
  host})` contra `x509.SystemCertPool()`; falha é **não-fatal**, só define `false` (mesmo
  espírito de `TrustedByPublicCA=false` tratado como neutro em `ChainValidationResult`).
- Erro de dial (timeout, connection refused, DNS): `Success: false`, `ErrorMessage` com a
  mensagem do erro Go, sem campos de certificado preenchidos.

**Arquivo:** `internal/certificates/endpoint_check_test.go` ← CRIAR
- `httptest.NewTLSServer` (servidor TLS real e local, sem mock de rede) pra handshake
  bem-sucedido + extração de campos.
- Caso de erro: dial contra porta fechada.
- `classifyExpiry` nos 3 limiares (valid/expiring/expired).

- [x] Criar `endpoint_check.go` com `CheckEndpointTLS`/`classifyExpiry`
- [x] Refatorar `parser.go:ParseTLSSecret` pra usar `classifyExpiry` (sem mudar comportamento)
- [x] Criar `endpoint_check_test.go`
- [x] `go test ./internal/certificates/... -race` — suite completa (novos + existentes) OK

---

## Fase 2 — Storage

**Arquivo:** `internal/storage/cert_endpoints_store.go` ← CRIAR

Mesmo padrão de `notes_store.go`/`snat_history_store.go`: banco SQLite próprio em
`~/.k8s-hpa-manager/cert-endpoints.db`, WAL mode, `sync.RWMutex`.

```sql
CREATE TABLE cert_endpoints (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL,
    host        TEXT NOT NULL,
    port        INTEGER NOT NULL DEFAULT 443,
    sni         TEXT,               -- override do ServerName; vazio = usa host
    group_label TEXT,               -- tag livre do usuário (ex: "on-prem", "windows")
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_by  TEXT NOT NULL,
    created_at  DATETIME NOT NULL
);

CREATE TABLE cert_endpoint_checks (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint_id    INTEGER NOT NULL REFERENCES cert_endpoints(id) ON DELETE CASCADE,
    checked_at     DATETIME NOT NULL,
    success        INTEGER NOT NULL,
    error_message  TEXT,
    subject        TEXT, issuer TEXT, serial_number TEXT,
    not_before     DATETIME, not_after DATETIME,
    dns_names      TEXT,     -- JSON array
    chain_length   INTEGER,
    status         TEXT,     -- "valid" | "expiring" | "expired"
    days_remaining INTEGER,
    trusted_by_public_ca INTEGER
);
CREATE INDEX idx_cert_endpoint_checks_endpoint ON cert_endpoint_checks(endpoint_id, checked_at DESC);
```

Métodos: `Create/Update/Delete/List(endpoints)`, `RecordCheck(endpointID, result)` (insere e
poda — mantém só as últimas 20 checagens por endpoint via `DELETE ... WHERE endpoint_id=? AND id
NOT IN (SELECT id ... ORDER BY checked_at DESC LIMIT 20)`), `GetLatestCheck(endpointID)`,
`GetHistory(endpointID, limit)`, `ListWithLatestCheck()` (join único pra alimentar a listagem
sem N+1 queries).

**Arquivo:** `internal/storage/cert_endpoints_store_test.go` ← CRIAR
- CRUD básico + poda de histórico acima de 20 checagens.

- [x] Criar `cert_endpoints_store.go`
- [x] Criar `cert_endpoints_store_test.go`
- [x] `go test ./internal/storage/... -race` — suite completa (novos + existentes) OK

---

## Fase 3 — Handler + rotas

**Arquivo:** `internal/web/handlers/cert_endpoints.go` ← CRIAR

```go
type CertEndpointsHandler struct {
    store *storage.CertEndpointsStore
}

GET    /api/v1/cert-endpoints                  → List() com latest check embutido
POST   /api/v1/cert-endpoints                  → Create()  [InjectUserEmail — grava created_by]
PUT    /api/v1/cert-endpoints/:id               → Update()
DELETE /api/v1/cert-endpoints/:id               → Delete()
POST   /api/v1/cert-endpoints/:id/check         → CheckOne()   (1 endpoint, sob demanda)
POST   /api/v1/cert-endpoints/check-all         → CheckAll()   (todos os enabled, paralelo)
GET    /api/v1/cert-endpoints/:id/history       → History()
```

`CheckAll`: semáforo limitando concorrência (mesmo padrão de `access_check_scan.go`, `sem :=
make(chan struct{}, 8)` + `sync.WaitGroup`), roda `CheckEndpointTLS` pra cada endpoint
habilitado, grava cada resultado via `RecordCheck`, retorna a lista atualizada.

**Arquivo:** `internal/web/server.go` ← MODIFICAR
- Registrar grupo `cert-endpoints`, **sem** `RequireSREGroup()` — é config local da própria
  ferramenta, não mutação de cluster (mesmo racional de Notes); só autoria via
  `InjectUserEmail()` nas rotas de escrita (Create/Update/Delete).
- Inicializar `CertEndpointsStore` no bloco dos stores existentes.

- [x] Criar `cert_endpoints.go` (handler)
- [x] Registrar rotas + inicializar store em `server.go`
- [x] `go build ./...` / `go vet ./...`
- [x] Smoke test manual via curl (JWT gerado a partir do `jwt.secret` real, sem depender do
      navegador — mesma técnica já usada antes nesta app): Create/List/Update/Delete/History/
      CheckOne/CheckAll validados contra `www.google.com` (certificado real, `trusted_by_public_ca:
      true`) e contra uma porta fechada (erro real de conexão recusada). Bug cosmético achado e
      corrigido nessa validação: `CheckOne` devolvia `checked_at` zerado (RecordCheck gerava seu
      próprio timestamp internamente, sem devolvê-lo ao chamador) — `RecordCheck` agora usa o
      `CheckedAt` já setado pelo chamador quando presente.

---

## Fase 4 — Frontend: API client, tipos, hook

**Arquivo:** `internal/web/frontend/src/lib/api/types.ts` ← MODIFICAR
- `CertEndpoint`, `CertEndpointCheck`, `CertEndpointWithStatus` (endpoint + latest check
  embutido, shape que a listagem consome).

**Arquivo:** `internal/web/frontend/src/lib/api/client.ts` ← MODIFICAR
- `listCertEndpoints`, `createCertEndpoint`, `updateCertEndpoint`, `deleteCertEndpoint`,
  `checkAllCertEndpoints`, `checkCertEndpoint`, `getCertEndpointHistory` — mesmo padrão de
  `this.request(...)` já usado no resto do client.

**Arquivo:** `internal/web/frontend/src/hooks/useCertEndpoints.ts` ← CRIAR
- React Query, mesmo padrão de `useNotes.ts` (`queryKey: ['cert-endpoints']`, `useMutation` +
  `invalidateQueries` no `onSuccess` pra create/update/delete/check).

- [x] Tipos em `types.ts`
- [x] Métodos em `client.ts`
- [x] `useCertEndpoints.ts`
- [x] `npx tsc --noEmit` (sem erros novos) + `npm run build` (produção, sem erros)

---

## Fase 5 — Frontend: UI

**Arquivo:** `internal/web/frontend/src/components/CertificatesTab.tsx` ← MODIFICAR
- Adicionar barra de 2 abas manuais no topo (div + estado local, **nunca** shadcn `<Tabs>` —
  quebra `flex-1 min-h-0`, e o `SplitView` atual depende dessa cadeia): "Certificados K8s" (todo
  o conteúdo/SplitView atual, sem alteração) e "Endpoints Externos" (painel novo). Mesmo padrão
  visual de tab bar já usado em `ServiceNowImportModal.tsx`/`FinOpsTab.tsx`.

**Arquivo:** `internal/web/frontend/src/components/ExternalCertEndpointsPanel.tsx` ← CRIAR
- Botão "Verificar agora" (topo) → `checkAllCertEndpoints`, com spinner e timestamp da última
  rodada.
- Botão "Adicionar Endpoint" → form pequeno: Nome, Host, Porta (default 443), SNI (opcional,
  tooltip "só necessário se o host não bate com o hostname real do certificado"), Grupo
  (opcional, texto livre).
- Tabela: Nome | Host:Porta | Status (badge verde/âmbar/vermelho + "N dias" ou "erro: …") |
  Emissor | Última verificação | ações (recheck individual via `checkCertEndpoint` / editar /
  excluir com confirmação).
- Clique na linha → modal de detalhe simples (Subject, Issuer, Serial, SAN, chain length,
  confiável por CA pública sim/não, histórico das últimas checagens via
  `getCertEndpointHistory`).

**Arquivo:** `internal/web/frontend/src/components/ExternalEndpointDetailModal.tsx` ← CRIAR
- Modal novo e enxuto — **não** reaproveita `CertificateDetailModal.tsx` (fortemente tipado em
  cima de `CertificateInfo` com campos K8s-only como `UsedByIngresses`/`UsedByGateways`/
  `CertManager` que não fazem sentido aqui; forçar compatibilidade custaria mais que um modal
  novo).

- [x] Tab bar em `CertificatesTab.tsx`
- [x] `ExternalCertEndpointsPanel.tsx`
- [x] `ExternalEndpointDetailModal.tsx`
- [x] `npx tsc --noEmit` (76 erros pré-existentes em arquivos não tocados, zero novos) +
      `npm run build` (produção, sem erros) + `./rebuild-web.sh -b` + servidor respondendo
      (200 em `/` e no asset JS)
- [ ] ⚠️ Validação visual no navegador NÃO feita nesta sessão (sem ferramenta de automação de
      browser disponível) — confirmar manualmente antes de considerar a Fase 5 100% fechada:
      abrir Certificados TLS → sub-aba "Endpoints Externos" → cadastrar/editar/excluir/verificar
      um endpoint de verdade.

---

## Fase 6 — Validação manual + documentação

- [ ] `./rebuild-web.sh -b` e teste manual real: cadastrar um endpoint (ex: servidor HTTPS
      público conhecido, ou servidor on-prem real do usuário), clicar "Verificar agora",
      confirmar que o certificado real aparece com status/dias corretos.
- [ ] Testar host:porta que falha (porta fechada) — confirmar erro legível na UI.
- [ ] Testar editar/excluir endpoint.
- [ ] Reiniciar o servidor (`kill <PID> && ./build/new-k8s-hpa web -f`) após o build, por
      convenção do projeto.
- [ ] `CLAUDE.md`: nova entrada documentando a feature (seção própria, padrão das demais),
      só depois da validação manual.
- [ ] Nova branch → commit → push → abrir PR (mesmo fluxo das tarefas anteriores desta sessão).
