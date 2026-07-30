# Plano: Validação de Cadeia de Certificados + Rollback de Atualizações TLS

Status: ✅ Fase 1 (validação de cadeia) concluída. ✅ Fase 2 (rollback) concluída — endpoints validados via curl/JWT real, ainda sem round-trip de escrita contra um cluster real (evitado por ser uma ação de escrita num sistema compartilhado — pendente de confirmação antes de rodar). ✅ Fase 3 (enriquecimento via Prometheus) concluída — métricas confirmadas empiricamente contra `akspriv-adanalytics-prd` real. ✅ Fase 4 (correções pós-implementação, achadas via uso real/feedback do usuário) concluída — 6 bugs reais corrigidos, ver seção própria. UI das 4 fases passou por `tsc`/`eslint` mas **segue sem teste visual em navegador** nesta série de rodadas (sem ferramenta de automação de browser disponível neste ambiente) — validar isso é o próximo passo recomendado antes de dar a feature por encerrada.

## Contexto

Hoje a aba Certificados só faz uma validação superficial ao subir um certificado novo: `certificates.ValidatePEM()` (`internal/certificates/parser.go:85`) apenas confere que existe pelo menos 1 cert no PEM e que a chave tem um bloco PEM decodificável — **não verifica se a chave bate com o certificado, não verifica a cadeia de confiança/ordem, e não detecta cadeia incompleta ou fora de ordem**. Além disso, `Scanner.UploadCertificate` (`internal/certificates/scanner.go:270`) sempre sobrescreve o Secret TLS direto (`Update`, fallback `Create`) **sem nunca ler o conteúdo anterior** — se o certificado novo tiver problema, não existe hoje nenhum jeito de voltar pro certificado antigo a não ser re-subir manualmente o PEM anterior (se alguém ainda tiver guardado).

Pedido do usuário: (1) validar a cadeia de verdade antes/depois de instalar, e (2) um mecanismo de rollback — ao atualizar `MEU-CERT-TLS`, guardar o conteúdo anterior em `rollback-certs/MEU-CERT-TLS/<data>/` antes de sobrescrever, com uma ação de restaurar esse backup em caso de problema com o novo certificado. Validação deve poder ser disparada manualmente e também automaticamente logo após a instalação.

**Achado importante da investigação**: a imagem de produção (`dockerfile`, raiz do repo) é `FROM scratch` — sem shell, sem binário `openssl` garantido. Por isso a validação de cadeia usa **Go nativo (`crypto/x509`/`crypto/tls`) como mecanismo primário e único obrigatório**, com uma tentativa de enriquecimento via `openssl` **totalmente opcional** (só roda se `exec.LookPath("openssl")` achar o binário; se não achar, ignora silenciosamente — nunca é bloqueante). Isso evita repetir a fragilidade que outras integrações CLI (`az`/`aws`/`gcloud`/`kubectl`) já têm nesse ambiente.

**Achado importante #2**: o fluxo AWX de renovação de certificado (`AWXCertForm.tsx` → `AWXHandler.LaunchJob`) **não passa por `Scanner.UploadCertificate`** — quem mexe no Secret é o playbook Ansible remoto, fora do controle deste backend. Um hook de backup só dentro de `UploadCertificate` NÃO cobre esse caminho. Por isso o backup é disparado em dois lugares (ver Fase 2, item 3).

---

## Fase 1 — Validação de cadeia (Go nativo, sem dependência de binário externo) ✅ concluída

### Backend

