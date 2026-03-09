# Checklist: Live Monitoring Tables — Deployments e Pods

**Objetivo**: Quando nenhum item estiver selecionado no painel esquerdo, o painel direito
das abas Deployments e Pods exibe uma tabela live estilo terminal (kubectl), com navegação
hierárquica e painel de logs inline.

**Branch**: `new-k8s-hpa-dev`
**Versão base**: v1.3.26 (commit `c0afbe3`)

---

## Contexto para novos chats

```
Projeto: K8s HPA Manager — React/TypeScript + Go (Gin)
Tarefa: Implementar live monitoring tables nas abas Deployments e Pods

Comportamento desejado:
1. DeploymentsTab (painel direito, quando nada selecionado):
   - Tabela terminal live: NAME | READY | UP-TO-DATE | AVAILABLE | AGE
   - Header: deployments(namespace)[count]
   - Cores: laranja quando readyReplicas < replicas ou available = 0
   - Auto-refresh 10s
   - Clique → navega para tabela de pods daquele deployment

2. Tabela de pods (drill-down de deployment OU painel direito do PodsPanel):
   - Colunas: NAME | PF● | READY | STATUS | RESTARTS | CPU | MEM | %CPU/R | %CPU/L | %MEM/R | %MEM/L | IP | NODE | AGE
   - Header: pods(namespace/deployment)[count]
   - Cores: verde=Running, laranja=Pending, vermelho=Failed
   - Auto-refresh 5s

3. Clique em pod → PodLogsPanel inline (não modal):
   - Container selector + tail lines
   - Auto-refresh switch ON por padrão (3s)
   - Botão copiar logs
   - Syntax highlighting

4. Botão voltar (breadcrumb): pod-logs → pod-table → deployment-table

5. Comportamento atual (YAML editor) PRESERVADO:
   - selectedDeployment !== null tem precedência total sobre rightView
   - Clicar na lista esquerda ainda abre o editor YAML normalmente

Arquivos chave:
- internal/web/handlers/pods.go — adicionar GetBatchMetrics
- internal/kubernetes/client.go — adicionar GetBatchPodMetrics (lista, não item único)
- internal/web/server.go — registrar rota GET /pods/metrics
- internal/web/frontend/src/lib/api/types.ts — BatchPodMetrics, PodMetricsSingle
- internal/web/frontend/src/lib/api/client.ts — getBatchPodMetrics()
- internal/web/frontend/src/components/DeploymentMonitorTable.tsx — NOVO
- internal/web/frontend/src/components/PodMonitorTable.tsx — NOVO
- internal/web/frontend/src/components/PodLogsPanel.tsx — NOVO
- internal/web/frontend/src/components/DeploymentsTab.tsx — integrar rightView
- internal/web/frontend/src/components/PodsPanel.tsx — integrar rightView

Referência de implementação existente:
- GetPodMetricsFromServer (client.go:4191) — versão single-pod, usar como base para batch
  Endpoint batch: /apis/metrics.k8s.io/v1beta1/namespaces/{ns}/pods (sem nome = lista todos)
- Syntax highlighting de logs já existe em PodsPanel.tsx — copiar o padrão
- formatAge já pode existir em lib/utils.ts — verificar antes de criar novo
```

---

## Fase 1 — Backend: Batch Metrics Endpoint

- [ ] **1.1** Adicionar `GetBatchPodMetrics` em `internal/kubernetes/client.go`
  - Chama `/apis/metrics.k8s.io/v1beta1/namespaces/{ns}/pods` (sem nome = PodMetricsList)
  - Retorna `map[string]PodMetricsSingle` (key = podName)
  - Graceful degradation: retorna `available: false` se metrics-server indisponível
  - Struct de retorno:
    ```go
    type PodMetricsSingle struct {
        CPUMillicores     int64   `json:"cpuMillicores"`
        MemoryBytes       int64   `json:"memoryBytes"`
        CPUPercentRequest float64 `json:"cpuPercentRequest"`  // -1 se sem request
        CPUPercentLimit   float64 `json:"cpuPercentLimit"`    // -1 se sem limit
        MemPercentRequest float64 `json:"memPercentRequest"`
        MemPercentLimit   float64 `json:"memPercentLimit"`
    }
    type BatchPodMetricsResult struct {
        Available bool                       `json:"available"`
        Pods      map[string]PodMetricsSingle `json:"pods"`
    }
    ```
  - Para calcular percentuais: faz uma chamada `clientset.CoreV1().Pods(ns).List()` para pegar
    requests/limits e faz join com as métricas pelo nome do pod

