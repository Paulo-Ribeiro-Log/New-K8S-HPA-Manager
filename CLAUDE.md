# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

**New K8s HPA Manager**: ferramenta web de gerenciamento de recursos Kubernetes/Azure AKS (também EKS/GKE) em larga escala — API Go/Gin + SPA React/TypeScript. Cobre HPAs/Node Pools em lote, Health Check com IA, FinOps, Access Checker, Code Editor (Git/LSP), Rollback de Deployment, aba VMs/EC2 e integrações (Dynatrace, ServiceNow, Teams, Spinnaker, Nexus, AWX).

**A TUI (Bubble Tea) foi removida.** `new-k8s-hpa` sem subcomando sobe o servidor web (`cmd/root.go`). Ignore qualquer menção a TUI/`AppModel`/`tea.Cmd` em docs antigos.

## Regras do projeto (obrigatórias)

- Responda **sempre em português brasileiro**; mensagens de commit também em pt-br.
- Filosofia **KISS**.
- Binários sempre em `./build/` (`./build/new-k8s-hpa`).
- `make build` **não** reinicia o servidor: `kill <PID> && ./build/new-k8s-hpa web -f` (ou `./rebuild-web.sh -b`).
- Mudou frontend → `./rebuild-web.sh -b` **e** hard refresh no browser (Ctrl+Shift+R). Os assets buildados são commitados em `internal/web/static/` (commits `chore(web): rebuild dos assets`).
- Se o agente reinicia o servidor, o processo herda a rede **do agente**, não a da máquina do usuário (sem VPN). Para testes contra clusters reais, peça ao usuário para reiniciar no terminal dele.

## Comandos

```bash
make build                              # backend Go (BUILD_PARALLEL=2 por padrão, WSL2; BUILD_PARALLEL=4 se sobrar RAM)
./rebuild-web.sh -b                     # frontend + backend + restart em background (-n: só restart, -k: mata :8080, -s: status)
./build/new-k8s-hpa web -f              # servidor em foreground (porta 8080); `web --ad` = bypass RBAC de emergência
./build/new-k8s-hpa autodiscover        # descobre clusters AKS+EKS+GKE em paralelo
make web-dev                            # Vite HMR (5173) — rode o backend em paralelo

go test -v ./internal/... -race                       # tudo, com race detector
go test -run TestNome -v ./internal/web/handlers/...  # teste único
SKIP_AZURE_TESTS=1 go test ./...                      # sem az CLI autenticado (usado na CI)
./testes/test-rbac.sh                                 # suíte RBAC

cd internal/web/frontend
npx tsc --noEmit -p tsconfig.app.json   # type-check REAL (ver abaixo)
npm run lint                            # eslint (no-undef desligado)
```

Antes de commitar: `go test ... -race`, `make build`, `go fmt ./...`, `go mod vendor` (⚠️ ver patch do go-rod), e rebuild do frontend se mexeu nele.

**Armadilhas de build/tooling**
- O arquivo é `makefile` (minúsculo).
- `vite build` **não** type-checka. `npx tsc --noEmit -p .` também não (tsconfig raiz é "solution", retorna sucesso sempre) — use `-p tsconfig.app.json`. O repo tem dezenas de erros de tipo/lint pré-existentes (`Index.tsx`, `client.ts`, …); compare com o baseline via `git stash` em vez de tentar zerar.
- `vendor/github.com/go-rod/rod/lib/cdp/client.go` tem **patch manual** (`consumeMessages` loga+`continue` em vez de `panic` em frame CDP malformado; comentário `PATCH MANUAL`). `go mod vendor` apaga o patch em silêncio — reaplique.
- `go.mod` tem `godebug tlsmlkem=0` (o key share pós-quântico do Go 1.24+ trava handshake TLS em VPN/EKS privado). Não remover.
- `make release`/`build-all` num host Linux geram binários **darwin com SQLite quebrado** (cgo desligado → stub do `go-sqlite3` compila mas falha em runtime). Release de verdade = workflow `release.yml` (runners macOS nativos). `make release-single` é o alvo usado pela CI.
- Não existe test runner de frontend. `make run-test` está quebrado (`cmd/k8s-teste` não existe).