- [x] `internal/certificates/validate.go` (novo arquivo): struct `ChainValidationResult` (`Valid`, `KeyMatchesCert`, `ChainOrderCorrect`, `TrustedByPublicCA`, `ExpiryOK`, `Errors []string`, `Warnings []string`, `ChainSubjects []string`, `OpenSSLNotes []string`) + `func ValidateCertificateChain(certPEM, keyPEM []byte) (*ChainValidationResult, error)`
- [x] Passo 1: `parsePEMChain(certPEM)` (já existe em `parser.go:103`) → `certs[0]` leaf, `certs[1:]` intermediários
- [x] Passo 2 (chave bate com o cert): `tls.X509KeyPair(certPEM, keyPEM)` (stdlib `crypto/tls` — já faz essa checagem sem comparar RSA/ECDSA/Ed25519 manualmente). Erro → `Errors` (bloqueante)
- [x] Passo 3 (ordem da cadeia): `certs[i].CheckSignatureFrom(certs[i+1])` em sequência — detecta cadeia fora de ordem ou com elo faltando. Erro → `Errors`
- [x] Passo 4 (confiança/expiração): `certs[0].Verify(x509.VerifyOptions{Intermediates: pool(certs[1:]), CurrentTime: time.Now()})` usando root store do sistema (não passar `Roots` explícito). `x509.UnknownAuthorityError` → **warning** (CA privada é caso comum e válido aqui, não bloqueia); qualquer outro erro do `Verify` (expirado, hostname, uso errado) → `Errors`
- [x] Passo 5: `ChainSubjects` = `cert.Subject.CommonName` de cada cert da cadeia, na ordem
- [x] Passo 6 (opcional): `enrichWithOpenSSL(certPEM []byte) []string` — só roda se `exec.LookPath("openssl")` existir; `exec.CommandContext` com timeout 10s (padrão de `internal/pkg/helm/cli_client.go`); erro sempre engolido silenciosamente, nunca afeta `Valid`/`Errors`. Validado: openssl está disponível neste ambiente e o enriquecimento roda de verdade
- [x] Endpoint `POST /api/v1/certificates/validate-chain` — body `{cert_pem, key_pem}` → `ChainValidationResult`. Sem RBAC (leitura/cálculo). Validado via curl real (cadeia válida + chave incompatível)
- [x] Endpoint `GET /api/v1/certificates/:cluster/:namespace/:name/validate-chain` — lê o Secret já instalado (`Scanner.GetRawTLSSecret`, novo) e roda a mesma validação. **Não validado contra cluster real** — VPN pra AKS privado estava fora do ar durante o desenvolvimento; rotas registradas sem conflito e compilam corretamente, mas o caminho de leitura do Secret real não foi exercitado ponta a ponta
- [x] `Upload` chama `ValidateCertificateChain` logo após o Update/Create ter sucesso, campo `validation` na resposta (não bloqueia o upload em caso de falha de validação). Rollback (Fase 2) vai reaproveitar o mesmo padrão quando implementado
- [x] `internal/certificates/validate_test.go`: 5 testes com certs gerados em memória (self-signed + mini-CA de 2 níveis via `crypto/x509`+`crypto/rsa`) — cadeia válida (CA privada → warning, não erro), chave incompatível, cadeia fora de ordem, cert expirado, PEM inválido. Todos passando

### Frontend

- [x] `CertificateChainValidationPanel.tsx` (novo, reaproveitável): renderiza `ChainValidationResult` — ✅/⚠️/❌ por checagem, `ChainSubjects` (leaf → ... → raiz), `Errors` em vermelho, `Warnings` em âmbar, `OpenSSLNotes` colapsável
- [x] `CertificateRenewModal.tsx`: após upload manual (campo `validation` já vem na resposta de `/upload` via novo `uploadCertificateWithValidation`) ou job AWX completar (busca via `GET .../validate-chain`, já que a resposta AWX só vem via SSE sem devolver o cert), renderiza `CertificateChainValidationPanel` inline automaticamente — modal fica aberto pra exibir o resultado, fecha só quando o usuário clica "Fechar"
- [x] `CertificateDetailModal.tsx`: botão "Validar Cadeia" no footer (chama `GET .../validate-chain` sob demanda) — cobre disparo manual sobre cert já instalado há tempos
- [x] `useCertificates.ts`: `validateChainPEM`, `validateInstalledChain`, `uploadCertificateWithValidation` (nova, não altera assinatura de `uploadCertificate` existente pra não quebrar os 2 call sites já existentes em `CertificatesTab.tsx`)
- [x] `types/certificates.ts`: `ChainValidationResult`
- [x] `tsc --noEmit` e `eslint` sem erros novos

---

## Fase 2 — Rollback de atualizações de certificado ✅ concluída

### Backend