- [ ] **1.2** Adicionar handler `GetBatchMetrics` em `internal/web/handlers/pods.go`
  - Query params: `cluster`, `namespace`
  - Chama `kubeClient.GetBatchPodMetrics(ctx, namespace)`
  - Retorna JSON com `BatchPodMetricsResult`

- [ ] **1.3** Registrar rota em `internal/web/server.go`
  - `pods.GET("/metrics", podHandler.GetBatchMetrics)`
  - **CRÍTICO**: adicionar ANTES das rotas com parâmetros `/:cluster/:namespace/:name`
    para evitar conflito no Gin (rotas estáticas têm precedência)

- [ ] **1.4** Testar endpoint manualmente:
  ```bash
  make build && ./build/new-k8s-hpa web -f &
  curl "http://localhost:8080/api/v1/pods/metrics?cluster=SEU_CLUSTER&namespace=default"
  # Esperado: { "available": true, "pods": { "pod-name": { "cpuMillicores": 50, ... } } }
  # Ou se metrics-server indisponível: { "available": false, "pods": {} }
  ```

---

## Fase 2 — Tipos e API Client TypeScript

- [ ] **2.1** Adicionar tipos em `internal/web/frontend/src/lib/api/types.ts`
  (após a interface `PodSummary`, linha ~390):
  ```typescript
  export interface PodMetricsSingle {
    cpuMillicores: number;
    memoryBytes: number;
    cpuPercentRequest: number;   // -1 se não disponível
    cpuPercentLimit: number;
    memPercentRequest: number;
    memPercentLimit: number;
  }

  export interface BatchPodMetrics {
    available: boolean;
    pods: Record<string, PodMetricsSingle>;  // key: podName
  }
  ```

- [ ] **2.2** Adicionar método em `internal/web/frontend/src/lib/api/client.ts`
  (junto com outros métodos de pod):
  ```typescript
  async getBatchPodMetrics(cluster: string, namespace: string): Promise<BatchPodMetrics> {
    try {
      const params = new URLSearchParams({ cluster, namespace });
      return await this.request<BatchPodMetrics>(`/pods/metrics?${params}`);
    } catch {
      return { available: false, pods: {} };
    }
  }
  ```

---

## Fase 3 — Utilitários

- [ ] **3.1** Verificar se `formatAge` já existe em `src/lib/utils.ts`
  - Se não existir, criar funções em `src/lib/monitorUtils.ts`:
    ```typescript
    // formatAge: converte ISO string → formato compacto kubectl
    // "2y266d", "3d5h", "45m30s", "97s"
    export function formatAge(isoString: string): string

    // formatBytes: bytes → "50Mi", "1.2Gi", "512Ki"
    export function formatBytes(bytes: number): string

    // formatMillicores: millicores → "250m", "1.5", "n/a"
    export function formatMillicores(m: number): string

    // formatPercent: número → "45%" ou "n/a" se < 0
    export function formatPercent(v: number): string

    // rowColorClass: retorna classe Tailwind baseada no estado do pod
    export function podRowColor(phase: string, reason?: string): string
    // "Running" → "text-green-400"
    // "Pending" → "text-orange-400"
    // "Failed" | "CrashLoopBackOff" | "Error" → "text-red-400"
    // default → "text-gray-300"
    ```

---

## Fase 4 — Componente `PodLogsPanel`

**Arquivo**: `src/components/PodLogsPanel.tsx` (NOVO)

- [ ] **4.1** Criar componente com a interface:
  ```typescript
  interface PodLogsPanelProps {
    cluster: string;
    pod: PodSummary;
    onBack: () => void;
    backLabel: string;  // ex: "← pods(namespace/deployment)"
  }
  ```

