# Aba ConfigMaps + Monaco Editor — Possibilidades

## 1. Objetivo
- Entregar uma nova aba na interface web que permita listar os ConfigMaps (e HPAs, no futuro) dos clusters AKS selecionados e edita-los com a mesma fluidez do VS Code usando Monaco Editor.
- Validar e aplicar manifestos via backend Go/client-go sem sair do fluxo web, reduzindo o uso direto de kubectl.
- Manter filosofia KISS: reaproveitar infraestrutura existente (auth, selecao de cluster, staging, historico) e ativar features em incrementos pequenos.

## 2. Radiografia atual
### Backend (Go)
- `internal/web/server.go` expone rotas com Gin e usa `config.KubeConfigManager` para instanciar clientes Kubernetes por contexto (string do kubeconfig, ex.: `akspriv-*-admin`).
- Handlers existentes (`internal/web/handlers/*.go`) cobrem HPAs, NodePools, CronJobs e Prometheus, mas nao ha leitura/edicao de ConfigMaps.
- `internal/kubernetes/client.go` encapsula operacoes client-go; hoje conhece HPAs, Deployments, CronJobs e NodePools, mas nao possui helpers para ConfigMaps nem server-side apply.
- Historico (`internal/history`) registra aplicaçoes de HPA/NodePool; Sessions guardam alteracoes em disco (`~/.k8s-hpa-manager`).

### Frontend (React + Vite + shadcn)
- `internal/web/frontend/src/pages/Index.tsx` centraliza as abas (Dashboard, HPAs, Node Pools...). TabNavigation e selecionada via estado local; so ha selecao de um cluster por vez.
- Hooks `useClusters`, `useNamespaces`, `useHPAs`, `useNodePools` fazem fetch simples com apiClient, sem cache sofisticado para manifestos.
- Editores atuais sao baseados em formularios (inputs/checkboxes). Ainda nao existe Monaco nem validacao por schema YAML.
- Build do frontend e embedado em `internal/web/static` (servido pelo backend). Qualquer dependencia extra precisa ser suportada pelo bundle Vite.

## 3. Requisitos chave da nova aba
1. **Coleta**: listar ConfigMaps dos clusters (um ou mais) com filtros por namespace e busca textual; mostrar metadata (cluster, namespace, labels, data keys, atualizacao).
2. **Edicao**: abrir ConfigMap em Monaco Editor (modo YAML), com syntax highlight, folding e formatacao.
3. **Validacao**: integrar `monaco-yaml` com schema do objeto (apiextensions) para hover, autocomplete e hints em tempo real.
4. **Aplicacao segura**: backend deve oferecer read/apply usando client-go, com `Server-Side Apply` + `dry-run=All` antes de aplicar de verdade.
5. **Diff opcional**: gerar diff lado a lado usando diff2html antes do apply, reaproveitando dados do backend.
6. **Compatibilidade HPA** (bonus): mesmo pipeline (Monaco + apply) deve ser reutilizavel para HPAs no futuro, evitando dois fluxos distintos.
7. **Observabilidade**: registrar aplicacoes no History e opcionalmente no Staging para manter rastreabilidade.

## 4. Arquitetura proposta
### 4.1 Backend
- **Modelos**: criar `models.ConfigMapSummary` (nome, namespace, cluster, labels principais, tamanhos) e `models.ConfigMapManifest` (YAML bruto + metadata + checksum).
- **Camada Kubernetes** (`internal/kubernetes/client.go`):
  - Funcoes `ListConfigMaps(ctx, namespaceFilter []string)` e `GetConfigMap(ctx, namespace, name)` retornando tanto objeto estruturado quanto YAML serializado.
  - Helper `GenerateConfigMapYAML(cm *corev1.ConfigMap)` usando `sigs.k8s.io/yaml`.
  - Metodo `ApplyConfigMap(ctx, yaml string, fieldManager string, dryRun bool)` usando `clientset.CoreV1().ConfigMaps(ns).Patch(..., types.ApplyPatchType, payload, metav1.PatchOptions{FieldManager: fieldManager, DryRun: ...})`.
  - Reaproveitar `Server-Side Apply` para HPAs via `autoscalingv2apply.HorizontalPodAutoscalerApplyConfiguration` na mesma camada (preparando future flag para editor de HPAs).
- **Handlers** (`internal/web/handlers/configmaps.go` novo):
  1. `GET /api/v1/configmaps?cluster=ctx&namespaces=ns1,ns2&search=...` -> lista agregada.
  2. `GET /api/v1/configmaps/:cluster/:namespace/:name` -> retorna manifesto YAML + metadata.
  3. `POST /api/v1/configmaps/diff` -> recebe YAML atual e proposto, devolve diff (texto ou estrutura) para o front.
  4. `POST /api/v1/configmaps/validate` -> executa server-side apply com `DryRunAll`, retorna warnings.
  5. `PUT /api/v1/configmaps/:cluster/:namespace/:name` -> aplica de fato e registra no history.