- [x] `internal/certificates/rollback.go` (novo arquivo): struct `RollbackStore{ baseDir string }` (`~/.k8s-hpa-manager/rollback-certs/`), `NewRollbackStore() (*RollbackStore, error)` com `MkdirAll(baseDir, 0700)`
- [x] struct `RollbackBackupInfo` (`BackupID`, `Cluster`, `Namespace`, `SecretName`, `BackedUpAt`, `Subject`, `SerialNumber`, `NotAfter`)
- [x] `func (r *RollbackStore) Backup(cluster, namespace, secretName string, secret *corev1.Secret) (RollbackBackupInfo, error)` — grava `tls.crt`/`tls.key`/`metadata.json` (todos 0600) em `<baseDir>/<secretName>/<timestamp>/` (0700); se `parsePEMChain` falhar ao ler o cert antigo (corrompido), salva os bytes brutos mesmo assim e deixa metadata vazio. Colisão de timestamp (2 backups no mesmo segundo) sufixa `-2`/`-3`/... em vez de sobrescrever silenciosamente (`uniqueBackupDir`)
- [x] `func (r *RollbackStore) List(secretName string) ([]RollbackBackupInfo, error)` — ordenado mais recente primeiro, nunca retorna nil (slice vazio)
- [x] `func (r *RollbackStore) Get(secretName, backupID string) (tlsCrt, tlsKey []byte, meta RollbackBackupInfo, err error)` — valida `secretName`/`backupID` contra path traversal (`validPathComponent`)
- [x] `func (r *RollbackStore) Prune(keepPerSecret int) error` — mantém as N mais recentes por `secretName` (padrão: 20), apaga o resto; ticker diário (`go store.cleanupLoop()`, mesmo padrão de `internal/storage/ai_history_store.go`)
- [x] **Caminho manual/batch**: `Scanner` ganha campo `rollbackStore *RollbackStore` (`NewScanner(km, rollbackStore)`, pode ser `nil`); `UploadCertificate` chama `backupBeforeOverwrite` antes do `Update` — melhor-esforço, erro só loga, nunca impede a atualização
- [x] **Caminho AWX**: endpoint `POST /api/v1/certificates/:cluster/:namespace/:name/backup` (RBAC `RequireSREGroup`) → `CertificatesHandler.Backup`. Frontend: `AWXCertForm` ganhou prop `onBeforeLaunch?: () => Promise<void>`, chamada por `CertificateRenewModal.tsx` antes de `launchAWXCertJob(...)` (best-effort — falha só gera um toast de aviso, não bloqueia o job)
- [x] `GET /api/v1/certificates/:cluster/:namespace/:name/rollback` — lista backups filtrados por cluster+namespace (evita misturar secrets homônimos de clusters diferentes), sem RBAC
- [x] `POST /api/v1/certificates/:cluster/:namespace/:name/rollback` — body `{backup_id}`, RBAC `RequireSREGroup`. `CertificatesHandler.Rollback`: valida cadeia do backup como sanidade (não bloqueia) → lê estado atual só pro "before" da auditoria → reaproveita `Scanner.UploadCertificate` pra escrever (que já faz o backup do estado atual sozinho via `backupBeforeOverwrite`, sem duplicar) → registra `HistoryTracker` (`action: "cert-rollback"`)
- [x] `NewCertificatesHandler(km, historyTracker)` — `RollbackStore` instanciado internamente; se falhar (ex: home dir), loga aviso e segue com `rollbackStore=nil` (backup/rollback fica indisponível, resto do handler funciona normal)
- [x] `internal/certificates/rollback_test.go`: 7 testes — round-trip Backup→List→Get, lista vazia sem nil, ordenação, colisão de ID, path traversal rejeitado, permissões 0700/0600, Prune mantendo só as N mais recentes. Todos passando

### Frontend

