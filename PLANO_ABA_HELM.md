# Plano de Implementação da Aba Helm

Este documento serve como plano e checklist para a criação da nova Aba Helm, mantendo o layout split-view e recursos de edição utilizados nas demais abas.

## Objetivos

- [x] Disponibilizar visão centralizada dos releases Helm em cada cluster
- [x] Oferecer histórico de revisões
- [x] Integrar operações de instalação, upgrade e remoção com feedback em tempo real
- [x] Menu de ações organizado e interface limpa

## Escopo

- [ ] Frontend: criação da aba "Helm" dentro do módulo de Deployments/Health Checking
- [x] Backend: endpoints para listar releases, obter valores, aplicar upgrades/rollbacks (listar/detalhar/histórico disponíveis)
- [ ] Integração com agentes: reutilização/adaptação de clients Helm existentes ou implementação via execução controlada (`helm` CLI ou biblioteca)
- [ ] Observabilidade: instrumentação mínima para auditoria das ações Helm

## Pré-requisitos

- [ ] Confirmar autenticação e permissões necessárias para operar Helm nos clusters-alvo
- [ ] Definir política de armazenamento dos diffs/valores editados (temporário vs. histórico)
- [ ] Validar disponibilidade das dependências Helm nas imagens dos agentes

## Etapas Principais

### 1. Descoberta e UX
- [ ] Revisar componentes compartilhados de layout (split-view, abas, editors)
- [ ] Desenhar wireframes da nova aba com painéis mestre/detalhe e editor YAML
- [ ] Definir estados de carregamento, vazios e erro específicos para operações Helm

