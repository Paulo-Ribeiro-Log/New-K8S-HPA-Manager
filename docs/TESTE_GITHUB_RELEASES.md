# Plano de Testes - GitHub Releases Compare

**Data**: 11/01/2026
**Versão**: MVP + Fase 4 (Filtro Inteligente)

---

## 🎯 Funcionalidades Implementadas

### ✅ Fase 1: Backend + Base de Conhecimento
- [x] Endpoints REST API (7 endpoints)
- [x] Deployment Registry (SQLite)
- [x] Integração com Health Checking
- [x] Estrutura hierárquica (clusters → namespaces → deployments)

### ✅ Fase 2: Sistema de Token Individual
- [x] Token storage com AES-256-GCM
- [x] Endpoints de token (status, save, delete)
- [x] RBAC integration (user_email)
- [x] Cadeia de fallback (user → global → unauthenticated)

### ✅ Fase 3: Frontend Básico
- [x] GitHubReleasesTab (SplitView)
- [x] Campo de busca por deployment
- [x] Token Config Modal
- [x] Comparação de releases
- [x] 3 tabs: Commits, Arquivos, Release Notes

### ✅ Fase 4: Filtro Inteligente
- [x] Toggle "Mostrar Todos" / "Apenas Infra"
- [x] Filtro automático (.yaml, .yml, Dockerfile)
- [x] Badges de contagem
- [x] Alerts informativos

---

## 🧪 Plano de Testes

### **1. Inicialização do Servidor** ✅

```bash
# 1.1. Iniciar servidor web
cd /home/paulo/Scripts/Scripts\ GO/New-K8s-HPA-Manager/Scale_HPA
./build/new-k8s-hpa web

# Verificar logs de inicialização:
# ✅ "🔑 Inicializando GitHub Tokens Store..."
# ✅ "✅ GitHub Tokens Store inicializado (AES-256-GCM)"
# ✅ "✅ GitHub Releases Handler inicializado"
# ✅ "✅ GitHub Tokens routes registradas"
```

**Critérios de Sucesso:**
- Servidor inicia sem erros
- Token store inicializado
- Rotas registradas corretamente

---

### **2. Acesso à Interface Web** ✅

```
# 2.1. Abrir navegador
http://localhost:8080

# 2.2. Navegar até aba "GitHub Releases"
# Localização: Menu principal → 11ª tab (após "AI Diagnostics")
# Ícone: GitCompareArrows (duas setas em círculo)
```

**Critérios de Sucesso:**
- Tab "GitHub Releases" aparece no menu
- Ícone correto exibido
- SplitView renderiza (painel esquerdo + direito)

---

### **3. Configuração de Token GitHub** 🔐

```
# 3.1. Clicar em botão "Token" (header do painel esquerdo)
# 3.2. Verificar modal de configuração

# TESTE SEM TOKEN (primeira vez):
# - Status: ⚪ "Token Não Configurado"
# - Alert cinza com mensagem informativa
# - Rate limit: 60 req/h (unauthenticated)

# 3.3. Obter token GitHub:
# - Acessar: https://github.com/settings/tokens
# - Generate new token (classic)
# - Escopo: repo (Full control of private repositories)
# - Copiar token gerado

# 3.4. Salvar token no modal:
# - Colar token no input
# - Clicar em "Salvar Token"
# - Aguardar validação com GitHub API

# TESTE COM TOKEN VÁLIDO:
# - Status: 🟢 "Token Válido"
# - Display: username, email, rate limit (5000 req/h)
# - Alert verde com detalhes
```

**Critérios de Sucesso:**
- Modal abre corretamente
- Token é validado antes de salvar
- Status atualiza em tempo real
- Username e rate limit exibidos
- Token criptografado no SQLite (~/.k8s-hpa-manager/github-tokens.db)

**Teste de Erro:**
```
# 3.5. Testar token inválido:
# - Input: ghp_invalid_token_123
# - Clicar em "Salvar Token"
# - Verificar mensagem de erro

# Resultado esperado:
# - Toast de erro: "Token inválido"
# - Modal não fecha
# - Token NÃO salvo no banco
```

---

### **4. Busca por Deployment** 🔍

**Pré-requisito**: Health Checking deve ter rodado pelo menos 1 vez para popular o `deployment-registry.db`