- [x] `CertificateRollbackModal.tsx` (novo): integrado direto em `CertificateDetailModal.tsx` (não em `CertificatesTab.tsx` — o modal de detalhe é compartilhado entre `CertificatesTab.tsx` e `SecretsTab.tsx`, então um único ponto de integração cobre as duas telas). Botão "Backups / Rollback" no footer, ao lado de "Validar Cadeia". Lista backups via `GET .../rollback`; "Restaurar" atrás de `AlertDialog` (padrão de `LoadSessionModal.tsx`)
- [x] Ao confirmar: `POST .../rollback`, mostra `ChainValidationResult` via `CertificateChainValidationPanel` já existente; `onRestored` callback opcional — `SecretsTab.tsx` reaproveita `handleSelectSecret`/`refreshTlsCertMap`/`silentRefetch` (mesmo trio do `CertificateRenewModal`), `CertificatesTab.tsx` reaproveita `handleScan`
- [x] `useCertificates.ts`: `backupCertificate`, `listRollbacks`, `rollbackCertificate`
- [x] `types/certificates.ts`: `RollbackBackupInfo`
- [x] `tsc --noEmit`/`eslint` sem erros novos. **Não testado visualmente em navegador** (sem ferramenta de automação de browser neste ambiente) — só validado via `curl` com JWT real contra os endpoints (request/response corretos; round-trip de escrita contra Secret real não executado, ver Verificação)

---

## Fase 3 — Enriquecimento de validação via Prometheus (propagação real no ingress-nginx) ✅ concluída

Confirmado empiricamente (VPN ativa, cluster real `akspriv-adanalytics-prd`, via `curl` direto ao Prometheus) que o ingress-nginx expõe `nginx_ingress_controller_ssl_certificate_info` (rotulada por `secret_name`/`namespace`/`serial_number`/`issuer_common_name`/`kubernetes_pod_name`) e `nginx_ingress_controller_ssl_expire_time_seconds` — permitindo detectar se um certificado recém-atualizado já se propagou para todas as réplicas do ingress-nginx, algo que a validação estática de PEM (Fase 1) nunca cobre (só olha o conteúdo do Secret, não o que está de fato sendo servido).

**Limitações intencionais**: não é um teste de handshake TLS ativo, só leitura de métrica já exportada; só cobre Secrets atrás de um Ingress via ingress-nginx (mTLS interno ou outro ingress controller → `checked:false`, não erro); depende do Prometheus do cluster estar acessível (senão, pula silenciosamente); validado empiricamente contra 1 cluster/tenant só — o código tolera ausência de métrica graciosamente por padrão.

- [x] `internal/certificates/prometheus_enrich.go` (novo): `LivePropagationResult` (`Checked`, `TotalReplicasFound`, `ReplicasCurrent`, `ReplicasStale`, `LiveIssuerCN`, `LiveExpiresAt`, `Notes`) + `EnrichWithPrometheus(cluster, namespace, secretName, leafSerialDecimal string) *LivePropagationResult` — reaproveita `internal/monitoring/client.NewPrometheusClient` (já usado por conntrack/finops/latency-test), timeout próprio de 8s, sempre melhor-esforço (`Checked:false` em vez de erro)
- [x] `LeafSerialDecimal(certPEM []byte) (string, error)` — extrai o serial do leaf em decimal (bate direto com o label `serial_number` do Prometheus; formato diferente do hex usado em `CertificateInfo.SerialNumber`)
- [x] `buildLivePropagationResult`/`latestExpireTimestamp` extraídas como lógica pura testável sem mock de HTTP
- [x] `ChainValidationResult` ganha campo `LivePropagation *LivePropagationResult` — preenchido só quando há cluster/namespace/secretName reais (`ValidateInstalledChain`, `Upload` quando destino é único, `Rollback` após a escrita de fato)
- [x] `internal/certificates/prometheus_enrich_test.go`: 9 testes — todas réplicas atualizadas, propagação incompleta (réplicas stale detectadas corretamente), sem série encontrada (`checked:false`), `certResult` nil, timestamp de expiração mais recente escolhido corretamente, valor inválido ignorado, `LeafSerialDecimal` válido/inválido. Todos passando
- [x] Frontend: `types/certificates.ts` +`LivePropagationResult`; `CertificateChainValidationPanel.tsx` ganha seção condicional (só renderiza se `live_propagation?.checked`) — "N/M réplicas do ingress-nginx com o certificado atual" + aviso âmbar listando pods com propagação pendente
- [x] `tsc --noEmit`/`eslint` sem erros novos. **Não validado contra Prometheus real via curl à parte do EnrichWithPrometheus em si** (a query manual que confirmou as métricas foi feita antes de escrever o código; o código em si só foi validado por teste unitário) — ver Verificação