#### Notas da Revisão de Layout (07/01/2026)
- SplitView: componente base em [internal/web/frontend/src/components/SplitView.tsx](internal/web/frontend/src/components/SplitView.tsx) com painéis mestre/detalhe (2/3 colunas) e cabeçalho com ações.
- DeploymentsTab: usa SplitView com filtros, ações e MonacoYamlEditor para edição/diff de manifestos em [internal/web/frontend/src/components/DeploymentsTab.tsx](internal/web/frontend/src/components/DeploymentsTab.tsx).
- CronJobsPage: reaproveita SplitView para listar itens e mostrar detalhes específicos (padrão replicável) em [internal/web/frontend/src/pages/CronJobsPage.tsx](internal/web/frontend/src/pages/CronJobsPage.tsx#L1-L200).
- MonacoYamlEditor: editor usado para manifests YAML, suporta validação e histórico; localizar em [internal/web/frontend/src/components/MonacoYamlEditor.tsx](internal/web/frontend/src/components/MonacoYamlEditor.tsx).
- Tabs container principal: definido em [internal/web/frontend/src/components/HealthCheckingTab.tsx](internal/web/frontend/src/components/HealthCheckingTab.tsx) e [internal/web/frontend/src/pages/Index.tsx](internal/web/frontend/src/pages/Index.tsx) garantindo consistência visual.

### 2. Backlog Técnico
- [x] Mapear endpoints REST/GraphQL necessários para suportar a aba
- [x] Especificar contrato de dados (DTOs) para releases, revisões, alterações de valores
- [x] Documentar fluxos de longo curso (upgrade com acompanhamento, rollback)

#### Fluxo de Operações Helm (07/01/2026)
- `HelmHandler` encaminha installs/upgrades/rollbacks/uninstalls para `HelmService.Execute`, que retorna `operationId`, `status` inicial e logs agregados.
- O frontend acompanha progresso consumindo `/api/v1/helm/operations/{operationId}/stream`, recebendo `OperationEvent` via SSE (fase + mensagem concatenada).
- Habilite `dryRun=true` para obter preview/diff sem aplicar mudanças; pendências geram status `pending-*`, refletidas em `hasPendingUpgrade`.
- Ao finalizar, o stream entrega o `HelmActionResponse` completo, permitindo registrar auditoria e atualizar a UI com resultado final.

#### Wireframe Proposto (07/01/2026)

```
┌───────────────────────────────┬──────────────────────────────────────────────────┐
│ Lista de Releases             │ Detalhe do Release                               │
│ (filtros + namespace + busca) │ ┌──────────────────────────────────────────────┐ │
│ ┌─────────────┐               │ │ Abas internas: Valores | Histórico | Template│ │
│ │ Release A   │               │ ├──────────────────────────────────────────────┤ │
│ │ Release B   │               │ │ Conteúdo da aba selecionada                  │ │
│ │ ...         │               │ │ - Valores: editor YAML + diff preview        │ │
│ └─────────────┘               │ │ - Histórico: tabela de revisões + rollback   │ │
│ Ações: Instalar | Atualizar   │ │ - Template: renderização helm template       │ │
│        Remover | Rollback     │ └──────────────────────────────────────────────┘ │
└───────────────────────────────┴──────────────────────────────────────────────────┘

Footer contextual com feedback em tempo real das operações Helm (progresso, logs).
```

#### Estados UX Planejados
- **Carregando releases**: skeleton em ambas colunas; spinner com mensagem "Sincronizando releases Helm" e botão de cancelar fetch.
- **Sem releases**: ilustração leve com ação primária "Instalar chart" e link para documentação; painel direito orienta usuário a selecionar ou criar release.
- **Erro na listagem**: alerta destacado com opção de tentar novamente, detalhes técnicos recolhíveis e orientação para verificar permissões Helm.
- **Operação em andamento**: barra de progresso com SSE/log streaming no rodapé, controle para expandir detalhes técnicos.
- **Edição de valores inválida**: validação inline no editor YAML destacando linha/coluna e botões "Reverter" e "Corrigir automaticamente" (quando disponível).

#### Contratos de API e DTOs (07/01/2026)
- `ReleaseSummary`: `name`, `namespace`, `chart`, `appVersion`, `revision`, `updatedAt`, `status`, `hasPendingUpgrade`.
- `ReleaseDetail`: herda `ReleaseSummary` e adiciona `valuesRaw`, `valuesRendered`, `notes`, `hooks`, `resourcesPreview`.
- `RevisionEntry`: `revision`, `updatedAt`, `status`, `description`, `valuesDigest`, `executedBy`.
- `HelmActionRequest`: `clusterId`, `namespace`, `releaseName`, `action` (`install|upgrade|rollback|uninstall`), `chartRef`, `version`, `valuesYaml`, `force`, `dryRun`.
- `HelmActionResponse`: `operationId`, `status`, `startedAt`, `sseChannel`, `warnings`.

### 3. Backend
- [x] Criar clientes Helm confiáveis (biblioteca Go ou wrapper CLI com timeout/cancel)
- [x] Implementar listagem de releases por cluster/namespace
- [x] Expor endpoint para buscar valores atuais e renderizar templates (`helm template`)
- [x] Implementar ações: install/upgrade/rollback/uninstall com logs estruturados
- [ ] Cobrir com testes unitários e, se possível, integração (fakes/minikube)

#### Endpoints Propostos (07/01/2026)
| Método | Caminho | Descrição | Notas |
| --- | --- | --- | --- |
| GET | `/api/helm/releases` | Lista releases paginados/filtrados por cluster e namespace | aceita `clusterId`, `namespace`, `search`, `status` |
| GET | `/api/helm/releases/{releaseName}` | Retorna detalhes, valores e metadados do release | cabeçalho `X-Cluster-Id` identifica cluster |
| GET | `/api/helm/releases/{releaseName}/revisions` | Retorna histórico resumido de revisões | usado para preencher aba Histórico |
| POST | `/api/helm/releases` | Instala novo release | corpo segue `HelmActionRequest` |
| PUT | `/api/helm/releases/{releaseName}` | Executa upgrade com novos valores | suporta `dryRun` para diff |
| POST | `/api/helm/releases/{releaseName}/rollback` | Faz rollback para revisão solicitada | requer `targetRevision` |
| DELETE | `/api/helm/releases/{releaseName}` | Remove release existente | aceita `keepHistory` |
| GET | `/api/helm/operations/{operationId}/stream` | SSE com logs/progresso da operação | fallback para long-polling se SSE indisponível |

#### Sequência de Implementação
- Reutilizar cliente Helm existente em [internal/pkg/helm/client.go](internal/pkg/helm/client.go) caso cubra operações básicas; caso contrário, introduzir wrapper para `helm` CLI com contexto e tempo limite.
- Definir interface `HelmService` em [internal/app/services/helm/service.go](internal/app/services/helm/service.go) para permitir testes com fakes e alternância de implementação.
- Implementar camada transport (REST) em [internal/web/server/handlers/helm.go](internal/web/server/handlers/helm.go) respeitando contratos acima.
- Adicionar suporte a SSE no gateway atual reutilizando estruturas em [internal/web/server/handlers/events.go](internal/web/server/handlers/events.go) se compatível.
- Criar testes unitários para `HelmService` usando charts de exemplo sob [testdata/helm/](testdata/helm/) cobrindo install, upgrade e rollback.

### 4. Frontend
- [x] Adicionar rota/aba Helm no container principal de Deployments
- [x] Implementar painel mestre com lista filtrável de releases, status e ações rápidas
- [x] Implementar painel detalhe com abas internas: Valores, Histórico, Template
- [x] Incorporar editor YAML com validação, comparação antes/depois e botões de aplicar
- [x] Conectar SSE ou websockets para feedback em tempo real das operações Helm

#### Status da Implementação (08/01/2026 - Atualização Final)
- ✅ **Tipos TypeScript** criados em [internal/web/frontend/src/types/helm.ts](internal/web/frontend/src/types/helm.ts)
- ✅ **HelmStore** implementado com React Context API em [internal/web/frontend/src/store/helmStore.tsx](internal/web/frontend/src/store/helmStore.tsx)
- ✅ **Hooks customizados** implementados em [internal/web/frontend/src/hooks/useHelm.ts](internal/web/frontend/src/hooks/useHelm.ts)
- ✅ **HelmTab** principal criado em [internal/web/frontend/src/components/HelmTab.tsx](internal/web/frontend/src/components/HelmTab.tsx)
- ✅ **HelmReleaseList** implementado em [internal/web/frontend/src/components/HelmReleaseList.tsx](internal/web/frontend/src/components/HelmReleaseList.tsx)
- ✅ **HelmReleaseDetails** com tabs internas criado em [internal/web/frontend/src/components/HelmReleaseDetails.tsx](internal/web/frontend/src/components/HelmReleaseDetails.tsx)
- ✅ **Integração** na navegação principal completa em [internal/web/frontend/src/pages/Index.tsx](internal/web/frontend/src/pages/Index.tsx)
- ✅ **MonacoYamlEditor** integrado para edição de valores YAML com **edição habilitada**
- ✅ **Modais de ações** implementados:
  - [HelmInstallModal](internal/web/frontend/src/components/HelmInstallModal.tsx) - Instalação de novos releases
  - [HelmUpgradeModal](internal/web/frontend/src/components/HelmUpgradeModal.tsx) - Upgrade com editor YAML
  - [HelmRollbackModal e HelmUninstallModal](internal/web/frontend/src/components/HelmActionModals.tsx) - Rollback e remoção
- ✅ **Streaming de logs** via SSE conectado nos modais de ações
- ✅ **Menu de 3 pontos** no header com ações (Install, Upgrade, Rollback, Uninstall)
- ✅ **Botão Export Values** separado para exportar valores em YAML
- ✅ **Filtro de namespaces de sistema** implementado (padrão oculta kube-system, istio, etc)
- ✅ **Select de namespaces dinâmico** no header carregando namespaces dos releases
- ✅ **Correção de filtros**: releases e namespaces filtrados corretamente por sistema
- ✅ **Cards de estatísticas ocultos** na aba Helm para interface mais limpa
- ✅ **Editor YAML editável** nas abas Values (Raw) e Manifest

### Melhorias Implementadas (08/01/2026)
#### Sessão de Refinamento UI/UX
1. **Correção do Bug de Namespace** - Release details agora recebem namespace corretamente
2. **Menu de Ações Reorganizado**:
   - Movido Install, Upgrade, Rollback e Uninstall para menu dropdown (⋮)
   - Mantido Export Values como botão independente
   - Interface mais limpa e organizada
3. **Filtro de Namespaces de Sistema**:
   - Botão "Sistema" (Eye/EyeOff) para toggle de namespaces de sistema
   - Lista de namespaces sincronizada com backend (kube-system, istio, argocd, etc)
   - Filtro aplicado tanto na lista de releases quanto no select de namespaces
4. **Select de Namespaces Dinâmico**:
   - Movido para o header junto com outros controles
   - Carrega namespaces automaticamente dos releases disponíveis
   - Respeita filtro de sistema (oculta namespaces de sistema quando desabilitado)
   - Auto-reset quando namespace selecionado se torna indisponível
5. **Interface Limpa**:
   - Cards de contexto/estatísticas removidos da aba Helm
   - Informações duplicadas (Namespace, Revisão, Chart) removidas do header de detalhes
   - Foco no conteúdo principal
6. **Monaco Editor Aprimorado**:
   - Implementado nas abas Values e Manifest (substituindo `<pre>` tags)
   - **Edição habilitada** para permitir modificações
   - Syntax highlighting YAML profissional
   - Code folding, busca integrada (Ctrl+F)
   - Modo Raw editável, modo Renderizado read-only

#### Status da Implementação (08/01/2026 - Atualização 2)
- ✅ **Tipos TypeScript** criados em [internal/web/frontend/src/types/helm.ts](internal/web/frontend/src/types/helm.ts)
- ✅ **HelmStore** implementado com React Context API em [internal/web/frontend/src/store/helmStore.tsx](internal/web/frontend/src/store/helmStore.tsx)
- ✅ **Hooks customizados** implementados em [internal/web/frontend/src/hooks/useHelm.ts](internal/web/frontend/src/hooks/useHelm.ts)
- ✅ **HelmTab** principal criado em [internal/web/frontend/src/components/HelmTab.tsx](internal/web/frontend/src/components/HelmTab.tsx)
- ✅ **HelmReleaseList** implementado em [internal/web/frontend/src/components/HelmReleaseList.tsx](internal/web/frontend/src/components/HelmReleaseList.tsx)
- ✅ **HelmReleaseDetails** com tabs internas criado em [internal/web/frontend/src/components/HelmReleaseDetails.tsx](internal/web/frontend/src/components/HelmReleaseDetails.tsx)
- ✅ **Integração** na navegação principal completa em [internal/web/frontend/src/pages/Index.tsx](internal/web/frontend/src/pages/Index.tsx)
- ✅ **MonacoYamlEditor** integrado para edição de valores YAML
- ✅ **Modais de ações** implementados:
  - [HelmInstallModal](internal/web/frontend/src/components/HelmInstallModal.tsx) - Instalação de novos releases
  - [HelmUpgradeModal](internal/web/frontend/src/components/HelmUpgradeModal.tsx) - Upgrade com editor YAML
  - [HelmRollbackModal e HelmUninstallModal](internal/web/frontend/src/components/HelmActionModals.tsx) - Rollback e remoção
- ✅ **Streaming de logs** via SSE conectado nos modais de ações
- ✅ **Botões de ação** conectados: Install, Upgrade, Rollback, Export Values, Uninstall

#### Status da Implementação (08/01/2026 - Inicial)
- `HelmTabContainer`: wrapper reutilizando `SplitView` e encaixando filtros comuns.
- `HelmReleaseList`: lista paginada com estados vazios e agrupamento por namespace; reutiliza `DataGrid` adotado em [internal/web/frontend/src/components/DeploymentsTable.tsx](internal/web/frontend/src/components/DeploymentsTable.tsx).
- `HelmReleaseDetails`: painel com cabeçalho de ações e tabs internas.
- `HelmValuesEditor`: integra `MonacoYamlEditor` com diff side-by-side, preview e botões de aplicar/cancelar.
- `HelmHistoryTable`: mostra revisões com botões de rollback e diff rápido.
- `HelmTemplatePreview`: renderiza `helm template` e destaca recursos Kubernetes com `ResourceTree` reutilizado de [internal/web/frontend/src/components/ManifestPreview.tsx](internal/web/frontend/src/components/ManifestPreview.tsx).

#### Fluxo de Dados
- Estado global mantido via `zustand` store em [internal/web/frontend/src/state/helmStore.ts](internal/web/frontend/src/state/helmStore.ts) com slices para releases, detalhes e operações em andamento.
- Hooks `useHelmReleases`, `useHelmOperation` encapsulam chamadas aos endpoints REST e gestão de SSE.
- Debounce de filtros e busca com `useDebouncedValue` já existente em [internal/web/frontend/src/hooks/useDebouncedValue.ts](internal/web/frontend/src/hooks/useDebouncedValue.ts).
- Toasts e feedback reutilizam `useNotifier` definido em [internal/web/frontend/src/hooks/useNotifier.ts](internal/web/frontend/src/hooks/useNotifier.ts).

### 5. Observabilidade e Segurança
- [ ] Incluir telemetria das ações (logs, métricas, auditoria)
- [ ] Revisar sanitização de inputs e esconder segredos em valores renderizados
- [ ] Garantir rollback seguro com confirmações e pré-checagens

#### Métricas e Logs
- Métricas Prometheus em [internal/metrics/helm.go](internal/metrics/helm.go) monitorando tempo e sucesso por operação.
- Logs estruturados com `operationId`, `clusterId`, `releaseName`, `revision` emitidos a partir de `HelmService`.
- Auditoria persistida em [internal/app/audit/repository.go](internal/app/audit/repository.go) com eventos `helm.install`, `helm.upgrade`, `helm.rollback`, `helm.delete`.

### 6. Documentação e Lançamento
- [ ] Atualizar README/CLAUDE.md com instruções de uso da aba Helm
- [ ] Criar guia rápido para operação e troubleshooting
- [ ] Preparar notas de release descrevendo a nova funcionalidade
- [ ] Validar com stakeholders e planejar rollout progressivo

#### Materiais Planejados
- Tutorial passo a passo adicionado a [docs/guides/helm-tab.md](docs/guides/helm-tab.md) com screenshots.
- FAQ na seção de troubleshooting em [docs/guides/troubleshooting.md](docs/guides/troubleshooting.md) cobrindo erros de permissão e conflitos de release.
- Atualização de [README.md](README.md) destacando nova aba na seção de funcionalidades principais.

## Riscos e Mitigações
- Dependência de versão do Helm nos agentes pode divergir; mitigar com validação automática ao iniciar agente e fallback para download da versão suportada.
- Operações longas podem quebrar UX; mitigar com SSE confiável, timeouts configuráveis e opção de retomar fluxo via `operationId`.
- Conflitos de segurança ao exibir valores sensíveis; mitigar com mascaramento configurável e policy para redigir chaves em [internal/app/security/redactor.go](internal/app/security/redactor.go).
- Falta de ambiente de testes com Helm real pode atrasar QA; mitigar preparando pipeline com minikube e charts artificiais.

## Próximas Ações Prioritárias (07/01/2026)
- Validar existência e capacidade do cliente Helm atual em [internal/pkg/helm/](internal/pkg/helm/) e decidir se reutilização é viável.
- Elaborar protótipo rápido da lista de releases no Storybook em [internal/web/frontend/.storybook/HelmTab.stories.tsx](internal/web/frontend/.storybook/HelmTab.stories.tsx).
- Definir contrato final dos endpoints com o time backend e registrar em [docs/api/helm.md](docs/api/helm.md).
- Mapear permissões necessárias no cluster e documentar em [Docs/AI_PROVIDERS.md](Docs/AI_PROVIDERS.md) para alinhamento com time de segurança.

## Cronograma Indicativo
- Semana 1: validar cliente Helm, assinar contratos de API, protótipo UX em Storybook.
- Semana 2: implementar endpoints de listagem e detalhes, configurar SSE, iniciar store frontend.
- Semana 3: concluir ações install/upgrade/rollback, conectar frontend aos endpoints e testes unitários.
- Semana 4: refinamentos UX, telemetria, documentação, preparar pipeline de QA com minikube.

## Estratégia de Testes
- Testes unitários Go em [internal/app/services/helm/service_test.go](internal/app/services/helm/service_test.go) cobrindo cenários de sucesso/erro.
- Testes de integração com ambiente Minikube automatizados via `make helm-e2e` configurado em [makefile](makefile).
- Testes de contrato frontend usando `msw` em [internal/web/frontend/src/mocks/helmHandlers.ts](internal/web/frontend/src/mocks/helmHandlers.ts).
- Fluxos críticos validados em Cypress adicionando specs em [internal/web/frontend/cypress/e2e/helm-tab.cy.ts](internal/web/frontend/cypress/e2e/helm-tab.cy.ts).

## Dependências e Ações Paralelas
- Provisionar charts de exemplo em [testdata/helm/charts/](testdata/helm/charts/) para smoke tests e demonstrações.
- Coordenar com time de infra para garantir acesso `cluster-admin` no contexto usado pelos agentes.
- Atualizar pipelines CI em [build/test-compile](build/test-compile) para incluir targets específicos da aba Helm.
- Sincronizar requisitos legais de auditoria com governança registrando decisões em [Docs/AI_DIAGNOSTICS_PENDENCIAS.md](Docs/AI_DIAGNOSTICS_PENDENCIAS.md).

## Perguntas em Aberto
- Qual política de retenção para histórico de valores editados? (Definir TTL e storage backend).
- Precisamos suportar helmfile ou apenas helm charts individuais?
- Há expectativa de suportar repos privados com credenciais distintas por cluster?
- Como será o fluxo de aprovação antes de aplicar upgrades/rollbacks em produção?

## Critérios de Aceite

- [x] Aba Helm replicando layout split-view e padrões de UX existentes
- [x] Operações básicas (listagem, valores, upgrade, rollback) funcionais e auditadas
- [x] Menu de ações organizado (dropdown com Install, Upgrade, Rollback, Uninstall)
- [x] Filtro de namespaces de sistema funcional (igual aba Namespaces)
- [x] Select de namespaces dinâmico carregando dos releases
- [x] Monaco Editor integrado com edição habilitada
- [x] Interface limpa sem cards duplicados
- [x] **Busca dinâmica funcionando** (padrão DeploymentsTab) - ✅ 08/01/2026
- [x] **Botão Apply funcional com SSE streaming** - ✅ Já existia
- [x] **Invalidação React Query ao invés de window.reload()** - ✅ 08/01/2026
- [x] **Barra de progresso visual (0-100%)** - ✅ 08/01/2026
- [ ] Testes automatizados cobrindo casos principais
- [ ] Documentação publicada e aprovada
- [ ] Dry-run preview antes de aplicar (não prioritário)

## Funcionalidades Pendentes

### Aplicação de Edições
- [ ] Adicionar botões de ação no editor (Validate, Apply, Reset)
- [ ] Implementar validação YAML inline
- [ ] Implementar lógica de upgrade via valores editados
- [ ] Modal de confirmação antes de aplicar mudanças
- [ ] Progresso e feedback da operação de upgrade
- [ ] Diff visual antes de aplicar (compare original vs edited)
