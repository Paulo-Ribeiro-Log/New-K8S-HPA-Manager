# Release v1.0.9 - Exportação de HPAs e Sistema Avançado de Cordon/Drain

**Data de lançamento:** 19 de novembro de 2025

## 🎯 Destaques da Versão

Esta release introduz **exportação de HPAs em múltiplos formatos** e **sistema avançado de gerenciamento de Node Pools** com validação de PodDisruptionBudgets e rollback automático.

---

## 🚀 Novas Funcionalidades

### 1. Sistema Completo de Exportação de HPAs

**Exportação em 3 formatos com seleção elegante**

- **Formatos disponíveis:**
  - 📊 **CSV**: Tabela plana para Excel/Sheets
  - 📝 **Markdown**: Relatório completo com metadados
  - 📄 **PDF**: Documento profissional com auto-table

- **Modo de seleção sem checkboxes:**
  - Botão "Selecionar" ativa modo visual
  - Click nas linhas para selecionar/desselecionar
  - Highlight com borda azul e ícone CheckCircle2
  - Badge contador no botão Exportar
  - Botão "Limpar" para desselecionar tudo

- **Exportação inteligente:**
  - Se nada selecionado → Exporta TODOS os HPAs
  - Se há seleção → Exporta APENAS os selecionados
  - Agrupamento automático por namespace
  - Nome de arquivo com data: `hpa-export-YYYY-MM-DD.{csv,md,pdf}`

**Casos de uso:**
- 📋 Documentação para relatórios
- 💾 Backup textual de configurações
- 🤝 Compartilhamento de estado do cluster
- 📊 Compliance e auditoria

---

### 2. Sistema Avançado de Cordon/Drain (Fase 2 e 3)

**Fase 2: Rollback Automático de Cordon**

- **Tracking de nodes cordoned:**
  - Array mantém lista de nodes com cordon aplicado
  - Função `rollbackCordon()` reverte em caso de erro

- **Rollback em 2 cenários:**
  - ❌ Erro durante CORDON → Uncordon nodes já processados
  - ❌ Erro durante DRAIN → Uncordon TODOS os nodes

- **Logs estruturados:**
  - Zerolog com contadores e detalhes
  - HTTP response inclui "details" com rollback count

**Fase 3: Validação de PDB Antes de Drain**

- **Validação preventiva:**
  - Verifica PodDisruptionBudgets ANTES de iniciar drain
  - Previne operações que ficariam travadas em timeout (600s)

- **Funções implementadas:**
  - `ValidatePDBsForNode()`: Valida PDBs para um node
  - `getPodsAffectedByPDB()`: Identifica pods protegidos
  - `podMatchesSelector()`: Match de labels (matchLabels)
  - `canDisruptPods()`: Verifica se PDB permite eviction

- **Cenários tratados:**
  - ❌ `DisruptionsAllowed = 0` → Bloqueia (HTTP 409)
  - ❌ `DisruptionsAllowed < pods` → Bloqueia (HTTP 409)
  - ✅ `DisruptionsAllowed >= pods` → Permite com warning
  - ✅ Sem pods running → Permite sem warnings

- **Rollback integrado:**
  - Erro na validação → Uncordon todos os nodes
  - PDB bloqueia → Uncordon + HTTP 409 Conflict

**Benefícios:**
- ✅ Cluster sempre consistente (rollback automático)
- ✅ Previne drain travado (validação PDB)
- ✅ Respeita SLOs de disponibilidade
- ✅ Logs detalhados para troubleshooting

---

### 3. SSE Progress Bar para Cordon/Drain

**Progresso em tempo real via Server-Sent Events**

- **4 fases monitoradas:**
  1. ⏳ **CORDON**: Marca nodes como unschedulable
  2. ⏳ **DRAIN**: Evacua pods com grace period
  3. ⏳ **AZURE**: Aplica mudanças via Azure CLI
  4. ✅ **COMPLETE**: Operação finalizada

- **Progress bar visual:**
  - Barra de progresso por fase (0-100%)
  - Contador de nodes processados
  - Status em tempo real (ex: "Draining node aks-prod-001...")

- **Handlers SSE:**
  - `/api/v1/sse/progress/:sessionId`: Stream de eventos
  - `cordon_started`, `cordon_progress`, `cordon_completed`
  - `drain_started`, `drain_progress`, `drain_completed`
  - `azure_started`, `azure_completed`
  - `error`: Erros com rollback automático

---

### 4. Melhorias na Interface Web

**Abas ConfigMaps, Secrets e Deployments**

- **ConfigMaps:**
  - Editor Monaco YAML com syntax highlighting
  - Diff visual com Diff2HTML (side-by-side)
  - Dry-run antes de apply
  - Toggle de labels

