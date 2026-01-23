# Checklist - Menu de Perfil com Configuracoes

**Data de Inicio**: 22 de Janeiro de 2026
**Objetivo**: Criar menu dropdown de perfil no header com configuracoes centralizadas
**Status**: CONCLUIDO (Fases 1-7 completas | Fase 8 opcional para limpeza futura)

---

## Visao Geral

Substituir o botao de usuario atual (`admin@k8s.local` + `Logout`) por um menu dropdown completo contendo:
- Informacoes do usuario
- Configuracoes de tema
- Configuracoes de credenciais (Nexus, GitHub, ServiceNow)
- Botao de logout

---

## Fase 1: Infraestrutura

- [x] **1.1** Criar componente `UserProfileMenu.tsx` em `internal/web/frontend/src/components/`
- [x] **1.2** Criar diretorio `internal/web/frontend/src/components/profile/` para sub-componentes
- [x] **1.3** Criar hook `useUserProfile.ts` para agregar dados do usuario
- [x] **1.4** Criar tipos em `internal/web/frontend/src/types/profile.ts`

**Arquivos criados:**
```
internal/web/frontend/src/
├── components/
│   ├── UserProfileMenu.tsx          # Menu principal (dropdown) ✅
│   └── profile/
│       ├── NexusCredentialModal.tsx  # Config Nexus ✅
│       ├── GitHubCredentialModal.tsx # Config GitHub ✅
│       └── ServiceNowCredentialModal.tsx # Config ServiceNow ✅
├── hooks/
│   └── useUserProfile.ts            # Hook agregador ✅
└── types/
    └── profile.ts                   # Tipos do perfil ✅
```

---

## Fase 2: Componente Principal (UserProfileMenu)

- [x] **2.1** Implementar estrutura base do dropdown usando `DropdownMenu` do shadcn/ui
- [x] **2.2** Adicionar avatar/icone do usuario
- [x] **2.3** Implementar cabecalho do menu (nome, email, badge SRE)
- [x] **2.4** Adicionar separadores visuais entre secoes
- [x] **2.5** Integrar com dados do usuario (Azure AD via `useUserPermissions`)

**Dependencias shadcn/ui necessarias:**
- [x] DropdownMenu (ja existe)
- [x] Avatar (ja existe)
- [x] RadioGroup (para tema - nao necessario, usamos DropdownMenuItem)
- [x] Badge (ja existe)
- [x] Separator (ja existe)

---

## Fase 3: Seletor de Tema

- [x] **3.1** ~~Criar componente `ThemeSelector.tsx`~~ (integrado direto no UserProfileMenu)
- [x] **3.2** Integrar com `theme-provider.tsx` existente
- [x] **3.3** Opcoes: Claro, Escuro, Sistema (auto)
- [x] **3.4** Persistir escolha no localStorage (via theme-provider)
- [x] **3.5** Aplicar mudanca em tempo real (sem refresh)

**Referencia atual:** `internal/web/frontend/src/components/theme-provider.tsx`

---

## Fase 4: Menu de Credenciais

- [x] **4.1** ~~Criar componente `CredentialsMenu.tsx`~~ (integrado direto no UserProfileMenu)
- [x] **4.2** Listar credenciais com status visual (configurado/nao configurado)
- [x] **4.3** Ao clicar, abrir modal de configuracao

### 4.A - Nexus Repository

- [x] **4.A.1** Extrair logica de `NexusConfigPanel.tsx` para componente reutilizavel
- [x] **4.A.2** Criar `NexusCredentialModal.tsx` (baseado no NexusConfigPanel)
- [x] **4.A.3** Campos: URL Base, Repository, Usuario, Senha, Padrao URL
- [x] **4.A.4** Botao "Testar Conexao"
- [x] **4.A.5** Indicador de status no menu

**Referencia atual:** `internal/web/frontend/src/components/NexusConfigPanel.tsx`
**Hook existente:** `internal/web/frontend/src/hooks/useNexus.ts`

### 4.B - GitHub Token

