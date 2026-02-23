# Plano de Consolidacao de Configuracoes

**Data**: 22 de Janeiro de 2026
**Objetivo**: Unificar todas as configuracoes da aplicacao em uma unica aba "Configuracoes"

---

## 1. Diagnostico Atual

### Configuracoes Espalhadas

| Configuracao | Localizacao Atual | Storage | Prioridade |
|--------------|-------------------|---------|------------|
| AI Tokens (Gemini, OpenAI, Claude, Copilot, Ollama) | Aba AI Diagnostics > Sub-aba Configuracoes | Backend SQLite (criptografado) | Alta |
| GitHub Token | Aba GitHub Releases > Modal | Backend SQLite (criptografado) | Alta |
| Nexus Repository | Aba Nexus Values > Modal | Backend SQLite (criptografado) | Alta |
| ServiceNow | Aba Health Check > Modal | Nao persiste (state local) | Media |
| Tema (Dark/Light) | Header > Toggle | localStorage | Baixa |

### Problemas Atuais
1. Usuario precisa navegar para cada aba para configurar credenciais
2. Nao ha visao unificada do status de todas as integracoes
3. Validacao de credenciais fragmentada
4. Dificuldade para troubleshooting (nao sabe o que esta configurado)

---

## 2. Proposta de Arquitetura

### 2.1 Nova Aba "Configuracoes" (Settings)

```
Configuracoes
├── Secao: Integracoes Externas
│   ├── Card: Nexus Repository Manager
│   │   ├── URL Base, Repository, Usuario, Senha
│   │   ├── Padrao de URL (avancado)
│   │   ├── Botao: Testar Conexao
│   │   └── Status: Conectado/Desconectado
│   │
│   ├── Card: GitHub
│   │   ├── Personal Access Token (PAT)
│   │   ├── Email/Login
│   │   ├── Rate Limit Info
│   │   └── Status: Valido/Invalido/Nao configurado
│   │
│   └── Card: ServiceNow (futuro)
│       ├── Instance URL
│       ├── Usuario, Senha
│       └── Status: Conectado/Desconectado
│
├── Secao: Provedores de IA
│   ├── Card: Email de Identificacao
│   │   └── Email usado para salvar configs (independente Azure AD)
│   │
│   ├── Card: Gemini (Google AI)
│   │   ├── API Key
│   │   ├── Modelo selecionado
│   │   └── Status: Configurado/Nao configurado
│   │
│   ├── Card: OpenAI (ChatGPT)
│   │   ├── API Key
│   │   ├── Modelo selecionado
│   │   └── Status: Configurado/Nao configurado
│   │
│   ├── Card: Claude (Anthropic)
│   │   ├── API Key
│   │   ├── Modelo selecionado
│   │   └── Status: Configurado/Nao configurado
│   │
│   ├── Card: Microsoft Copilot (Azure OpenAI)
│   │   ├── Endpoint, Deployment
│   │   ├── API Key
│   │   └── Status: Configurado/Nao configurado
│   │
│   └── Card: Ollama (Local)
│       ├── Modelo local (dropdown)
│       └── Status: Disponivel/Indisponivel
│
├── Secao: Preferencias
│   ├── Card: Tema
│   │   ├── Dark / Light / System
│   │   └── Preview
│   │
│   └── Card: Provedor de IA Preferido
│       ├── Select com providers disponiveis
│       └── Usado como padrao em analises
│
└── Secao: Informacoes do Sistema
    ├── Versao da Aplicacao
    ├── Usuario Azure AD logado
    ├── Grupos RBAC (SRE, etc)
    └── Ultima sincronizacao
```

### 2.2 Componentes a Criar

```
internal/web/frontend/src/
├── components/
│   ├── SettingsTab.tsx           # Nova aba principal
│   ├── settings/
│   │   ├── NexusSettingsCard.tsx     # Refatorado de NexusConfigPanel
│   │   ├── GitHubSettingsCard.tsx    # Refatorado de TokenConfigModal
│   │   ├── AISettingsCard.tsx        # Refatorado de AISettingsTab
│   │   ├── ServiceNowSettingsCard.tsx # Novo
│   │   ├── ThemeSettingsCard.tsx     # Novo (simples)
│   │   └── SystemInfoCard.tsx        # Novo
│   └── ...
├── hooks/
│   └── useSettings.ts            # Hook unificado para todas configs
└── store/
    └── settingsStore.ts          # Store Zustand para estado global
```

