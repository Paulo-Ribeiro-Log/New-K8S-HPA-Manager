# Plano: Validação de Cadeia de Certificados + Rollback de Atualizações TLS

Status: 🔵 planejado — nenhuma fase iniciada.

## Contexto

Hoje a aba Certificados só faz uma validação superficial ao subir um certificado novo: `certificates.ValidatePEM()` (`internal/certificates/parser.go:85`) apenas confere que existe pelo menos 1 cert no PEM e que a chave tem um bloco PEM decodificável — **não verifica se a chave bate com o certificado, não verifica a cadeia de confiança/ordem, e não detecta cadeia incompleta ou fora de ordem**. Além disso, `Scanner.UploadCertificate` (`internal/certificates/scanner.go:270`) sempre sobrescreve o Secret TLS direto (`Update`, fallback `Create`) **sem nunca ler o conteúdo anterior** — se o certificado novo tiver problema, não existe hoje nenhum jeito de voltar pro certificado antigo a não ser re-subir manualmente o PEM anterior (se alguém ainda tiver guardado).

Pedido do usuário: (1) validar a cadeia de verdade antes/depois de instalar, e (2) um mecanismo de rollback — ao atualizar `MEU-CERT-TLS`, guardar o conteúdo anterior em `rollback-certs/MEU-CERT-TLS/<data>/` antes de sobrescrever, com uma ação de restaurar esse backup em caso de problema com o novo certificado. Validação deve poder ser disparada manualmente e também automaticamente logo após a instalação.

**Achado importante da investigação**: a imagem de produção (`dockerfile`, raiz do repo) é `FROM scratch` — sem shell, sem binário `openssl` garantido. Por isso a validação de cadeia usa **Go nativo (`crypto/x509`/`crypto/tls`) como mecanismo primário e único obrigatório**, com uma tentativa de enriquecimento via `openssl` **totalmente opcional** (só roda se `exec.LookPath("openssl")` achar o binário; se não achar, ignora silenciosamente — nunca é bloqueante). Isso evita repetir a fragilidade que outras integrações CLI (`az`/`aws`/`gcloud`/`kubectl`) já têm nesse ambiente.

**Achado importante #2**: o fluxo AWX de renovação de certificado (`AWXCertForm.tsx` → `AWXHandler.LaunchJob`) **não passa por `Scanner.UploadCertificate`** — quem mexe no Secret é o playbook Ansible remoto, fora do controle deste backend. Um hook de backup só dentro de `UploadCertificate` NÃO cobre esse caminho. Por isso o backup é disparado em dois lugares (ver Fase 2, item 3).

---

## Fase 1 — Validação de cadeia (Go nativo, sem dependência de binário externo)

### Backend

- [ ] `internal/certificates/validate.go` (novo arquivo): struct `ChainValidationResult` (`Valid`, `KeyMatchesCert`, `ChainOrderCorrect`, `TrustedByPublicCA`, `ExpiryOK`, `Errors []string`, `Warnings []string`, `ChainSubjects []string`, `OpenSSLNotes []string`) + `func ValidateCertificateChain(certPEM, keyPEM []byte) (*ChainValidationResult, error)`
- [ ] Passo 1: `parsePEMChain(certPEM)` (já existe em `parser.go:103`) → `certs[0]` leaf, `certs[1:]` intermediários
- [ ] Passo 2 (chave bate com o cert): `tls.X509KeyPair(certPEM, keyPEM)` (stdlib `crypto/tls` — já faz essa checagem sem comparar RSA/ECDSA/Ed25519 manualmente). Erro → `Errors` (bloqueante)
- [ ] Passo 3 (ordem da cadeia): `certs[i].CheckSignatureFrom(certs[i+1])` em sequência — detecta cadeia fora de ordem ou com elo faltando. Erro → `Errors`
- [ ] Passo 4 (confiança/expiração): `certs[0].Verify(x509.VerifyOptions{Intermediates: pool(certs[1:]), CurrentTime: time.Now()})` usando root store do sistema (não passar `Roots` explícito). `x509.UnknownAuthorityError` → **warning** (CA privada é caso comum e válido aqui, não bloqueia); qualquer outro erro do `Verify` (expirado, hostname, uso errado) → `Errors`
- [ ] Passo 5: `ChainSubjects` = `cert.Subject.CommonName` de cada cert da cadeia, na ordem
- [ ] Passo 6 (opcional): `enrichWithOpenSSL(certPEM []byte) []string` — só roda se `exec.LookPath("openssl")` existir; `exec.CommandContext` com timeout 10s (padrão de `internal/pkg/helm/cli_client.go`); erro sempre engolido silenciosamente, nunca afeta `Valid`/`Errors`
- [ ] Endpoint `POST /api/v1/certificates/validate-chain` — body `{cert_pem, key_pem}` → `ChainValidationResult`. Sem RBAC (leitura/cálculo)
- [ ] Endpoint `GET /api/v1/certificates/:cluster/:namespace/:name/validate-chain` — lê o Secret já instalado e roda a mesma validação (cobre disparo manual sobre cert já instalado)
- [ ] `Upload` (e `Rollback`, Fase 2) chamam `ValidateCertificateChain` logo após o Update/Create ter sucesso e incluem o resultado no campo `validation` da resposta — cobre "logo depois do certificado instalado" sem chamada extra do frontend. Falha na validação NÃO desfaz o upload (informativo, não bloqueante)
- [ ] `internal/certificates/validate_test.go`: gerar certs de teste em memória (self-signed + mini-CA de 2 níveis via `crypto/x509`+`crypto/rsa`, sem fixtures externas) cobrindo: cadeia válida, chave que não bate, cadeia fora de ordem, cert expirado, CA privada (deve dar `Warnings`, não `Errors`)