- [ ] **4.2** Implementar estados internos:
  ```typescript
  const [selectedContainer, setSelectedContainer] = useState(pod.containers[0]?.name ?? "")
  const [tailLines, setTailLines] = useState(500)
  const [autoRefresh, setAutoRefresh] = useState(true)  // ON por padrão
  const [logs, setLogs] = useState("")
  const [loading, setLoading] = useState(false)
  ```

- [ ] **4.3** Fetch de logs com auto-refresh:
  - Busca via `apiClient.getPodLogs(cluster, pod.namespace, pod.name, selectedContainer, tailLines)`
  - `useEffect` com `setInterval(fetchLogs, 3000)` quando `autoRefresh === true`
  - Auto-scroll para o final do log quando `autoRefresh === true`

- [ ] **4.4** Botão copiar:
  - `navigator.clipboard.writeText(logs)`
  - Toast de confirmação (sonner)

- [ ] **4.5** Syntax highlighting (copiar padrão do PodsPanel.tsx existente):
  - ERROR/FATAL → `text-red-400 bg-red-950/20`
  - WARN/WARNING → `text-yellow-400`
  - INFO → `text-blue-400`
  - DEBUG/TRACE → `text-purple-400`
  - HTTP 4xx/5xx → `text-orange-400`
  - default → `text-green-300`

- [ ] **4.6** Layout:
  ```
  [← backLabel]                                    [Container: select] [Tail: select]
  ──────────────────────────────────────────────────────────────────────────────────
  [Auto-refresh toggle: ON]                                              [Copiar logs]
  ──────────────────────────────────────────────────────────────────────────────────
  <ScrollArea bg-black font-mono text-xs>
    ... linhas de log com highlighting ...
  </ScrollArea>
  ```

---

## Fase 5 — Componente `PodMonitorTable`

**Arquivo**: `src/components/PodMonitorTable.tsx` (NOVO)

- [ ] **5.1** Criar componente com a interface:
  ```typescript
  interface PodMonitorTableProps {
    cluster: string;
    pods: PodSummary[];
    loading: boolean;
    metrics: BatchPodMetrics | null;
    metricsLoading: boolean;
    onSelectPod: (pod: PodSummary) => void;
    headerLabel: string;  // ex: "pods(namespace/deployment)[44]"
    onRequestRefresh: () => void;
  }
  ```

- [ ] **5.2** Auto-refresh interno (5s):
  ```typescript
  useEffect(() => {
    const id = setInterval(onRequestRefresh, 5000)
    return () => clearInterval(id)
  }, [onRequestRefresh])
  ```

- [ ] **5.3** Colunas (CSS grid com `overflow-x-auto`):
  ```
  NAME(flex-1) | PF(32px) | READY(60px) | STATUS(100px) | RESTARTS(70px) |
  CPU(60px) | MEM(70px) | %C/R(55px) | %C/L(55px) | %M/R(55px) | %M/L(55px) |
  IP(110px) | NODE(flex-1) | AGE(65px)
  ```

- [ ] **5.4** Colorização por linha:
  - Usar `podRowColor(pod.phase, pod.statusReason)` do monitorUtils
  - Dot da coluna PF: `w-2 h-2 rounded-full` com cor correspondente à fase

- [ ] **5.5** Estilo terminal:
  ```tsx
  <div className="bg-black rounded-md font-mono text-xs">
    {/* Header */}
    <div className="text-cyan-400 text-center py-1 border-b border-gray-800">
      ── {headerLabel} ──
    </div>
    {/* Cabeçalho de colunas */}
    <div className="grid ... text-gray-500 uppercase px-2 py-1 border-b border-gray-800">
      <span>NAME</span><span>PF</span>...
    </div>
    {/* Linhas */}
    {pods.map(pod => (
      <button
        key={pod.name}
        className={`grid ... w-full px-2 py-0.5 hover:bg-gray-900 ${rowColor}`}
        onClick={() => onSelectPod(pod)}
      >
        ...
      </button>
    ))}
  </div>
  ```

---

## Fase 6 — Componente `DeploymentMonitorTable`

**Arquivo**: `src/components/DeploymentMonitorTable.tsx` (NOVO)