---

## Fase 4 — Correções pós-implementação (achadas via uso real) ✅ concluída

Após as Fases 1-3, o uso real da feature (via feedback direto do usuário testando os modais) revelou 6 bugs reais, todos corrigidos nesta fase — nenhum deles hipotético, todos reproduzidos/explicados concretamente antes da correção:

1. **Falso-positivo na checagem de ordem da cadeia** (`internal/certificates/validate.go`) — o "Passo 2" comparava `certs[i]` com o próximo item EXATO do arquivo (`certs[i+1]`) pra decidir se a cadeia estava "fora de ordem". Bundles reais (ex: Sectigo/USERTrust, confirmado contra um cert real `casasbahia-tls`) frequentemente trazem intermediário/raiz em ordem diferente da canônica — isso não quebra o TLS de verdade (servidores/browsers fazem path-building sobre o conjunto todo, não checagem posicional), mas reprovava praticamente qualquer certificado real testado ("todos os testes com erro na cadeia"). Corrigido pra procurar o assinante do leaf em qualquer posição do PEM, não só na seguinte — mantém a detecção real de intermediário genuinamente ausente. Teste antigo que codificava o comportamento errado (`TestValidateCertificateChain_WrongOrder`) substituído por `TestValidateCertificateChain_ReorderedIntermediateRoot_NaoEhErro` (regressão) + `TestValidateCertificateChain_IntermediarioRealmenteAusente` (garante que o caso real de bug continua detectado).

2. **Footer do `CertificateDetailModal.tsx` estourava a largura do modal** — com 5 botões (Fechar/Validar Cadeia/Backups-Rollback/Copiar/Atualizar em Massa), o `DialogFooter` (shadcn) não tinha `flex-wrap`, então os botões extrapolavam a borda do modal em vez de quebrar linha. Corrigido com `className="flex-wrap gap-2 sm:space-x-0"` só nessa instância (não no componente compartilhado, pra não afetar outros modais que já cabem numa linha).

3. **"Atualização em Massa" (`CertificatesTab.tsx`) não tinha opção de AWX** — só aceitava colar PEM manualmente, diferente do modal de renovação em Secrets (`CertificateRenewModal.tsx`), que já suportava AWX desde antes. Adicionado: toggle Manual/AWX também nesse modo (antes só existia em "Instalação"); como `AWXCertForm` lança 1 job por vez (não em lote), o formulário só aparece com exatamente 1 destino marcado na lista de "Clusters encontrados no scan" — com 0 ou mais de 1, mostra aviso explicando a limitação. Dispara `backupCertificate` antes do job e refaz o scan ao concluir.

4. **Falha silenciosa no AWX — bug de stale closure** (`AWXCertForm.tsx`) — `es.onerror` (SSE) checava `jobStatus === "running"` lendo a variável `jobStatus` capturada no closure de `launchJob`, que sempre refletia o valor de ANTES do clique (`setState` é assíncrono, não muda o binding já capturado) — a condição era efetivamente sempre falsa. Resultado: uma queda de conexão SSE (proxy corporativo, rede instável, servidor reiniciando) nunca disparava toast nem tirava a UI do estado "running" — o job ficava girando "Aguardando output..." pra sempre, sem nenhum aviso. Corrigido com uma variável local (`finished`, não estado React) escopada à própria execução do job, reatribuída pelos próprios handlers no mesmo escopo — reflete o estado real. Como `AWXCertForm` é compartilhado pelas 3 telas que usam AWX (Certificados TLS, Secrets, modal standalone), a correção cobre todas de uma vez.