```
# 4.1. Testar busca com mínimo de caracteres
# Input: "ab" (menos de 3 chars)
# Resultado: Nenhuma busca executada

# 4.2. Testar busca válida
# Input: "faturamento" (exemplo)
# Resultado esperado:
# - Loading spinner aparece
# - Cards de deployment exibidos
# - Informações: cluster, namespace, versão, status
# - Badge colorido de status (healthy=verde, critical=vermelho)

# 4.3. Clicar em resultado
# Resultado esperado:
# - Repositório GitHub correspondente é auto-selecionado
# - Campo de busca limpo
# - Select de repositório preenchido
# - Toast de sucesso: "Repositório selecionado: owner/repo"

# 4.4. Testar busca sem resultados
# Input: "deployment-inexistente-xyz"
# Resultado esperado:
# - Alert informativo: "Nenhum deployment encontrado"
```

**Critérios de Sucesso:**
- Busca só executa com 3+ caracteres
- Resultados renderizados corretamente
- Clique auto-seleciona repositório
- Mensagens de erro/sucesso apropriadas

---

### **5. Seleção de Repositório e Releases** 📦

```
# 5.1. Abrir select "Repositório GitHub"
# Resultado esperado:
# - Lista carregada do github-repos.yaml
# - Formato: "owner/repo" (ex: viavarejo-internal/vv-retira-geolocalizacao)

# 5.2. Selecionar um repositório
# Resultado esperado:
# - Select de "Base Tag" habilitado
# - Select de "Head Tag" habilitado
# - Releases carregados da GitHub API
# - Drafts filtrados automaticamente
# - Loading spinners durante fetch

# 5.3. Verificar releases no select
# Resultado esperado:
# - Tags ordenadas por data (mais recente primeiro)
# - Formato: "v1.2.3" ou "1.2.3-4"
# - Pre-releases marcados com "(pre-release)"
```

**Critérios de Sucesso:**
- Repositórios carregados do config
- Releases carregados da GitHub API
- Drafts excluídos da lista
- Pre-releases marcados

---

### **6. Auto-Detecção de Versão em Produção** 🎯

```
# 6.1. Clicar em "Auto-detectar" (abaixo do select "Base Tag")
# Resultado esperado:
# - Busca deployment no registry SQLite
# - Se encontrado:
#   - Base tag preenchido automaticamente
#   - Toast sucesso: "Versão em produção detectada: vX.Y.Z"
#   - Descrição: "Cluster: xxx | Namespace: yyy"
# - Se NÃO encontrado:
#   - Toast erro: "Não foi possível detectar versão em produção"

# NOTA: Depende do Health Checking ter populado o registry
```

**Critérios de Sucesso:**
- Detecta versão se deployment existe no registry
- Preenche base tag automaticamente
- Mensagens claras de sucesso/erro

---

### **7. Comparação de Releases** 🔀

```
# 7.1. Selecionar Base Tag (ex: v1.0.0)
# 7.2. Selecionar Head Tag (ex: v1.1.0)
# 7.3. Clicar em "Comparar Releases"

# Resultado esperado:
# - Loading spinner durante fetch
# - Painel direito exibe resultados
# - Header: "Comparação: v1.0.0 → v1.1.0"
# - Subheader: "owner/repo"
# - Badge informativo: "X commit(s) à frente | Y commit(s) atrás"
# - 3 tabs disponíveis: Commits, Arquivos, Release Notes

# 7.4. Testar comparação inválida
# Base Tag = Head Tag (ex: v1.0.0 = v1.0.0)
# Resultado esperado:
# - Toast erro: "Base e Head tags devem ser diferentes"
```

**Critérios de Sucesso:**
- Comparação executada corretamente
- Resultados carregados em < 2 segundos
- Header exibe tags corretas
- Badge com contagem de commits
- Validação de tags iguais

---

### **8. Tab Commits** 📝

```
# 8.1. Clicar na tab "Commits"
# Resultado esperado:
# - Lista de commits entre base e head
# - Cada card exibe:
#   - Mensagem (primeira linha)
#   - Autor (com ícone User)
#   - Data formatada (pt-BR: DD/MM/YYYY HH:MM:SS)
#   - SHA curto (7 caracteres)
#   - Link para GitHub (abre em nova aba)

# 8.2. Clicar em link do SHA
# Resultado esperado:
# - Nova aba abre com commit no GitHub
# - URL formato: https://github.com/owner/repo/commit/sha
```

**Critérios de Sucesso:**
- Commits listados corretamente
- Autor e data exibidos
- Link para GitHub funcional
- Layout limpo e legível

---

### **9. Tab Arquivos (COM Filtro Inteligente)** 📂