- [ ] **6.1** Criar componente com a interface:
  ```typescript
  interface DeploymentMonitorTableProps {
    deployments: DeploymentSummary[];
    loading: boolean;
    headerLabel: string;  // ex: "deployments(namespace)[6]"
    onSelectDeployment: (dep: DeploymentSummary) => void;  // drill → pods
    onOpenEditor: (dep: DeploymentSummary) => void;        // abre YAML editor (lista esquerda)
    onRequestRefresh: () => void;
  }
  ```

- [ ] **6.2** Auto-refresh interno (10s):
  ```typescript
  useEffect(() => {
    const id = setInterval(onRequestRefresh, 10000)
    return () => clearInterval(id)
  }, [onRequestRefresh])
  ```

- [ ] **6.3** Colunas:
  ```
  NAME(flex-1) | READY(80px) | UP-TO-DATE(90px) | AVAILABLE(90px) | AGE(70px) | [edit icon](32px)
  ```

- [ ] **6.4** Colorização:
  - Saudável (`readyReplicas === replicas && availableReplicas > 0`): `text-green-400`
  - Problema: `text-orange-400`
  - READY mostra `{readyReplicas}/{replicas}` — laranja se parcial

- [ ] **6.5** Botão de edição (ícone `Pencil` pequeno, última coluna):
  - `onClick={(e) => { e.stopPropagation(); onOpenEditor(dep); }}`
  - Evita que o click propague para `onSelectDeployment`

---

## Fase 7 — Integrar em `PodsPanel`

**Arquivo**: `src/components/PodsPanel.tsx`

- [ ] **7.1** Adicionar estados de navegação:
  ```typescript
  type PodRightView =
    | { kind: "pod-table" }
    | { kind: "pod-logs"; pod: PodSummary }

  const [rightView, setRightView] = useState<PodRightView>({ kind: "pod-table" })
  const [batchMetrics, setBatchMetrics] = useState<BatchPodMetrics | null>(null)
  const [metricsLoading, setMetricsLoading] = useState(false)
  ```

- [ ] **7.2** Fetch de batch metrics quando namespace muda:
  ```typescript
  useEffect(() => {
    if (!cluster || !selectedNamespace) return
    setMetricsLoading(true)
    apiClient.getBatchPodMetrics(cluster, selectedNamespace)
      .then(setBatchMetrics)
      .finally(() => setMetricsLoading(false))
  }, [cluster, selectedNamespace])
  ```

- [ ] **7.3** Substituir o slot vazio do painel direito:
  - Localizar onde `!selectedPod` resulta em conteúdo vazio
  - Substituir por:
    ```tsx
    {rightView.kind === "pod-table" && (
      <PodMonitorTable
        cluster={cluster}
        pods={filteredPods}
        loading={loading}
        metrics={batchMetrics}
        metricsLoading={metricsLoading}
        headerLabel={`pods(${selectedNamespace || "all"})[${filteredPods.length}]`}
        onSelectPod={(pod) => setRightView({ kind: "pod-logs", pod })}
        onRequestRefresh={fetchPods}
      />
    )}
    {rightView.kind === "pod-logs" && (
      <PodLogsPanel
        cluster={cluster}
        pod={rightView.pod}
        onBack={() => setRightView({ kind: "pod-table" })}
        backLabel={`← pods(${rightView.pod.namespace})`}
      />
    )}
    ```

- [ ] **7.4** Quando o usuário seleciona um pod pelo painel esquerdo (comportamento atual),
  o `selectedPod !== null` continua controlando o painel de detalhes existente. O `rightView`
  só é relevante quando `selectedPod === null`.

- [ ] **7.5** Reset de `rightView` quando cluster ou namespace muda:
  ```typescript
  useEffect(() => {
    setRightView({ kind: "pod-table" })
  }, [cluster, selectedNamespace])
  ```

---

## Fase 8 — Integrar em `DeploymentsTab`

**Arquivo**: `src/components/DeploymentsTab.tsx`

- [ ] **8.1** Adicionar estados de navegação:
  ```typescript
  type DeploymentRightView =
    | { kind: "deployment-table" }
    | { kind: "pod-table"; deployment: DeploymentSummary }
    | { kind: "pod-logs"; pod: PodSummary }

  const [rightView, setRightView] = useState<DeploymentRightView>({ kind: "deployment-table" })
  const [monitorPods, setMonitorPods] = useState<PodSummary[]>([])
  const [monitorPodsLoading, setMonitorPodsLoading] = useState(false)
  const [batchMetrics, setBatchMetrics] = useState<BatchPodMetrics | null>(null)
  const [metricsLoading, setMetricsLoading] = useState(false)
  ```