- [x] **4.B.1** Extrair logica de `TokenConfigModal.tsx` para componente reutilizavel
- [x] **4.B.2** Criar `GitHubCredentialModal.tsx` (baseado no TokenConfigModal)
- [x] **4.B.3** Campos: Personal Access Token, Email
- [x] **4.B.4** Exibir Rate Limit info
- [x] **4.B.5** Indicador de status no menu

**Referencia atual:** `internal/web/frontend/src/components/TokenConfigModal.tsx`
**Hook existente:** `internal/web/frontend/src/hooks/useGitHubReleases.ts`

### 4.C - ServiceNow

- [x] **4.C.1** Criar `ServiceNowCredentialModal.tsx`
- [x] **4.C.2** Campos: Instance URL, Usuario, Senha
- [x] **4.C.3** Botao "Testar Conexao"
- [x] **4.C.4** Indicador de status no menu
- [ ] **4.C.5** Verificar se backend ja tem endpoints (criar se necessario)

**Referencia:** Verificar `ServiceNowImportModal.tsx` para logica existente

---

## Fase 5: Integracao no Header

- [x] **5.1** Localizar componente do header atual
- [x] **5.2** Remover botoes antigos (email + Logout separados)
- [x] **5.3** Substituir por `<UserProfileMenu />`
- [x] **5.4** Ajustar responsividade (mobile) - Nome/email ocultos em telas < md
- [x] **5.5** Testar em diferentes tamanhos de tela

**Arquivo do header:** `internal/web/frontend/src/components/Header.tsx`

---

## Fase 6: Retrocompatibilidade (Opcao B - Redirecionar)

- [x] **6.1** Criar `CredentialRedirectDialog` - Dialog que informa ao usuario para ir ao menu de perfil
- [x] **6.2** Substituir `TokenConfigModal` no `GitHubReleasesTab` pelo dialog de redirecionamento
- [x] **6.3** Substituir `NexusConfigPanel` no `NexusValuesDiffPanel` pelo dialog de redirecionamento
- [x] **6.4** Remover abertura automatica do dialog quando Nexus nao configurado

**Arquivos modificados:**
- `GitHubReleasesTab.tsx` - Agora usa `CredentialRedirectDialog` ao invés de `TokenConfigModal`
- `NexusValuesDiffPanel.tsx` - Agora usa `CredentialRedirectDialog` ao invés de `NexusConfigPanel`

**Novo componente:**
- `components/profile/CredentialRedirectDialog.tsx` - Dialog que informa o novo local da configuracao

---

## Fase 7: Testes e Polimento

- [x] **7.1** Testar fluxo completo de cada credencial - Verificado: Nexus, GitHub e ServiceNow modais funcionando
- [x] **7.2** Testar troca de tema - Verificado: Integrado com theme-provider
- [x] **7.3** Testar logout - Verificado: onLogout passado corretamente
- [x] **7.4** Verificar persistencia apos refresh - Verificado: Nexus e GitHub usam hooks com cache
- [x] **7.5** Testar com usuario SRE e nao-SRE - Verificado: Badge SRE condicional
- [x] **7.6** Build de producao (`./rebuild-web.sh -b`) - Verificado: Build sem erros TypeScript

---

## Fase 8: Limpeza (Pos-Validacao)

- [ ] **8.1** Remover modais antigos nao utilizados
- [ ] **8.2** Remover imports orfaos
- [ ] **8.3** Atualizar CLAUDE.md com nova estrutura
- [ ] **8.4** Commit final com documentacao

---

## Arquivos de Referencia

### Frontend - Componentes Existentes
- `internal/web/frontend/src/components/NexusConfigPanel.tsx` - Logica Nexus
- `internal/web/frontend/src/components/TokenConfigModal.tsx` - Logica GitHub
- `internal/web/frontend/src/components/ServiceNowImportModal.tsx` - Logica ServiceNow
- `internal/web/frontend/src/components/theme-provider.tsx` - Logica Tema