5. **Seleção padrão da "Atualização em Massa" sempre marcava TODOS os destinos** (`CertificatesTab.tsx`) — `setBatchSelected(new Set(found))` marcava todos os clusters/namespaces encontrados por padrão em 3 pontos (botão "Atualizar em Massa", toggle de modo, campo de nome do secret), inclusive quando o modo já era AWX — que só aceita exatamente 1 destino. Isso fazia o formulário AWX nunca aparecer, caindo sempre no aviso "selecione 1 destino" (sintoma reportado como "a opção de instalação/update usando AWX foi retirada"). Corrigido: extraído helper `defaultBatchSelection(found)` que marca todos em modo manual (comportamento histórico) e só o primeiro em modo AWX; toggle Manual→AWX também colapsa uma seleção múltipla pra 1 se necessário.

6. **Modal de rollback "voltava" pra lista de backups em vez de avançar** (`CertificateRollbackModal.tsx`) — mesmo padrão de bug #4 (stale closure): `AlertDialogAction` (Radix) fecha a confirmação sozinho ao clicar, e o guard de `onOpenChange` (`!o && !restoring && setPendingRestore(null)`) lia `restoring` (estado React) de um closure desatualizado — sempre `false` no momento do clique — então a confirmação sempre fechava na hora. Sem nenhum estado visual de "restaurando" no modal externo, o usuário via a lista de backups de novo, parecendo ter voltado em vez de avançar pra aplicação do certificado. Corrigido com `useRef` (mesma técnica do bug #4) pro guard + novo estado visual "Restaurando certificado..." exibido imediatamente após confirmar, independente do timing de fechamento da confirmação.