---

## 3. Plano de Execucao

### Fase 1: Infraestrutura (1-2 horas)
- [ ] Criar estrutura de diretorio `components/settings/`
- [ ] Criar hook `useSettings.ts` que agrega todos os hooks existentes
- [ ] Criar store `settingsStore.ts` com Zustand para estado unificado
- [ ] Criar componente base `SettingsTab.tsx` com layout

### Fase 2: Migracao de Componentes (3-4 horas)
- [ ] Extrair `NexusSettingsCard.tsx` de `NexusConfigPanel.tsx`
  - Manter logica, mudar apenas layout (de Modal para Card)
  - Adicionar indicador de status visual
- [ ] Extrair `GitHubSettingsCard.tsx` de `TokenConfigModal.tsx`
  - Manter logica, mudar apenas layout
  - Adicionar rate limit info inline
- [ ] Extrair `AISettingsCard.tsx` de `AISettingsTab.tsx`
  - Simplificar para cards por provider
  - Manter validacao e salvamento

### Fase 3: Novos Componentes (1-2 horas)
- [ ] Criar `ThemeSettingsCard.tsx`
- [ ] Criar `SystemInfoCard.tsx`
- [ ] Criar `ServiceNowSettingsCard.tsx` (placeholder)

### Fase 4: Integracao (1 hora)
- [ ] Adicionar nova aba "Configuracoes" em `Index.tsx`
- [ ] Adicionar icone Settings no menu lateral
- [ ] Atualizar navegacao

### Fase 5: Retrocompatibilidade (1 hora)
- [ ] Manter modais existentes funcionando (deprecados)
- [ ] Adicionar links "Ir para Configuracoes" nos modais antigos
- [ ] Atualizar documentacao

### Fase 6: Limpeza (opcional, futuro)
- [ ] Remover modais antigos apos periodo de transicao
- [ ] Remover imports nao utilizados
- [ ] Atualizar testes

---

## 4. Design dos Cards

### Card Base (Padrao)

```tsx
<Card className="relative">
  <CardHeader className="pb-2">
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-2">
        <Icon className="h-5 w-5" />
        <CardTitle className="text-lg">Nome da Integracao</CardTitle>
      </div>
      <Badge variant={status === 'connected' ? 'success' : 'destructive'}>
        {status === 'connected' ? 'Conectado' : 'Desconectado'}
      </Badge>
    </div>
    <CardDescription>Descricao breve</CardDescription>
  </CardHeader>
  <CardContent className="space-y-4">
    {/* Campos de configuracao */}
  </CardContent>
  <CardFooter className="flex justify-between">
    <Button variant="outline" onClick={handleTest}>
      <TestTube2 className="mr-2 h-4 w-4" />
      Testar
    </Button>
    <Button onClick={handleSave}>
      <Save className="mr-2 h-4 w-4" />
      Salvar
    </Button>
  </CardFooter>
</Card>
```

### Status Badges

```tsx
// Status possiveis
type IntegrationStatus =
  | 'connected'      // Verde - Configurado e validado
  | 'disconnected'   // Vermelho - Configurado mas falhou
  | 'not_configured' // Cinza - Nao configurado
  | 'validating'     // Amarelo/Loading - Testando conexao
```

---

## 5. API Backend (Existente)

Nenhuma mudanca necessaria no backend - todos os endpoints ja existem:

```
AI Tokens:
  POST /api/v1/ai/tokens/save
  GET  /api/v1/ai/tokens
  DELETE /api/v1/ai/tokens

GitHub:
  POST /api/v1/github/token/save
  GET  /api/v1/github/token/status
  DELETE /api/v1/github/token

Nexus:
  POST /nexus/config
  GET  /nexus/config
  DELETE /nexus/config
  POST /nexus/test
```

---

## 6. Mockup Visual