### Frontend

- [ ] `CertificateChainValidationPanel.tsx` (novo, reaproveitável): renderiza `ChainValidationResult` — ✅/⚠️/❌ por checagem, `ChainSubjects` (leaf → ... → raiz), `Errors` em vermelho, `Warnings` em âmbar, `OpenSSLNotes` colapsável se presente
- [ ] `CertificateRenewModal.tsx`: após upload manual (ou job AWX completar via SSE) ter sucesso, renderiza `CertificateChainValidationPanel` inline automaticamente (manual: já vem no campo `validation` da resposta; AWX: disparar `GET .../validate-chain` já que a resposta desse caminho só vem via eventos SSE)
- [ ] `CertificateDetailModal.tsx`: botão "Validar Cadeia" (chama `GET .../validate-chain` sob demanda) — cobre disparo manual sobre cert já instalado há tempos
- [ ] `useCertificates.ts`: `validateChainPEM(certPem, keyPem)`, `validateInstalledChain(cluster, namespace, name)`
- [ ] `types/certificates.ts`: `ChainValidationResult`

---

## Fase 2 — Rollback de atualizações de certificado

### Backend

- [ ] `internal/certificates/rollback.go` (novo arquivo): struct `RollbackStore{ baseDir string }` (`~/.k8s-hpa-manager/rollback-certs/`), `NewRollbackStore() (*RollbackStore, error)` com `MkdirAll(baseDir, 0700)`
- [ ] struct `RollbackBackupInfo` (`BackupID`, `Cluster`, `Namespace`, `SecretName`, `BackedUpAt`, `Subject`, `SerialNumber`, `NotAfter`)
- [ ] `func (r *RollbackStore) Backup(cluster, namespace, secretName string, secret *corev1.Secret) (RollbackBackupInfo, error)` — grava `tls.crt`/`tls.key`/`metadata.json` (todos 0600) em `<baseDir>/<secretName>/<timestamp>/` (0700); se `parsePEMChain` falhar ao ler o cert antigo (corrompido), salva os bytes brutos mesmo assim e deixa metadata vazio — backup nunca falha por causa de metadata
- [ ] `func (r *RollbackStore) List(secretName string) ([]RollbackBackupInfo, error)` — ordenado mais recente primeiro
- [ ] `func (r *RollbackStore) Get(secretName, backupID string) (tlsCrt, tlsKey []byte, meta RollbackBackupInfo, err error)`
- [ ] `func (r *RollbackStore) Prune(keepPerSecret int) error` — mantém as N mais recentes por `secretName` (padrão: 20), apaga o resto; ticker diário (`go store.cleanupLoop()`, mesmo padrão de `internal/storage/ai_history_store.go`)
- [ ] **Caminho manual/batch**: `Scanner` ganha campo `rollbackStore *RollbackStore` (`NewScanner(km, rollbackStore)`); dentro do loop de `UploadCertificate`, antes do `Update`, faz `Get` do Secret existente e chama `rollbackStore.Backup(...)` se existir — melhor-esforço, erro de backup só loga (`log.Warn`), nunca impede a atualização
- [ ] **Caminho AWX**: novo endpoint `POST /api/v1/certificates/:cluster/:namespace/:name/backup` (RBAC `RequireSREGroup`) → handler `CertificatesHandler.Backup` faz o mesmo Get+Backup. Frontend chama esse endpoint no `onSubmit` de `AWXCertForm`/onde é usado pra certs, antes de `launchAWXCertJob(...)`
- [ ] Endpoint `GET /api/v1/certificates/:cluster/:namespace/:name/rollback` — lista backups (`RollbackStore.List`), sem RBAC (leitura)
- [ ] Endpoint `POST /api/v1/certificates/:cluster/:namespace/:name/rollback` — body `{backup_id}`, RBAC `RequireSREGroup`. Handler `CertificatesHandler.Rollback`:
  - [ ] `RollbackStore.Get(name, backupID)` → `tlsCrt, tlsKey, meta`
  - [ ] Roda `ValidateCertificateChain` de novo como checagem de sanidade (não bloqueia)
  - [ ] Faz backup do estado ATUAL antes de restaurar (reaproveita `Backup()` — rollback também é reversível, "avançar" é só outro rollback)
  - [ ] Sobrescreve o Secret (Get+Update, mesmo padrão do upload)
  - [ ] Registra em `HistoryTracker` (`action: "cert-rollback"`, `Resource: namespace/name`, `Before`/`After` com serial/subject/not_after) — diferente do Upload/Copy existentes (que não logam, limitação já documentada e fora de escopo mudar); rollback já nasce com auditoria