```
# 9.1. Clicar na tab "Arquivos"
# Resultado esperado (FILTRO ATIVO por padrão):
# - Header: "Apenas Infraestrutura"
# - Badge: "X de Y arquivos" (ex: 5 de 42 arquivos)
# - Botão toggle: "Mostrar Todos" (Eye icon, variant outline)
# - Alert azul informativo:
#   "Exibindo apenas arquivos de infraestrutura Kubernetes (.yaml, .yml, Dockerfile).
#    37 arquivo(s) de código da aplicação oculto(s)."
# - Lista exibe APENAS arquivos .yaml, .yml, Dockerfile

# 9.2. Verificar arquivos exibidos
# Cada card exibe:
# - Ícone FileCode
# - Nome do arquivo (truncado se muito longo)
# - Badge de status: added (azul), modified (cinza), deleted (vermelho)
# - Contadores: +X (verde) / -Y (vermelho)
# - Badge de extensão (se disponível)

# 9.3. Clicar em "Mostrar Todos"
# Resultado esperado:
# - Botão muda para variant "secondary"
# - Texto: "Apenas Infra" (EyeOff icon)
# - Header: "Todos os Arquivos"
# - Badge atualiza: "42 de 42 arquivos" (ou apenas "42 arquivos")
# - Alert desaparece
# - Lista exibe TODOS os arquivos (incluindo .java, .go, .ts, .md, etc)

# 9.4. Clicar em "Apenas Infra" (voltar ao filtro)
# Resultado esperado:
# - Volta ao estado inicial (filtro ativo)
# - Lista volta a exibir apenas .yaml, .yml, Dockerfile

# 9.5. Testar release SEM arquivos de infra
# (Ex: release que só alterou código Java)
# Resultado esperado:
# - Lista vazia
# - Alert informativo:
#   "ℹ️ Nenhum arquivo de infraestrutura
#    Esta release não contém alterações em arquivos de infraestrutura Kubernetes.
#    Clique em 'Mostrar Todos' para ver todos os X arquivo(s) alterado(s)."
```

**Critérios de Sucesso:**
- Filtro ativo por padrão (apenas .yaml, .yml, Dockerfile)
- Toggle funciona corretamente
- Badges atualizam dinamicamente
- Alert informativo quando filtro ativo
- Mensagem apropriada quando lista vazia
- Performance boa mesmo com 100+ arquivos

---

### **10. Tab Release Notes** 📄

```
# 10.1. Clicar na tab "Release Notes"
# Resultado esperado:
# - Grid 2 colunas lado a lado
# - Coluna esquerda: "Base: v1.0.0"
# - Coluna direita: "Head: v1.1.0"
# - Markdown renderizado com syntax highlighting
# - Links funcionais (abrem em nova aba)
# - Código em blocos com fundo cinza
# - Listas, headers, bold, italic renderizados

# 10.2. Testar release SEM release notes
# Resultado esperado:
# - Card exibe: "Sem release notes" (texto cinza, itálico)

# 10.3. Testar markdown complexo
# - Headings (# ## ###)
# - Code blocks (```language)
# - Links [texto](url)
# - Listas (- item, 1. item)
# - Checkboxes (- [ ] todo, - [x] done)
```

**Critérios de Sucesso:**
- Markdown renderizado corretamente
- Syntax highlighting funcional (react-markdown + remark-gfm)
- Layout lado a lado legível
- Links abrem em nova aba
- Mensagem apropriada quando vazio

---

## 🐛 Testes de Erro e Edge Cases

### **11. Rate Limit GitHub API** ⚠️

```
# Sem token: 60 requisições/hora
# Com token: 5000 requisições/hora

# Teste:
# - Fazer múltiplas comparações rapidamente
# - Verificar se rate limit é exibido no token status
# - Testar comportamento quando rate limit esgotado

# Resultado esperado:
# - Toast erro: "GitHub API rate limit exceeded"
# - Mensagem sugere configurar token (se não autenticado)
```

---

### **12. Repositório Privado sem Token** 🔒

```
# Teste:
# - Remover token (clicar em "Remover Token")
# - Tentar acessar repositório privado
# - Clicar em "Comparar Releases"

# Resultado esperado:
# - Erro 404 ou 403 da GitHub API
# - Toast erro: "Falha ao carregar releases"
# - Mensagem sugere configurar token
```

---

### **13. Cluster sem Deployment Registry** 📭

```
# Cenário: Health Checking nunca rodou (registry.db vazio)