- **Secrets:**
  - Decode automático de base64
  - Busca por namespace
  - Edição segura de valores

- **Deployments:**
  - Visualização de recursos
  - Filtro por namespace
  - Painel otimizado

**Monitoramento:**
- Destaque visual em cards de CPU e latência (background azul escuro)
- Atualização silenciosa de gráficos
- AreaChart com `isAnimationActive`

**Versão da Imagem:**
- Exibição de versão da imagem nos HPAs
- Badge visual com código de versão

---

## 🐛 Correções de Bugs

### Interface Web

- **ConfigMaps/Secrets:**
  - Corrigido seletor de namespaces
  - Corrigido decode/encode de Secrets usando `yaml.dump`
  - Corrigido erros TypeScript em tabs

- **Monitoramento:**
  - Corrigido AreaChart de CPU (isAnimationActive)
  - Corrigido tipo de dados de recursos (string)

### Sistema de Instalação

- **Instalador:**
  - Busca automática de versão latest do GitHub
  - Download de binário pré-compilado
  - Verificação de requisitos

---

## 📚 Documentação

### Modularização do CLAUDE.md

**Estrutura reorganizada em módulos:**

```
CLAUDE.md (índice principal - 131 linhas)
├── docs/guides/
│   ├── QUICK_START.md
│   ├── DEVELOPMENT_COMMANDS.md
│   ├── WEB_INTERFACE.md
│   ├── COMMON_PITFALLS.md
│   ├── TESTING.md
│   ├── TROUBLESHOOTING.md
│   └── CONTINUING_DEV.md
├── docs/architecture/
│   └── OVERVIEW.md
└── docs/history/
    └── CHANGELOG.md
```

**Benefícios:**
- ✅ Redução de 97% no arquivo principal (4089 → 131 linhas)
- ✅ Links bidirecionais entre documentos
- ✅ Fácil navegação e manutenção
- ✅ Documentação focada por tópico

---

## 🔄 Dependências Atualizadas

**Frontend:**
- `jspdf@2.5.2` (NOVO) - Geração de PDFs
- `jspdf-autotable@3.8.4` (NOVO) - Tabelas em PDF

---

## 📊 Estatísticas da Release

- **Commits:** 31 desde v1.0.6
- **Arquivos modificados:** 150+
- **Linhas adicionadas:** +4500
- **Linhas removidas:** -2500
- **Código limpo:** Redução líquida de 2000 linhas

---

## 🚧 Progresso do Sistema de Cordon/Drain

### Implementado (v1.0.9)

1. ✅ **Fase 1** - Feedback visual durante drain (SSE Progress Bar)
2. ✅ **Fase 2** - Rollback automático de Cordon em caso de falha
3. ✅ **Fase 3** - Validação de PDB antes de iniciar drain

### Roadmap (próximas versões)

4. ⏳ **Fase 4** - Chunk size dinâmico baseado em número de pods
5. ⏳ **Fase 5** - Histórico de operações Cordon/Drain (audit log)

---

## 📦 Instalação

### Instalação Rápida (Recomendado)

```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

### Download Manual

**Linux (amd64):**
```bash
wget https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.9/k8s-hpa-manager-linux-amd64
chmod +x k8s-hpa-manager-linux-amd64
sudo mv k8s-hpa-manager-linux-amd64 /usr/local/bin/k8s-hpa-manager
```

**macOS (Intel):**
```bash
wget https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.9/k8s-hpa-manager-darwin-amd64
chmod +x k8s-hpa-manager-darwin-amd64
sudo mv k8s-hpa-manager-darwin-amd64 /usr/local/bin/k8s-hpa-manager
```

**macOS (Apple Silicon):**
```bash
wget https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.9/k8s-hpa-manager-darwin-arm64
chmod +x k8s-hpa-manager-darwin-arm64
sudo mv k8s-hpa-manager-darwin-arm64 /usr/local/bin/k8s-hpa-manager
```

**Windows (WSL2):**
```bash
wget https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.9/k8s-hpa-manager-windows-amd64.exe
chmod +x k8s-hpa-manager-windows-amd64.exe
```

---

## 🔄 Atualização de Versões Anteriores

```bash
# Atualização automática
~/.k8s-hpa-manager/scripts/auto-update.sh

# Verificar versão atual
k8s-hpa-manager version
```

---

## 📝 Notas de Compatibilidade

- **Sessões:** 100% compatível com v1.0.6 (formato JSON inalterado)
- **Cordon/Drain Config:** Novo campo opcional em sessões (backward compatible)
- **Exportação:** Feature nova (sem impacto em funcionalidades existentes)

---

## 🙏 Agradecimentos

Obrigado a todos que contribuíram com feedback e reportaram bugs!

**Full Changelog:** https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/compare/v1.0.6...v1.0.9