- [ ] `NewCertificatesHandler` passa a receber `historyTracker *history.HistoryTracker` e `rollbackStore *certificates.RollbackStore`; atualizar call site em `server.go`
- [ ] `internal/certificates/rollback_test.go`: `t.TempDir()` pra isolar `baseDir`; testar `Backup`→`List`→`Get` (round-trip), permissões de arquivo (`0600`/`0700` via `os.Stat().Mode()`), `Prune` mantendo só as N mais recentes

### Frontend

- [ ] `CertificateRollbackModal.tsx` (novo): acessível via botão "Backups / Rollback" ao lado de "Atualizar Certificado" no painel de detalhe de `CertificatesTab.tsx`. Lista backups (data, subject, serial, validade) via `GET .../rollback`; cada linha com botão "Restaurar" atrás de `AlertDialog` de confirmação (mesmo padrão de `LoadSessionModal.tsx`)
- [ ] Ao confirmar rollback: chama `POST .../rollback`, mostra `ChainValidationResult` do cert restaurado, recarrega o Secret no editor (`handleSelectSecret`, `refreshTlsCertMap`, `silentRefetch` — mesmo `onSuccess` já usado por `CertificateRenewModal`)
- [ ] `useCertificates.ts`: `backupCertificate(cluster, namespace, name)`, `listRollbacks(cluster, namespace, name)`, `rollbackCertificate(cluster, namespace, name, backupId)`
- [ ] `types/certificates.ts`: `RollbackBackupInfo`
- [ ] `AWXCertForm.tsx`: chama `backupCertificate` antes de `launchAWXCertJob` (só no contexto de certs)

---

## Arquivos afetados

| Arquivo | Mudança |
|---|---|
| `internal/certificates/validate.go` | **novo** — `ValidateCertificateChain`, enriquecimento OpenSSL opcional |
| `internal/certificates/rollback.go` | **novo** — `RollbackStore` (Backup/List/Get/Prune) |
| `internal/certificates/validate_test.go` | **novo** |
| `internal/certificates/rollback_test.go` | **novo** |
| `internal/certificates/scanner.go` | `Scanner` ganha `rollbackStore`; `UploadCertificate` faz backup antes de sobrescrever |
| `internal/web/handlers/certificates.go` | +`Backup`, `ListRollbacks`, `Rollback`, `ValidateChainPEM`, `ValidateInstalledChain`; `NewCertificatesHandler` ganha `historyTracker` |
| `internal/web/server.go` | 5 rotas novas + atualiza call site de `NewCertificatesHandler` |
| `internal/web/frontend/src/lib/api/client.ts` | +5 métodos |
| `internal/web/frontend/src/hooks/useCertificates.ts` | +5 funções |
| `internal/web/frontend/src/types/certificates.ts` | +2 tipos |
| `internal/web/frontend/src/components/CertificateChainValidationPanel.tsx` | **novo** |
| `internal/web/frontend/src/components/CertificateRollbackModal.tsx` | **novo** |
| `internal/web/frontend/src/components/CertificateRenewModal.tsx` | validação automática pós-instalação |
| `internal/web/frontend/src/components/CertificateDetailModal.tsx` | botão "Validar Cadeia" |
| `internal/web/frontend/src/components/CertificatesTab.tsx` | botão "Backups / Rollback"; chama `backupCertificate` antes do submit AWX |
| `internal/web/frontend/src/components/AWXCertForm.tsx` | chama `backupCertificate` antes de `launchAWXCertJob` (só quando usado no contexto de cert) |

## Verificação

- [ ] `go build ./...`, `go vet ./internal/certificates/... ./internal/web/handlers/...`, `go test ./internal/... -race` (novos testes de `validate`/`rollback` + suíte existente sem regressão)
- [ ] `tsc --noEmit`, `eslint .` no frontend
- [ ] Teste manual ponta a ponta via curl: gerar um cert self-signed local (`openssl req -x509 -newkey rsa:2048 ...` só pra criar material de teste, não como dependência da feature) → upload → conferir pasta `~/.k8s-hpa-manager/rollback-certs/<nome>/<data>/` criada com o cert ANTERIOR → upload de um segundo cert (chave trocada de propósito) → `validate-chain` deve reportar `key_matches_cert:false` → `rollback` pro backup do primeiro cert → `validate-chain` deve voltar a `valid:true`
- [ ] Validar em navegador (`rebuild-web.sh -b`) os 3 pontos de UI: botão "Validar Cadeia" no detalhe, painel de resultado após upload, modal de rollback listando e restaurando um backup
