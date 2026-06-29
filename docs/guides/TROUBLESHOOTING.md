# Troubleshooting

[Voltar ao CLAUDE.md principal](../../CLAUDE.md)

## Geral / Build / Dev

| Problema | Solução |
|----------|---------|
| Frontend não atualiza após mudança | `./rebuild-web.sh -b` + Ctrl+Shift+R no browser |
| Mudanças no backend não tomam efeito | Servidor não reiniciado — `make build` só gera o binário; matar e reiniciar (`kill <PID> && ./build/new-k8s-hpa web -f`) |
| Servidor desliga sozinho após ~40min | Auto-shutdown por inatividade — reabrir a aba reinicia o heartbeat (browsers throttleiam `setInterval` em abas de fundo) |
| Build falha sem versão | `git fetch --tags --prune` |
| Race condition em testes | Verificar mutex em `internal/config/kubeconfig.go` |
| AI Diagnostics timeout | Usar modelo llama3.2:3b (max viável com 6GB RAM) |
| Cluster inacessível | VPN ou cluster desligado — `kubectl cluster-info --context <name>` |
| Editor YAML "apiVersion not set" | Adicionar TypeMeta antes do `yaml.Marshal` (API typed do K8s não preenche TypeMeta) |

## Auth / JWT

| Problema | Solução |
|----------|---------|
| JWT: login retorna 501 | `K8S_HPA_JWT_SECRET` não definido no servidor — frontend cai para token estático automaticamente |
| JWT: login retorna "AZ_CLI_ERROR" | Azure CLI não autenticado no servidor — executar `az login` no servidor |
| JWT: token expira antes do esperado | Verificar `K8S_HPA_JWT_TTL` (padrão `8h`); refresh automático falhou — verificar logs do servidor |
| JWT: frontend em loop de login | `isTokenExpired()` retornando true mas refresh retornando 401 — limpar `localStorage` manualmente |
| K8s RBAC: botão disabled mesmo sendo SRE | `useK8sPermissions` ainda carregando ou cluster sem permissão real — verificar se `allowed` prop está sendo passada; RBAC do cluster prevalece sobre grupo AD |
| K8s RBAC: `Incomplete: true` na resposta | Cluster usa wildcard policies — handler assume acesso total, botões ficam habilitados |

## Frontend / Monaco / Code Editor

| Problema | Solução |
|----------|---------|
| Monaco: Ctrl+Shift+D/E sumiu do contexto | `configureMonacoYaml` foi chamado múltiplas vezes — verificar flag `_yamlConfigured` em `MonacoYamlEditor.tsx`. Nunca remover esse guard |
| Terminal duplica "ç" | Verificar `event.preventDefault()` antes de `ws.send()` em `PodTerminal.tsx` |
| Tab de modal não preenche a altura | shadcn `<Tabs>` usa `display:block` que quebra `flex-1 min-h-0`. Substituir por implementação manual com `div` + estado local (ver `PodQuickViewModal.tsx`) |
| Command Runner sem resposta | Verificar se SSE broker está iniciado e session ID é único |
| Dependency graph não carrega | Cytoscape requer container com dimensões definidas (não `height: 0`) |
| Colunas CPU/MEM no monitor se movem juntas | Data cells com `text-right` criam ilusão de movimento ao arrastar — manter alinhamento à esquerda nas células de dados |
| SNAT widget não aparece na aba Node Pools | Widget fica em `Index.tsx` case `"nodepools"` — `NodePoolTab.tsx` é componente **órfão** (nunca importado) |
| SNAT widget não carrega | `az aks show` retorna erro ou timeout (60s). Verificar VPN e autenticação Azure CLI (`az account show`) |
| SNAT widget mostra "0 IPs" | Cluster sem LB profile — `allocatedOutboundPorts` e `outboundIpCount` = 0. Checar `az aks show --query networkProfile.loadBalancerProfile` |
| HPAEditor: botões disabled mesmo sendo SRE | `ProtectedAction` usa `allowed={permissions.canUpdateHPA}` — RBAC K8s prevalece. Kubeconfig do servidor não tem permissão `update` naquele namespace |
| HPAEditor: checkbox staging não adiciona ao painel | Verificar se `StagingContext` está montado (wrapping do `HPATab`); `staging.addHPA()` só funciona dentro do provider |

### Code Editor (detalhes)