```
┌─────────────────────────────────────────────────────────────────────────────┐
│ Configuracoes                                                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│ INTEGRACOES EXTERNAS                                                        │
│ ┌─────────────────────────────────┐ ┌─────────────────────────────────┐    │
│ │ Nexus Repository         [OK]  │ │ GitHub                    [OK]  │    │
│ │ ─────────────────────────────  │ │ ─────────────────────────────   │    │
│ │ URL: https://nexus.vv.com.br   │ │ Token: ****...****             │    │
│ │ Repository: workspace          │ │ Email: user@example.com        │    │
│ │ Usuario: user.name             │ │ Rate Limit: 4985/5000          │    │
│ │                                │ │                                 │    │
│ │ [Testar]           [Salvar]    │ │ [Testar]           [Salvar]    │    │
│ └─────────────────────────────────┘ └─────────────────────────────────┘    │
│                                                                             │
│ PROVEDORES DE IA                                                            │
│ ┌─────────────────────────────────┐ ┌─────────────────────────────────┐    │
│ │ Gemini (Google)          [OK]  │ │ OpenAI (ChatGPT)          [--]  │    │
│ │ ─────────────────────────────  │ │ ─────────────────────────────   │    │
│ │ API Key: ****...****           │ │ API Key: Nao configurado        │    │
│ │ Modelo: gemini-1.5-pro         │ │ Modelo: --                      │    │
│ │                                │ │                                 │    │
│ │ [Validar]          [Salvar]    │ │ [Validar]          [Salvar]    │    │
│ └─────────────────────────────────┘ └─────────────────────────────────┘    │
│                                                                             │
│ ┌─────────────────────────────────┐ ┌─────────────────────────────────┐    │
│ │ Claude (Anthropic)       [OK]  │ │ Ollama (Local)            [OK]  │    │
│ │ ─────────────────────────────  │ │ ─────────────────────────────   │    │
│ │ API Key: ****...****           │ │ Modelo: llama3.2:3b             │    │
│ │ Modelo: claude-3-opus          │ │ Status: Servidor rodando        │    │
│ │                                │ │                                 │    │
│ │ [Validar]          [Salvar]    │ │ [Detectar Modelos]  [Salvar]   │    │
│ └─────────────────────────────────┘ └─────────────────────────────────┘    │
│                                                                             │
│ PREFERENCIAS                                                                │
│ ┌─────────────────────────────────┐ ┌─────────────────────────────────┐    │
│ │ Tema                           │ │ IA Preferida                    │    │
│ │ ─────────────────────────────  │ │ ─────────────────────────────   │    │
│ │ ( ) Claro  (●) Escuro  ( ) Auto│ │ [Gemini          v]             │    │
│ │                                │ │ Usado como padrao em analises   │    │
│ └─────────────────────────────────┘ └─────────────────────────────────┘    │
│                                                                             │
│ INFORMACOES DO SISTEMA                                                      │
│ ┌───────────────────────────────────────────────────────────────────────┐  │
│ │ Versao: v1.3.12 | Usuario: paulo.ribeiro@viavarejo.com.br            │  │
│ │ Grupos: VV_CLOUD_SRE | Ultima sync: 22/01/2026 16:45                 │  │
│ └───────────────────────────────────────────────────────────────────────┘  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 7. Estimativa de Tempo

| Fase | Descricao | Tempo Estimado |
|------|-----------|----------------|
| 1 | Infraestrutura | 1-2 horas |
| 2 | Migracao de Componentes | 3-4 horas |
| 3 | Novos Componentes | 1-2 horas |
| 4 | Integracao | 1 hora |
| 5 | Retrocompatibilidade | 1 hora |
| **Total** | | **7-10 horas** |

---

## 8. Riscos e Mitigacoes

| Risco | Mitigacao |
|-------|-----------|
| Quebrar funcionalidade existente | Manter modais antigos funcionando em paralelo |
| Perder configuracoes do usuario | Nao modificar backend, apenas frontend |
| Complexidade de migracao | Extrair componentes sem modificar logica |
| Estado inconsistente | Usar store Zustand centralizada |

---

## 9. Criterios de Aceite

- [ ] Nova aba "Configuracoes" visivel no menu
- [ ] Todas as integracoes configuráveis em um so lugar
- [ ] Status visual de cada integracao (conectado/desconectado)
- [ ] Validacao funcionando para cada integracao
- [ ] Salvamento funcionando para cada integracao
- [ ] Modais antigos redirecionam para nova aba (opcional)
- [ ] Sem regressao nas funcionalidades existentes

---

## 10. Proximos Passos

1. **Revisar plano** com o usuario
2. **Decidir** se implementar tudo de uma vez ou em sprints
3. **Iniciar pela Fase 1** (infraestrutura)
4. **Testar incrementalmente** cada componente migrado

---

**Autor**: Claude Code
**Versao do Plano**: 1.0
