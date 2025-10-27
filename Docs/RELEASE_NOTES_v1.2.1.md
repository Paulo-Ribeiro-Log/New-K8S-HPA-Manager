# Release Notes v1.2.1

**Data de Lançamento**: 24 de outubro de 2025
**Versão**: v1.2.1
**Tipo**: Patch Release (Bug Fixes + Features)

---

## 🎯 Novas Funcionalidades

### Campo de Busca Inteligente
- ✅ **Painel HPAs**: Campo de busca por nome e namespace
- ✅ **Painel Node Pools**: Campo de busca por nome e cluster
- ✅ Interface consistente com ícone de lupa
- ✅ Feedback visual quando nenhum item é encontrado
- ✅ Busca case-insensitive em tempo real

**Benefício**: Aumenta significativamente a produtividade ao trabalhar com clusters grandes (70+ HPAs/Node Pools)

### Modal de Edição Inline (ApplyAllModal)
- ✅ **Edição Completa de HPAs**:
  - Min/Max Replicas
  - Target CPU/Memory (%)
  - CPU/Memory Request/Limit
  - Opções de Rollout (Deployment, DaemonSet, StatefulSet)
- ✅ **Dropdown Menu (⋮)** com ações individuais:
  - **Editar Conteúdo**: Abre modal de edição inline
  - **Remover da Lista**: Remove item da lista de alterações
- ✅ **Validação de Campos**:
  - Min Replicas não pode ser maior que Max Replicas
  - Target CPU/Memory deve estar entre 1-100%
- ✅ **Botão "Aplicar"**: Mantido separado do dropdown para ação rápida

**Benefício**: Permite corrigir erros antes de aplicar alterações sem precisar voltar ao editor principal

---

## 🐛 Correções de Bugs Críticos

### 1. Bug de Restart ao Aplicar Node Pools ✅
**Problema**:
- Aplicação de Node Pools causava `window.location.reload()`
- Página recarregava completamente
- Perda de estado e contexto durante operações longas

**Solução**:
- Removido `window.location.reload()` do `Index.tsx`
- Implementado sistema de eventos customizados (`rescanNodePools`)
- Adicionado listener no hook `useNodePools` para refetch via API
- Dados atualizados sem perda de estado

**Impacto**: Operações de Node Pools agora são estáveis e previsíveis

### 2. Bug de Perda de Dados ao Refresh ✅
**Problema**:
- Refresh involuntário durante operações
- Modais fechavam inesperadamente
- Staging area era limpa sem confirmação

**Solução**:
- Removido reload forçado que causava instabilidade
- Sistema de eventos mantém estado durante operações
- Refetch via API sem reload da página

**Impacto**: Usuário não perde mais trabalho durante operações longas

---

## 📊 Melhorias de UX

### Produtividade
- **Busca Rápida**: Encontrar HPAs/Node Pools específicos em segundos
- **Edição Inline**: Corrigir erros sem interromper fluxo de trabalho
- **Feedback Visual**: Mensagens claras de sucesso/erro

### Estabilidade
- **Sem Reloads**: Operações sem recarregar página
- **Estado Preservado**: Contexto mantido durante operações
- **Sistema de Eventos**: Comunicação eficiente entre componentes

### Interface
- **Consistência**: Campos de busca com mesmo padrão visual
- **Acessibilidade**: Placeholders claros e ícones intuitivos
- **Responsividade**: Feedback imediato em todas as ações

---

## 📥 Instalação

### Nova Instalação
```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
```

### Atualização de Versão Anterior
```bash
# Opção 1: Auto-update (recomendado)
~/.k8s-hpa-manager/scripts/auto-update.sh --yes

# Opção 2: Manual
cd ~/.k8s-hpa-manager
git pull
make build-web
sudo cp ./build/k8s-hpa-manager /usr/local/bin/
```

---

## 🔄 Changelog Completo

### Arquivos Modificados
- `internal/web/frontend/src/pages/Index.tsx` (+129 linhas)
- `internal/web/frontend/src/hooks/useAPI.ts` (+32 linhas)
- `internal/web/frontend/src/components/ApplyAllModal.tsx` (+355 linhas)
- `internal/web/static/` (rebuild frontend)

### Commits
- `a098820` - feat: adiciona busca e corrige bugs de reload na interface web
- `683842b` - feat: adiciona dropdown menu para ações individuais no ApplyAllModal
- `b09a313` - fix: corrige 2 bugs críticos na instalação e startup

**Comparação**: [v1.2.0...v1.2.1](https://github.com/Paulo-Ribeiro-Log/Scale_HPA/compare/v1.2.0...v1.2.1)

---

## 🧪 Testes Recomendados

Após atualizar para v1.2.1, teste:

1. **Campo de Busca**:
   - Buscar HPAs por nome/namespace
   - Buscar Node Pools por nome/cluster
   - Verificar feedback quando nada é encontrado

2. **Edição Inline**:
   - Editar HPA no modal de confirmação
   - Validar campos com valores inválidos
   - Remover item da lista de alterações

3. **Node Pools**:
   - Aplicar alterações em Node Pool
   - Verificar que página não recarrega
   - Confirmar que dados são atualizados automaticamente

4. **Estabilidade**:
   - Fazer operações longas (múltiplos Node Pools)
   - Verificar que estado é mantido
   - Confirmar que não há perda de dados

---

## 📚 Documentação Atualizada

- **CLAUDE.md**: Atualizado com novas features e correções
- **README.md**: Atualizado com versão v1.2.1
- **RELEASE_NOTES_v1.2.1.md**: Este arquivo

---

## 🆘 Suporte

- **Issues**: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/issues
- **Documentação**: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/blob/main/README.md
- **Changelog**: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/blob/main/Docs/RELEASE_NOTES_v1.2.1.md

---

## 🙏 Agradecimentos

Esta release foi desenvolvida com assistência da Claude Code (Anthropic).

**Release Notes gerado em**: 24 de outubro de 2025
**Versão**: v1.2.1
**Branch**: k8s-hpa-manager-dev2