## Arquitetura (visão geral)

**Backend** (`internal/`, módulo Go `k8s-hpa-manager`, imports `k8s-hpa-manager/internal/...`; deps vendorizadas)
- `web/server.go` registra **todas** as rotas e faz a injeção de dependências; `web/handlers/` = um arquivo por recurso, recebem `*ClientCache`/stores via construtor (nunca criam client K8s próprio). `web/middleware/` = JWT, RBAC, WebSocket auth. `web/sse/` = broker de progresso.
- `models/types.go` é a fonte única dos tipos de domínio compartilhados.
- `config/kubeconfig.go` (`KubeConfigManager`) é o núcleo: cache de clients (RWMutex, TTL 30min), **cópia privada do kubeconfig** em `~/.k8s-hpa-manager/kubeconfig` + `KUBECONFIG` setado no processo (isola de kubectl/k9s/az/aws externos — mudanças no kubeconfig original só aparecem após reiniciar), pré-checagem TCP de alcance do cluster, tokens EKS/GKE cacheados com auto-renovação (`eksTokenRoundTripper`/`gkeTokenRoundTripper`).
- `cloudprovider/`: `NodeGroupProvider` (Azure `az`, AWS `aws eks`, GCP `gcloud`) escolhido por `GetNodeGroupProvider()` pelo formato do nome do context (`arn:aws:eks:` → EKS; `gke_` → GKE; senão AKS). `VMProvider` (EC2 hoje) para VMs fora de K8s. Configs de cluster separadas por provider em `~/.k8s-hpa-manager/{clusters,eks-clusters,gke-clusters}-config.json`. Para classificar provider use `config.DetectCloudProvider`, nunca o prefixo da string.
- `kubernetes/` (wrapper client-go; **sem dynamic client** — CRDs via `kubectl … -o yaml`), `collectors/`, `healthcheck/`, `finops/`, `monitoring/`, `ai/` (Ollama/Claude/Gemini/OpenAI/Copilot; `sanitizer/` mascara dados antes de ir à IA), `certificates/`, `dynatrace/`, `spinnaker/`, `servicenow/` + `teams/` + `browser/` (automação go-rod), `podsftp/` (SFTP in-process sobre `kubectl exec`), `portforward/`, `vmssh/`, `storage/` (dezenas de SQLite em `~/.k8s-hpa-manager/*.db`, WAL).
- Toda operação destrutiva registra no `HistoryTracker` (`helpers.CreateHistoryEntry`).
- Chamadas a CLI cloud (`az`/`aws`/`gcloud`) **sempre** com `exec.CommandContext` + timeout e, se no caminho quente de request, cache com TTL.

**Frontend** (`internal/web/frontend`, React 18 + Vite + shadcn/ui + Tailwind + Monaco + xterm + Cytoscape)
- Uma só rota útil (`/` → `pages/Index.tsx`); a navegação é o estado `activeTab` (sem rotas por aba). Abas pesadas ficam montadas com `display:none` (ref `hasBeenMounted`); as demais são desmontadas via `renderTabContent()`. Menus: `WorkloadMenu`, `ToolsMenu` (nova ferramenta = novo item ali). Vários arquivos em `pages/` (`Index.backup/broken`, `SimpleIndex`, …) e `NodePoolTab.tsx` são **código morto** — edite `Index.tsx`/`TabContent.tsx`.
- Estado global: `StagingContext` (mudanças pendentes de HPA/NodePool), `TabContext` (multi-cluster). Dados: React Query (`queryKey` único; invalide em vez de `location.reload()`), e algumas listas com polling manual em `hooks/useAPI.ts`.
- **Todo** HTTP passa por `lib/api/client.ts` (auto-refresh de JWT, `encodeURIComponent` — nomes de cluster EKS são ARNs com `/` e `:`). Não use `fetch` direto em componentes.
- Modais/painéis com `flex-1 min-h-0` não podem usar shadcn `<Tabs>` (o `display:block` quebra a cadeia flex) — use abas manuais com `useState`. Não adicione classe de `position` (`relative`, etc.) via `className` em componente shadcn: `tailwind-merge` troca o `fixed` da base em silêncio. Não ponha arrays derivados de `.map()` em deps de `useEffect`.