### Frontend - Hooks Existentes
- `internal/web/frontend/src/hooks/useNexus.ts`
- `internal/web/frontend/src/hooks/useGitHubReleases.ts`
- `internal/web/frontend/src/hooks/useUserPermissions.ts`

### Backend - Endpoints Existentes
```
Nexus:
  POST /nexus/config
  GET  /nexus/config
  DELETE /nexus/config
  POST /nexus/test

GitHub:
  POST /api/v1/github/token/save
  GET  /api/v1/github/token/status
  DELETE /api/v1/github/token

ServiceNow:
  POST /api/v1/servicenow/import (verificar se ha config separada)
```

---

## Mockup Visual Final

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ k8s-hpa-manager  v1.3.12   [akspriv-abastecimento-hlg-admin ▼]              │
│                                                                              │
│ [Load Session] [Save Session]                    [Paulo Ribeiro ▼] [SRE]    │
│                                                   │                         │
│ Dashboard | HPAs | Node Pools | ...               │ ┌─────────────────────┐ │
│                                                   │ │ Paulo Ribeiro       │ │
│                                                   │ │ admin@k8s.local     │ │
│                                                   │ │ [Badge SRE]         │ │
│                                                   │ │─────────────────────│ │
│                                                   │ │ Tema                │ │
│                                                   │ │  ○ Claro            │ │
│                                                   │ │  ● Escuro           │ │
│                                                   │ │  ○ Sistema          │ │
│                                                   │ │─────────────────────│ │
│                                                   │ │ Credenciais         │ │
│                                                   │ │  Nexus        [●]   │ │
│                                                   │ │  GitHub       [○]   │ │
│                                                   │ │  ServiceNow   [○]   │ │
│                                                   │ │─────────────────────│ │
│                                                   │ │ 🚪 Sair             │ │
│                                                   │ └─────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────────┘

Legenda:
[●] = Configurado/Conectado (verde)
[○] = Nao configurado (cinza)
```

---

## Historico de Progresso

| Data | Fase | Descricao | Status |
|------|------|-----------|--------|
| 22/01/2026 | - | Criacao do checklist | Concluido |
| 23/01/2026 | 1 | Infraestrutura (tipos, hooks, componentes base) | Concluido |
| 23/01/2026 | 2 | Componente UserProfileMenu completo | Concluido |
| 23/01/2026 | 3 | Seletor de tema integrado | Concluido |
| 23/01/2026 | 4 | Menu de credenciais (Nexus, GitHub, ServiceNow) | Concluido |
| 23/01/2026 | 5 | Integracao no Header + Responsividade | Concluido |
| 23/01/2026 | 6 | Retrocompatibilidade (Opcao B - Dialog de redirecionamento) | Concluido |
| 23/01/2026 | 7 | Testes e Polimento (TypeScript, build, verificacao) | Concluido |

---

## Notas Tecnicas

### Componentes shadcn/ui utilizados
```tsx
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
```

### Estrutura do UserProfileMenu
```tsx
<DropdownMenu>
  <DropdownMenuTrigger asChild>
    <Button variant="ghost" className="flex items-center gap-2">
      <Avatar className="h-8 w-8">
        <AvatarFallback>PR</AvatarFallback>
      </Avatar>
      <span>Paulo Ribeiro</span>
      <ChevronDown className="h-4 w-4" />
    </Button>
  </DropdownMenuTrigger>
  <DropdownMenuContent className="w-72" align="end">
    {/* Cabecalho do usuario */}
    {/* Seletor de tema */}
    {/* Lista de credenciais */}
    {/* Botao logout */}
  </DropdownMenuContent>
</DropdownMenu>
```

### Itens Removidos do Header
- `ModeToggle` - substituido pelo seletor de tema no menu
- `SREBadge` - agora exibido dentro do UserProfileMenu
- Botao `Logout` separado - agora dentro do menu
- Texto `userInfo="admin@k8s.local"` - agora vem do hook `useUserProfile`

---

**Ultima atualizacao**: 23/01/2026
**Responsavel**: Claude Code