- [ ] **8.2** Função de drill-down: deployment → pods:
  ```typescript
  const handleMonitorDeployment = async (dep: DeploymentSummary) => {
    setRightView({ kind: "pod-table", deployment: dep })
    setMonitorPodsLoading(true)
    try {
      const allPods = await apiClient.getPods(cluster, [dep.namespace])
      // Filtra pods pelo nome do deployment (heurística: pod.name começa com dep.name + "-")
      const depPods = allPods.filter(p =>
        p.name.startsWith(dep.name + "-") ||
        p.labels?.["app"] === dep.labels?.["app"]
      )
      setMonitorPods(depPods)
      // Buscar métricas
      setMetricsLoading(true)
      const m = await apiClient.getBatchPodMetrics(cluster, dep.namespace)
      setBatchMetrics(m)
    } catch { }
    finally {
      setMonitorPodsLoading(false)
      setMetricsLoading(false)
    }
  }
  ```

- [ ] **8.3** Localizar `renderManifestPanel` (função que retorna o conteúdo do painel direito)
  e adicionar ANTES do bloco existente (que só roda quando `selectedDeployment !== null`):
  ```typescript
  const renderManifestPanel = () => {
    // PRIORIDADE 1: deployment selecionado pela lista esquerda → YAML editor (comportamento atual)
    if (selectedDeployment) {
      // ... todo código existente, sem alteração ...
    }

    // PRIORIDADE 2: monitoring navigation (nenhum deployment selecionado para edição)
    if (rightView.kind === "deployment-table") {
      return (
        <DeploymentMonitorTable
          deployments={filteredDeployments}
          loading={loading}
          headerLabel={`deployments(${nsLabel})[${filteredDeployments.length}]`}
          onSelectDeployment={handleMonitorDeployment}
          onOpenEditor={handleSelectDeployment}  // função existente
          onRequestRefresh={refetch}
        />
      )
    }

    if (rightView.kind === "pod-table") {
      return (
        <div className="flex flex-col h-full">
          <button
            onClick={() => setRightView({ kind: "deployment-table" })}
            className="text-xs text-muted-foreground hover:text-foreground mb-2 text-left"
          >
            ← deployments({rightView.deployment.namespace})
          </button>
          <PodMonitorTable
            cluster={cluster}
            pods={monitorPods}
            loading={monitorPodsLoading}
            metrics={batchMetrics}
            metricsLoading={metricsLoading}
            headerLabel={`pods(${rightView.deployment.namespace}/${rightView.deployment.name})[${monitorPods.length}]`}
            onSelectPod={(pod) => setRightView({ kind: "pod-logs", pod })}
            onRequestRefresh={() => handleMonitorDeployment(rightView.deployment)}
          />
        </div>
      )
    }

    if (rightView.kind === "pod-logs") {
      return (
        <PodLogsPanel
          cluster={cluster}
          pod={rightView.pod}
          onBack={() => setRightView({ kind: "pod-table", deployment: (rightView as any)._from ?? { kind: "deployment-table" } })}
          backLabel={`← pods(${rightView.pod.namespace})`}
        />
      )
    }
  }
  ```

  > **Nota**: Guardar `_from` para que o botão voltar do pod-logs saiba para onde voltar
  > (pod-table com o deployment correto). Alternativa mais limpa: adicionar campo `from`
  > no estado `pod-logs`:
  > ```typescript
  > | { kind: "pod-logs"; pod: PodSummary; fromDeployment: DeploymentSummary }
  > ```

- [ ] **8.4** Reset de `rightView` quando cluster ou namespace muda:
  ```typescript
  useEffect(() => {
    setRightView({ kind: "deployment-table" })
    setMonitorPods([])
    setBatchMetrics(null)
  }, [cluster, namespacesKey])
  ```