| Problema | Solução |
|----------|---------|
| Context menu não fecha | `document.addEventListener("mousedown")` depende de `onMouseDown={e.stopPropagation()}` dentro do menu |
| "Revelar na tree" não destaca o arquivo | Verificar se `setSidePanel("files")` foi chamado antes de `setRevealPath`; dir pai precisa estar expandido |
| Font size não muda no Monaco | `updateOptions` depende de `editorRef.current` montado; verificar se `handleEditorMount` está sendo chamado |
| Ctrl+P não abre paleta | Verificar se `relative` está no container pai (`absolute inset-0` do overlay depende disso) |
| Auto-save salva conteúdo desatualizado | `useEffect(() => { saveFileRef.current = saveFile; })` sem deps garante atualização — nunca remover esse efeito |
| Format on save não formata | Verificar se linguagem está em `["go","typescript","javascript","python","json"]`; erros de format são silenciosos |
| "Repositório já clonado localmente" | 409 do backend — clicar "Abrir repositório existente" |
| Clone falha com "não autorizado" | GitHub PAT não configurado. Ir em GitHub Releases → token, scope `repo` + SSO autorizado |
| Push falha com 403 | PAT sem permissão de escrita ou SSO não autorizado. Classic PAT: "Configure SSO" no GitHub |
| Arquivo não abre (5MB limit) | Limite de 5MB para edição. Arquivos binários também falham |
| Árvore não mostra `vendor/` | Ignorado intencionalmente (junto com `node_modules`, `.git`, `build`, `dist`). Editar `ignoredDirs` em `code_editor.go` se necessário |
| Branch remoto não aparece | `GET /branches` faz `git fetch --prune`. VPN ou rede indisponível pode bloquear |
| Ctrl+S não salva | `editor.addCommand` usa keycode `(2048\|49)` = Ctrl+S. Salvo via `codeEditorWriteFile` — verificar console do browser |
| Painel Source Control vazio | Verificar se `git status --porcelain` retorna output; sem arquivos modificados o painel fica vazio |
| Push rejeitado com "non-fast-forward" | Pull --rebase automático implementado — se rebase falhar (conflito), push retorna erro com mensagem de conflito |

### LSP

| Problema | Solução |
|----------|---------|
| Go: completions não aparecem | Verificar se `gopls` está instalado (`~/go/bin/gopls` ou PATH). Status: `GET /repos/:id/lsp/status?lang=go`. Primeira completion demora 3-5s (indexando workspace) |
| Go: "language server 'go' não encontrado" | Instalar: `go install golang.org/x/tools/gopls@latest` |
| Go: completions duplicadas | `__monacoGoLSPRegistered` evita registro duplo — verificar se a flag foi removida de `handleEditorMount` |
| Diagnósticos não aparecem | Polling roda apenas quando aba Go/Python está ativa. Verificar `useEffect([activeTabIdx])` no CodeEditorTab |
| TS/JS: erros "cannot find module" | Esperado — Monaco TS worker não tem acesso ao `node_modules`. Suprimir com `noSemanticValidation: true` |

### JSON Inspector

| Problema | Solução |
|----------|---------|
| Botão flutuante não aparece | Verificar se `onMouseUp={jsonInspector.handleMouseUp}` está no container correto; em `<textarea>` o inspetor usa botão no toolbar |
| Badge âmbar "JSON extraído" | Normal para logs com prefixo (`TIMESTAMP LEVEL {...}`) — só o bloco JSON foi parseado |
| Linha de erro não destaca | V8 antigo pode não incluir `(line N column M)` na mensagem — `errorLine` é calculado a partir do offset |
| Painel direito em branco | `ValidJsonPanel` deve chamar `tokenizeJson(line)` linha a linha — **nunca** tokenizar o JSON completo numa chamada só |
| Logs FluentD/EventHub não estruturados | Formato `TIMESTAMP LEVEL {...}` não é JSON puro — usar `{"time":"...","level":"...","msg":"..."}` com todos os campos dentro do objeto |

## Dynatrace / AI / FinOps

| Problema | Solução |
|----------|---------|
| Dynatrace 401/403 | Token inválido ou sem permissão `Read problems` |
| Dynatrace URL errada | Usar `*.live.dynatrace.com`, não `*.apps.dynatrace.com` |
| Node Pool Registry vazio | Clicar "Escanear Clusters" no tab Dynatrace (requer VPN + clusters acessíveis) |
| Health Check Dynatrace retorna vazio | Correlação K8s↔DT requer `check_dynatrace: true` no request + token configurado em AI Settings |
| Aba K8s↔DT vazia após HC | Normal se não há workloads problemáticos — só aparece com sintomas K8s ou problems DT ativos |
| Investigação profunda analisa cluster hlg para problem de prd | `extractEnvHint` retornou non-prd indevidamente — verificar se management zone tem token "hlg"/"sit" como substring de outra palavra (ex: "transition") |
| Investigação profunda não identifica namespace | Keywords com acentos não batiam em nomes K8s (ASCII). `extractKeywords` normaliza automaticamente |
| Aba GitHub (DynatraceTab) vazia | Sem k8sWorkloads nem affectedEntities com info K8s no problem DT. Executar scan na aba GitHub Releases |
| Export PDF com "%P%P%P" no texto | Caracteres Unicode fora do WinAnsi (═══, →, —). `sanitizePDF()` já converte — verificar se está sendo chamada em todos os `doc.text()` |
| Vertex AI sem permissão | Verificar ADC: `gcloud auth application-default print-access-token`. App usa `~/.k8s-hpa-manager/google_adc.json` |
| Gemini não autentica no WSL2 | OAuth app-callback usa porta 8080 (forwarded no WSL2). Se popup bloqueado: clicar no link "Clique aqui para abrir o login" |
| WIF SSO: campos não populam ao carregar | Verificar se `ai_email` está no `localStorage` e corresponde ao email salvo no backend |
| WIF SSO: autenticar sem preencher Pool/Provider | Preencher formato `poolID/providerID` ANTES de clicar "Autenticar com Google" |
| WIF SSO: "Testar Conexão" falha após OAuth | Projeto GCP não configurado — preencher Project ID antes de testar |
| Vertex AI: modelos diferentes do esperado | Vertex AI usa modelos Agentspace (`gemini-3.5-flash`, `gemini-3.1-pro-001`, `gemini-2.5-pro-preview-05-06`) — não aceita IDs do AI Studio |