# Teste:
# - Campo de busca: digitar "qualquer-app"
# - Resultado esperado:
#   - Loading completa
#   - Alert: "Nenhum deployment encontrado"

# Teste:
# - Clicar em "Auto-detectar"
# - Resultado esperado:
#   - Toast erro: "Não foi possível detectar versão em produção"
```

---

### **14. Arquivo github-repos.yaml Inexistente** 📄

```
# Cenário: ~/.k8s-hpa-manager/github-repos.yaml não existe

# Resultado esperado (backend):
# - Log warning: "GitHub repos config not found; returning empty list"
# - Retorna lista vazia (não erro 500)

# Resultado esperado (frontend):
# - Select de repositório vazio
# - Placeholder: "Selecione um repositório..."
# - Nenhum erro visual
```

---

### **15. Token Inválido/Expirado** 🔑

```
# Cenário: Token salvo fica inválido (revogado no GitHub)

# Teste:
# - Abrir modal de token
# - Clicar em "Status"

# Resultado esperado:
# - Status: 🔴 "Token Inválido"
# - Alert vermelho: "Token configurado mas não é válido"
# - Botão "Remover Token" habilitado
# - API usa fallback para global token ou unauthenticated
```

---

## 📊 Checklist de Testes

### Backend
- [ ] Servidor inicia sem erros
- [ ] Token store inicializado (AES-256-GCM)
- [ ] GitHub Tokens routes registradas
- [ ] Endpoints REST respondem corretamente
- [ ] Deployment registry populado pelo Health Checking
- [ ] Criptografia de token funcional

### Frontend - UI/UX
- [ ] Tab "GitHub Releases" aparece no menu
- [ ] SplitView renderiza corretamente
- [ ] Token modal abre/fecha
- [ ] Todos os selects funcionam
- [ ] Badges de contagem corretos
- [ ] Loading spinners exibidos
- [ ] Toast notifications apropriadas
- [ ] Hard refresh (Ctrl+Shift+R) após rebuild

### Frontend - Funcionalidades
- [ ] Campo de busca (min 3 chars)
- [ ] Auto-detecção de versão em produção
- [ ] Comparação de releases
- [ ] Tab Commits com links funcionais
- [ ] Tab Arquivos com filtro inteligente
- [ ] Toggle "Mostrar Todos" / "Apenas Infra"
- [ ] Tab Release Notes com markdown renderizado

### Segurança
- [ ] Token criptografado no SQLite
- [ ] Permissões 0600 no arquivo .secret
- [ ] RBAC middleware aplicado (InjectUserEmail)
- [ ] Token nunca exposto em logs/responses
- [ ] Validação de token antes de salvar

### Edge Cases
- [ ] Rate limit handling (60 vs 5000 req/h)
- [ ] Repositório privado sem token (403/404)
- [ ] Deployment registry vazio
- [ ] github-repos.yaml inexistente
- [ ] Token inválido/expirado
- [ ] Release sem arquivos de infra
- [ ] Release sem release notes
- [ ] Base tag = Head tag (validação)

---

## 🚀 Próximos Passos Após Testes

1. ✅ **Se tudo funciona**: Documentar problemas encontrados
2. ✅ **Criar commit** da Fase 3+4
3. ✅ **Atualizar CLAUDE.md** com nova funcionalidade
4. ⚪ **Decidir se continua para Fase 5** (Análise de IA)

---

## 📝 Observações

**Arquivo de Config Exemplo:**
```bash
# Verificar se existe:
ls -la ~/.k8s-hpa-manager/github-repos.yaml

# Se não existir, criar a partir do exemplo:
cp github-repos.yaml.example ~/.k8s-hpa-manager/github-repos.yaml

# Editar com seus repositórios:
nano ~/.k8s-hpa-manager/github-repos.yaml
```

**Banco de Dados:**
```bash
# Verificar tokens salvos:
sqlite3 ~/.k8s-hpa-manager/github-tokens.db "SELECT user_email, created_at FROM github_tokens;"

# Verificar deployment registry:
sqlite3 ~/.k8s-hpa-manager/deployment-registry.db "SELECT COUNT(*) FROM deployments;"
```

**Hard Refresh Obrigatório:**
```
Após qualquer rebuild do frontend:
- Linux/Windows: Ctrl+Shift+R
- macOS: Cmd+Shift+R

Isso limpa cache do browser e carrega novos assets.
```

---

**Última Atualização**: 11/01/2026
**Versão Testada**: MVP + Fase 4 (Filtro Inteligente)