**Padrão recorrente identificado** (bugs #4 e #6): checar uma variável de estado React (`useState`) dentro de um callback assíncrono que roda fora do fluxo normal de render (event handler de terceiro — `EventSource.onerror`, `Radix AlertDialog.onOpenChange`) é uma fonte real e recorrente de bugs nesta base de código — o valor lido reflete o render anterior, nunca o que acabou de ser setado na mesma call stack. Sempre que precisar checar "o estado mais recente" dentro desse tipo de callback, usar `useRef`/variável local escopada, nunca o valor de `useState` direto.

Nenhum teste automatizado novo nesta fase (bugs de UI/interação, cobertos por `tsc`/`eslint` + análise de código; teste automatizado de comportamento de closures assíncronos em React não está no escopo de ferramentas deste projeto).

---

## Arquivos afetados (acumulado das 4 fases)

| Arquivo | Mudança |
|---|---|
| `internal/certificates/validate.go` | **novo** — `ValidateCertificateChain`, enriquecimento OpenSSL opcional; ganhou campo `LivePropagation` (Fase 3); checagem de ordem da cadeia corrigida pra ser order-independent (Fase 4) |
| `internal/certificates/rollback.go` | **novo** — `RollbackStore` (Backup/List/Get/Prune) |
| `internal/certificates/prometheus_enrich.go` | **novo** — `EnrichWithPrometheus`, `LivePropagationResult`, `LeafSerialDecimal` |
| `internal/certificates/validate_test.go` | teste de ordem errada substituído por 2 testes (reordenação não é erro; intermediário ausente ainda é) — Fase 4 |
| `internal/certificates/rollback_test.go` | **novo** |
| `internal/certificates/prometheus_enrich_test.go` | **novo** |
| `internal/certificates/scanner.go` | `Scanner` ganha `rollbackStore`; `UploadCertificate` faz backup antes de sobrescrever (`backupBeforeOverwrite`) |
| `internal/web/handlers/certificates.go` | +`Backup`, `ListRollbacks`, `Rollback`, `ValidateChainPEM`, `ValidateInstalledChain`, `enrichLivePropagation`; `NewCertificatesHandler` ganha `historyTracker`+`rollbackStore` interno |
| `internal/web/server.go` | rotas novas de rollback/backup + atualiza call site de `NewCertificatesHandler` |
| `internal/web/frontend/src/hooks/useCertificates.ts` | +`backupCertificate`, `listRollbacks`, `rollbackCertificate` |
| `internal/web/frontend/src/types/certificates.ts` | +`RollbackBackupInfo`, +`LivePropagationResult` |
| `internal/web/frontend/src/components/CertificateChainValidationPanel.tsx` | **novo** (Fase 1); seção de propagação Prometheus (Fase 3) |
| `internal/web/frontend/src/components/CertificateRollbackModal.tsx` | **novo** (Fase 2); `useRef` pro bug de fechamento prematuro + estado visual "Restaurando..." (Fase 4, bug #6) |
| `internal/web/frontend/src/components/CertificateRenewModal.tsx` | validação automática pós-instalação; `onBeforeLaunch` pro AWX |
| `internal/web/frontend/src/components/CertificateDetailModal.tsx` | botão "Validar Cadeia"; botão "Backups / Rollback" + `onRestored`; footer `flex-wrap` (Fase 4, bug #2) |
| `internal/web/frontend/src/components/CertificatesTab.tsx` | `onRestored={handleScan}`; toggle Manual/AWX + seleção padrão no modo "Atualização em Massa" (Fase 4, bugs #3 e #5) |
| `internal/web/frontend/src/components/SecretsTab.tsx` | `onRestored` no `CertificateDetailModal` (mesmo trio de `CertificateRenewModal.onSuccess`) |
| `internal/web/frontend/src/components/AWXCertForm.tsx` | prop `onBeforeLaunch` chamada antes de `launchAWXCertJob`; `es.onerror` corrigido com variável local em vez de estado React (Fase 4, bug #4) |

## Verificação

- [x] `go build ./...`, `go vet ./internal/certificates/... ./internal/web/handlers/... ./internal/web/...`, `go test ./internal/... -race` (todos os testes novos + suíte existente completa, sem regressão)
- [x] `tsc --noEmit`, `eslint .` no frontend — sem erros novos, em todas as 4 fases
- [x] Servidor sobe sem panic de rota conflitante; smoke-test via `curl` com JWT real: `ListRollbacks` de secret inexistente retorna `{"data":[],"success":true}`; `Rollback` sem `backup_id` retorna 400; `Backup` contra cluster inexistente retorna o erro de client K8s esperado (não um erro de código)
- [x] Fase 4, bug #1 (ordem da cadeia): reproduzido e corrigido contra um cenário real (bundle Sectigo/USERTrust reordenado, mesmo padrão do `casasbahia-tls` reportado pelo usuário) — coberto por teste unitário de regressão
- [ ] Teste manual ponta a ponta de ESCRITA (upload → backup → segundo upload → rollback) **não executado ainda** — exigiria escrever um Secret de teste num cluster real; evitado por ser uma ação de escrita não solicitada explicitamente. Roteiro sugerido: gerar um cert self-signed local (`openssl req -x509 -newkey rsa:2048 ...`) → upload → conferir pasta `~/.k8s-hpa-manager/rollback-certs/<nome>/<data>/` criada com o cert ANTERIOR → upload de um segundo cert (chave trocada de propósito) → `validate-chain` deve reportar `key_matches_cert:false` → `rollback` pro backup do primeiro → `validate-chain` deve voltar a `valid:true`
- [ ] Fase 3 contra cluster real: chamar `GET .../validate-chain` de um Secret real com Ingress (ex: `viavarejo-tls`/`adanalytics-prd`/`akspriv-adanalytics-prd`) e conferir que `live_propagation.checked=true` — não executado ainda (mesmo motivo acima, e por ora não é estritamente uma escrita, mas envolve consultar Prometheus de produção)
- [ ] **Validar em navegador os 6 bugs da Fase 4** — nenhum deles foi confirmado visualmente ainda (sem ferramenta de automação de browser disponível neste ambiente); a análise/correção de cada um foi feita por leitura de código + raciocínio sobre o comportamento do React/Radix, não por reprodução ao vivo. Recomendado antes de dar a feature por encerrada: (a) testar upload/renovação via AWX em Secrets e em Certificados TLS, incluindo o cenário de conexão caindo no meio do job; (b) testar "Atualização em Massa" com AWX alternando entre 0/1/vários destinos marcados; (c) testar o fluxo completo de restaurar um backup no modal de rollback, incluindo o estado visual "Restaurando..."; (d) conferir que os 5 botões do footer de `CertificateDetailModal` quebram linha em vez de estourar; (e) validar a cadeia de um certificado real com bundle "fora de ordem" e confirmar `valid:true`