## GitHub Releases / ServiceNow / Teams

| Problema | Solução |
|----------|---------|
| GitHub Releases: erro "token SAML" | PAT não tem SSO autorizado. Classic PAT: GitHub → Settings → PAT → "Configure SSO". Fine-grained: criar com org selecionada |
| GitHub Releases: org errada | Editar org no modal de credenciais GitHub (ícone de perfil). Padrão: `casas-bahia` |
| Commit GitHub falha "Not Found" | PAT sem permissão de escrita ou SSO não autorizado. Scope necessário: `contents: write` |
| Job criado sem namespace correto | Backend sempre sobrescreve namespace pelo parâmetro do request. Verificar `CreateJobFromYAML` em `internal/kubernetes/client.go` |
| ServiceNow não abre no navegador (WSL2) | CDP não conectou na porta 9223. Iniciar Chrome com `--remote-debugging-port=9223`. Ver `WindowsCDPPort` em `wsl_browser.go` |
| ServiceNow extrai mas não autentica | Sessão expirada (>8h). Limpar sessão via `DELETE /api/v1/servicenow/session` e re-autenticar no Chrome Windows |
| Teams: refresh retorna 409 Conflict | Extração já em andamento — aguardar ~90s ou reiniciar o servidor |
| Teams: CHGs não aparecem (DOM vazio) | Mr.ViaBot não carregou no prazo. Verificar se a sessão `~/.k8s-hpa-manager/teams-session/` está válida — deletar a pasta para re-autenticar |
| Teams: thread ID do Mr.ViaBot mudou | Atualizar constante `mrViaBotThreadID` em ambos `discover.go` e `extractor.go` |
| Teams: Chrome não abre (WSL2 sem display) | Adicionar `--no-sandbox` e verificar se Chrome está em `/usr/bin/google-chrome*` |
| SRE Approval: aprovação retorna "já finalizada" mas é 200 | Comportamento correto — `ErrAlreadyFinalized` retorna `already_finalized: true` na resposta JSON |
| SreApprovalButton não carrega status | `getSreApprovalInfo` falhou — verificar `/api/v1/sre-approval/info?url=...`. Página `devstartcd.via.com.br` pode estar inacessível fora da rede corporativa |

## GKE / Clusters

| Problema | Solução |
|----------|---------|
| GKE: workloads não carregam (deployments/ingress vazios) | `GetFreshGKEToken()` sem credenciais. Verificar `~/.k8s-hpa-manager/gcp-adc.json` ou autenticar via AutoDiscover → GCP |
| GKE: autodiscovery falha com "gcloud not found" | Autenticar via Device Auth Grant no AutoDiscoverDialog — salva ADC em `gcp-adc.json` sem precisar do gcloud local |
| GKE: aviso de auth em AutoDiscover sem ter gcloud | Se `/api/v1/gcp/auth/status` retorna `has_gcloud: false`, o aviso não deve aparecer — corrigido |

## Conntrack

| Problema | Solução |
|----------|---------|
| Snapshot sempre vazio | Pod efêmero precisa de `hostNetwork: true` e acesso ao node. Verificar se o cluster permite pods privilegiados |
| Histórico não carrega | Prometheus indisponível — comportamento esperado (fallback gracioso). Verificar URL do Prometheus em `/api/v1/monitoring/v2/` |

## Debug Mode

```bash
# Logs do servidor web (modo background)
tail -f /tmp/k8s-hpa-manager-web-*.log

# Foreground com logs no terminal
./build/new-k8s-hpa web -f

# TUI com debug
./build/new-k8s-hpa --debug
```

## Backup e Restore

```bash
# Criar backup antes de modificações
./backup.sh "descrição do backup"

# Listar backups disponíveis
./restore.sh

# Restaurar backup específico
./restore.sh backup_20251001_122526
```