- [ ] **8.5** Título dinâmico do painel direito (no SplitView ou no header):
  ```typescript
  const rightPanelTitle = selectedDeployment
    ? `Manifesto — ${selectedDeployment.name}`
    : rightView.kind === "deployment-table" ? "Monitoramento"
    : rightView.kind === "pod-table" ? `Pods — ${rightView.deployment.name}`
    : `Logs — ${rightView.pod.name}`
  ```

---

## Fase 9 — Build, Testes e Ajustes

- [ ] **9.1** Build backend:
  ```bash
  make build
  ```

- [ ] **9.2** Build frontend:
  ```bash
  ./rebuild-web.sh -b
  ```

- [ ] **9.3** Testes manuais — DeploymentsTab:
  - [ ] Nenhum deployment selecionado → tabela de deployments aparece no painel direito
  - [ ] Cores corretas (laranja para deployments com problema)
  - [ ] Auto-refresh a cada 10s (verificar no Network tab do browser)
  - [ ] Clicar linha → tabela de pods do deployment aparece
  - [ ] Botão `←` volta para tabela de deployments
  - [ ] Clicar pod → PodLogsPanel aparece com logs
  - [ ] Auto-refresh de logs ligado por padrão
  - [ ] Botão copiar funciona
  - [ ] Botão `←` em logs volta para pods do deployment
  - [ ] Clicar na lista esquerda (card) ainda abre o YAML editor normalmente

- [ ] **9.4** Testes manuais — PodsPanel:
  - [ ] Nenhum pod selecionado → tabela de pods aparece no painel direito
  - [ ] Clicar pod → PodLogsPanel inline
  - [ ] Botão `←` volta para tabela de pods
  - [ ] Clicar na lista esquerda ainda abre o painel de detalhes normalmente

- [ ] **9.5** Testar graceful degradation de métricas:
  - [ ] Colunas CPU/MEM mostram `n/a` quando metrics-server indisponível
  - [ ] Tabela não quebra sem métricas

- [ ] **9.6** Testes responsividade:
  - [ ] Tabela de pods tem scroll horizontal em telas menores
  - [ ] Sem quebra de layout em 1920x1080

---

## Commit sugerido

```
feat(monitoring): tabelas live de deployments e pods com navegação hierárquica

- Painel direito default: tabela terminal estilo kubectl com auto-refresh
- DeploymentsTab: deployments → pods → logs com botão voltar
- PodsPanel: pods → logs com botão voltar
- Comportamento existente (YAML editor) preservado sem alterações
- Backend: endpoint GET /pods/metrics para batch metrics em uma única chamada
- PodLogsPanel: auto-refresh ON por padrão, botão copiar, syntax highlighting
```

---

## Status

- [x] Fase 1 — Backend batch metrics
- [x] Fase 2 — Tipos TypeScript
- [x] Fase 3 — Utilitários (formatAge, etc.)
- [x] Fase 4 — PodLogsPanel
- [x] Fase 5 — PodMonitorTable
- [x] Fase 6 — DeploymentMonitorTable
- [x] Fase 7 — Integração PodsPanel
- [x] Fase 8 — Integração DeploymentsTab
- [x] Fase 9 — Build e testes

## Extras implementados (além do checklist original)

- **PodQuickViewModal**: Modal de detalhes rápido ao clicar em um pod na tabela
  - Gauge duplo concêntrico CPU/MEM (% vs request)
  - Grid de informações: namespace, restarts, ready, IP, node (sem truncar), resources
  - Tab Logs: container selector, tail lines, auto-refresh 5s, copiar, scroll corrigido
  - Ações na aba Detalhes (seção "Ações"): Rollout Restart, Kill (Forçar), Deletar Pod
  - Confirmação inline com descrição da ação antes de executar
  - RBAC: todos os botões protegidos com `<ProtectedAction>`
  - `onRefresh` conectado: PodsPanel → `fetchPods(true)`, DeploymentsTab → `refreshMonitorPods`
- **PodMonitorTable**: filtros por Status, Node, Namespace (popovers com checkboxes), busca, chips de filtros ativos, indicador "X atrás"
- **DeploymentMonitorTable**: filtros por Status (Saudável/Degradado) e Namespace, busca, botão lápis para abrir editor YAML