- **Diff server-side**: usar `github.com/google/go-cmp/cmp` ou `k8s.io/apimachinery/pkg/util/diff` para gerar diff estruturado; retornar tambem o YAML renderizado antigo/novo para o front alimentar diff2html.
- **Autenticacao e auditoria**: seguir AuthMiddleware existente; logar aplicacoes via `historyTracker.AddEntry` com tipo `configmap`.
- **Perf/KISS**:
  - Limitar payload por padrao (ex.: so chaves e 200 primeiros chars na listagem; YAML completo so no `GET` detalhado).
  - Adicionar `limit`/`continue` se necessario antes de pensar em watchers.
  - Reaproveitar `kubeManager.SwitchContext` quando o front escolher outro cluster (mantendo padrao atual de enviar contexto e nao nome do cluster).

### 4.2 Frontend
- **Dependencias**: adicionar `monaco-editor`, `@monaco-editor/react`, `monaco-yaml`, `js-yaml` (ou `yaml`), e `diff2html` (plus CSS). Configurar Vite para carregar os workers via `monaco-editor/esm/vs/...`.
- **Estado/Filtros**:
  - Criar `useConfigMaps` hook usando React Query (TanStack ja esta instalado) para cache por chave (`cluster+namespace+search`).
  - Permitir selecao multipla de clusters/namespace usando componentes existentes (`Select`, `Checkbox`) ou chip list (mantendo UI simples: primeiro release pode ser 1 cluster por vez, mas armazenar tipagem para varios).
- **Lista + Editor**:
  - Novo componente `ConfigMapList` (grid ou tabela) mostrando cluster/namespace/labels, com search e pagination basica.
  - `ConfigMapEditorPanel` contendo Monaco e acoes (Validar, Mostrar diff, Aplicar, Reverter para ultimo snapshot).
  - Mostrar diff em drawer/modal usando `diff2html` (import `Diff2HtmlUI` ou componente React); fallback para `pre` caso lib nao esteja disponivel.
- **Monaco + YAML**:
  - Registrar idioma `yaml` e integrar `monaco-yaml` com um schema armazenado em `src/lib/schemas/configmap.schema.json` (podemos extrair do OpenAPI do cluster ou manter schema estatico).
  - Carregar default completions (metadata.name, metadata.namespace, data, binaryData) e validar tamanho de chaves.
  - Opcional: botao para formatar YAML (`editor.getAction("editor.action.formatDocument")`).
- **Fluxo de aplicacao**:
  1. Usuario carrega manifesto -> edita -> clica `Validar` => POST validate (dry-run). Mostrar warnings/toasts.
  2. `Mostrar diff` -> requisita diff (ou reutiliza YAML original guardado local + chama lib diff2html client-side se preferir economizar roundtrip).
  3. `Aplicar` -> PUT apply. Ao concluir, disparar toast + atualizar lista + registrar no History/Staging (pode simplesmente chamar `history` endpoint para atualizar painel).
- **Reutilizacao futura**: extrair `MonacoResourceEditor` generico para ConfigMap/HPA; so trocar schema e rotas.

### 4.3 Fluxo resumido
1. Selecionar cluster(s) e namespace(s).
2. Carregar lista de ConfigMaps com paginacao simples.
3. Selecionar um item -> painel direito abre Monaco com YAML + metadata.
4. Editar -> Validar (dry-run). Em caso de erro, mostrar saida do kube-apiserver.
5. Opcional: abrir diff antes de aplicar.
6. Aplicar -> backend executa server-side apply, retorna `uid/resourceVersion`. Atualizar UI e registrar log.

## 5. Incrementos sugeridos
1. **Infra backend basica**: adicionar metodos em `internal/kubernetes/client.go` + novos handlers+rotas (sem diff ainda).
2. **Hook + listagem**: criar `useConfigMaps`, lista paginada e painel de detalhes sem editor (apenas YAML readonly) para validar API.
3. **Monaco Editor + schema**: integrar `@monaco-editor/react` + `monaco-yaml`, disponibilizar botoes Validar/Aplicar.
4. **Dry-run + diff**: implementar endpoints de validacao/diff e UI correspondente (botao `Mostrar diff`).
5. **Integraçoes avanancadas**: registrar historico/staging, habilitar mesmo pipeline para HPAs, opcional multi-cluster simultaneo.