**Tempo real e auth**
- Rotas SSE e WebSocket (browser não manda `Authorization`) ficam em grupos com `WebSocketJWTAuthMiddleware` (token em `?token=`). Uma rota SSE atrás do middleware JWT padrão dá 401 silencioso.
- Auth dual: JWT quando há `K8S_HPA_JWT_SECRET` ou `~/.k8s-hpa-manager/jwt.secret` (claims `email`, `name`, `is_sre`), senão token estático `K8S_HPA_WEB_TOKEN`.
- Duas camadas de RBAC: grupo Azure AD (`RequireSREGroup()`, hoje **no-op** — `OptionalSRECheck` sempre `isSRE=true`; não remova sem alinhar) e RBAC real do cluster via `SelfSubjectRulesReview` (`useK8sPermissions` + `<ProtectedAction allowed={…}>`).
- Servidor se desliga sozinho após ~40min sem heartbeat do frontend.

**Padrões recorrentes**
- Ferramentas de teste ativo (Latência, Kafka, Banco de Dados, Descoberta de Rede) têm modo `pod` (Ephemeral Container/pod efêmero no cluster) e modo `local` (`docker run` no host, com pré-checagem + reaper de containers órfãos em `db_test_docker.go`). Ao cancelar, limpe o container pelo `--name`.
- Watch (informer do client-go via SSE) só em Pods/Deployments/HPAs; o resto é polling. `usePodsWatch`/`pods_watch.go` são o molde.
- Rollback de Deployment: 6 modos (K8s nativo, Helm, Nexus, Imagem, Spinnaker, Arquivos), todos com bypass automático da label Kyverno `devops.k8s.io/kyverno-bypass` (ref-counted por namespace, removida no fim).
- Sessão Teams/ServiceNow usam perfis de browser **separados** (`teams-session/` vs `rod-session/`); nunca misture.

**Aba VMs / EC2** (branch `feat/vms-ec2-tab`, ainda **não** mesclada na `main`; ver `git log --grep 'feat(vms'`)
- `cloudprovider.VMProvider` + `cloudprovider/aws/ec2.go` (inventário e start/stop/reboot). Rotas `/api/v1/vms/*` em `server.go`: leitura sem RBAC de grupo, escrita/terminal/túnel atrás de `RequireSREGroup()`.
- Dois transportes para acessar a VM: **SSH direto** (`internal/vmssh`: client `golang.org/x/crypto/ssh`, `known_hosts` compartilhado com confirmação TOFU, terminal WebSocket, SFTP) e **SSM** (Session Manager: terminal, túnel `aws ssm start-session` para SFTP, e modo "SSM sem SSH" via `cloudprovider/aws/ssm_command.go` + `vm_sftp_ssm.go`).
- Credenciais SSH por usuário em `storage.VMCredentialStore` (perfis, gerar/importar chave local). Certificados em VM (`certificates_vm.go`, `certificates/vm_cert_pair.go`): ler/validar par cert+key, transferir, reiniciar serviço.
- Frontend: `VMsTab`, `VMConnectModal`, `VMCredentialsModal`, `VMSFTPModal`, `VMCertificatePanel`.

## Onde está o detalhe

`docs/history/CLAUDE-DETAILED-NOTES.md` (~650 KB) é o diário por feature (decisões, achados reais, bugs corrigidos). **Nunca leia inteiro** — `grep -n '^##' docs/history/CLAUDE-DETAILED-NOTES.md` e leia só a seção da feature. Outros: `docs/guides/` (TROUBLESHOOTING, COMMON_PITFALLS, DEVELOPMENT_COMMANDS, WEB_INTERFACE, RBAC_AZURE_AD_IMPLEMENTATION), `docs/history/CHANGELOG.md`, `docs/architecture/OVERVIEW.md`, e os `*-PLAN.md` da raiz linkados com status nas notas detalhadas (os demais `*-PLAN.md`/`Docs/` são histórico não mantido).

Ao terminar uma feature ou corrigir um bug, **não** acrescente narrativa a este arquivo: registre em `docs/history/CHANGELOG.md` (ou na seção da feature nas notas detalhadas) e aqui só o que muda como se trabalha no código.