## 6. Pontos de atencao
- **Tamanho de ConfigMaps**: objetos grandes podem pesar no Monaco; aplicar limite de MB e avisar quando `binaryData` existir (exibir somente placeholder).
- **Server-Side Apply**: fieldManager deve ser unico (ex.: `web-configmap-editor`), senão o apiserver pode negar patches se houver conflito de proprietario.
- **Dry-run com RBAC**: clusters precisam permitir `update`/`patch` com `--dry-run`; caso contrario, retornar mensagem clara.
- **Schema**: `monaco-yaml` precisa do schema Kubernetes (OpenAPI). Podemos empacotar o schema oficial da versao suportada (1.29+) para manter offline.
- **Bundle size**: `monaco-editor` aumenta ~2MB no bundle. Avaliar code splitting e carregamento preguiçoso (dynamic import) somente quando a aba for aberta.
- **Diff2html CSS**: garantir import do CSS no build e adicionar fallback plain diff para nao bloquear o fluxo caso a lib quebre.

## 7. Proximos passos imediatos (KISS)
1. Definir contrato JSON das novas rotas (`GET/PUT/POST`) e atualizar `internal/web/server.go` com os handlers vazios.
2. Mapear campos minimos do ConfigMap a exibir na lista (nome, namespace, age, labels) para evitar overfetch.
3. Criar skeleton da aba (lista + painel com textarea) sem Monaco para validar layout e fluxo de selecao.
4. So apos skeleton estar ok, adicionar dependencias Monaco/diff para evitar re-trabalho no bundle.
5. Documentar requisitos de RBAC e testar em um cluster AKS representativo antes do rollout para todos.


## 8. Contrato inicial das rotas
### 8.1 Listagem
- **Endpoint**: `GET /api/v1/configmaps`
- **Query params**:
  - `cluster` (obrigatório) — contexto/cluster atual (`akspriv-*-admin`).
  - `namespaces` (opcional) — lista CSV; vazio = todos.
  - `search` (opcional) — filtra por nome/label.
  - `limit`/`continue` (opcional) — paginação futura.
- **Resposta 200**:
```json
{
  "success": true,
  "data": [
    {
      "cluster": "akspriv-lab",
      "namespace": "app-ns",
      "name": "app-config",
      "labels": {"app": "checkout"},
      "dataKeys": ["config.yaml", "env"],
      "binaryKeys": [],
      "resourceVersion": "12345",
      "updatedAt": "2025-10-10T12:00:00Z"
    }
  ],
  "count": 1
}
```
- **Erros**: `400` (cluster ausente), `500` (falha client-go).

### 8.2 Detalhe
- **Endpoint**: `GET /api/v1/configmaps/:cluster/:namespace/:name`
- **Resposta 200**:
```json
{
  "success": true,
  "data": {
    "cluster": "akspriv-lab",
    "namespace": "app-ns",
    "name": "app-config",
    "yaml": "apiVersion: v1...",
    "metadata": {
      "uid": "...",
      "resourceVersion": "...",
      "labels": {"app": "checkout"},
      "annotations": {"managed-by": "k8s-hpa-web"}
    }
  }
}
```
- **Erros**: `404` (não encontrado), `500` (erro ao serializar ou obter objeto).

### 8.3 Diff
- **Endpoint**: `POST /api/v1/configmaps/diff`
- **Request body**:
```json
{
  "originalYaml": "apiVersion: v1...",
  "updatedYaml": "apiVersion: v1..."
}
```
- **Resposta 200** (payload inicial):
```json
{
  "success": true,
  "data": {
    "unifiedDiff": "--- original\n+++ updated\n@@ ...",
    "hasChanges": true
  }
}
```
- **Erros**: `400` (YAML inválido), `500` (falha ao gerar diff).

### 8.4 Validação (dry-run)
- **Endpoint**: `POST /api/v1/configmaps/validate`
- **Request body**:
```json
{
  "cluster": "akspriv-lab",
  "namespace": "app-ns",
  "yaml": "apiVersion: v1...",
  "fieldManager": "web-configmap-editor"
}
```
- **Resposta 200**:
```json
{
  "success": true,
  "data": {
    "warnings": [],
    "resourceVersion": "12345"
  }
}
```
- **Erros**: `400` (payload faltando), `422` (erro de validação Kubernetes retornado no dry-run), `500` (falha no client-go).

### 8.5 Aplicação
- **Endpoint**: `PUT /api/v1/configmaps/:cluster/:namespace/:name`
- **Request body**:
```json
{
  "yaml": "apiVersion: v1...",
  "fieldManager": "web-configmap-editor",
  "dryRun": false
}
```
- **Resposta 200**:
```json
{
  "success": true,
  "data": {
    "name": "app-config",
    "namespace": "app-ns",
    "cluster": "akspriv-lab",
    "resourceVersion": "12346",
    "appliedAt": "2025-11-11T03:00:00Z"
  }
}
```
- **Erros**: `400` (YAML inválido ou mismatch nome/namespace), `409` (conflito server-side apply), `500` (erro interno).

> Todas as respostas seguem padrão existente (`success` + `data`/`error`) e retornam códigos HTTP coerentes para facilitar tratamento no frontend.
