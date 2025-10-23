# 🔄 Comportamento da Aplicação com Novas Releases

## 📋 Visão Geral

Este documento explica **exatamente** como a aplicação se comporta quando uma nova release é publicada no GitHub.

---

## 🔍 Sistema de Verificação Automática

### Quando a Verificação Acontece

A aplicação verifica automaticamente por updates nas seguintes situações:

#### 1. **Startup da Aplicação (TUI)**
```bash
k8s-hpa-manager
```

- ✅ Verifica **1x por dia** (cache de 24 horas)
- ✅ Execução **em background** (não bloqueia a UI)
- ✅ Aguarda **3 segundos** após startup para não interferir
- ✅ Notificação aparece no **StatusContainer** (canto inferior da tela)

#### 2. **Comando `version`**
```bash
k8s-hpa-manager version
```

- ✅ Verifica **sempre** (ignora cache)
- ✅ Execução **síncrona** (aguarda resultado)
- ✅ Exibe resultado **imediatamente** no terminal

#### 3. **Modo Web**
```bash
k8s-hpa-manager web
```

- ⚠️ **Não verifica** automaticamente (servidor roda em background)
- ℹ️ Para verificar, use: `k8s-hpa-manager version`

---

## 🎯 Fluxo Completo de Notificação

### Cenário: Nova Release v1.2.0 Publicada

Você está usando **v1.1.0** e publica **v1.2.0** no GitHub.

#### **Passo 1: Publicar Release no GitHub**

```bash
# Via web interface
https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/new?tag=v1.2.0

# Ou via script (se tiver token)
./create_release.sh  # (atualizar versão no script)
```

**GitHub API agora retorna:**
```json
{
  "tag_name": "v1.2.0",
  "name": "Release v1.2.0",
  "html_url": "https://github.com/.../releases/tag/v1.2.0",
  "body": "Release notes..."
}
```

---

#### **Passo 2: Usuário Inicia Aplicação**

```bash
k8s-hpa-manager
```

**O que acontece (linha do tempo):**

```
T+0s:    🚀 Aplicação inicia
         📋 TUI carrega (clusters, namespaces, etc)

T+0s:    ⚙️ checkForUpdatesAsync() inicia em goroutine separada
         └─ Verifica cache: ~/.k8s-hpa-manager/.update-check
         └─ Se passou 24h OU arquivo não existe → continua
         └─ Se < 24h → para aqui (não verifica)

T+3s:    🔍 Verificação aguarda 3s (para não atrapalhar startup)

T+3s:    📡 HTTP GET para GitHub API:
         GET https://api.github.com/repos/Paulo-Ribeiro-Log/Scale_HPA/releases/latest

T+3.5s:  📦 GitHub responde: { "tag_name": "v1.2.0", ... }

T+3.5s:  🧮 Comparação de versões:
         CurrentVersion: 1.1.0
         LatestVersion:  1.2.0
         IsNewerThan():  true ✅

T+3.5s:  💾 Marca cache: ~/.k8s-hpa-manager/.update-check (timestamp atual)

T+3.5s:  📢 NOTIFICAÇÃO aparece no StatusContainer:
```

**No TUI você vê:**

```
┌─ Status e Informações ────────────────────────────────────┐
│ [Updates] 🆕 Nova versão disponível: 1.1.0 → 1.2.0        │
│ [Updates] 📦 Download: https://github.com/.../v1.2.0      │
│ [Updates] 💡 Execute 'k8s-hpa-manager version'            │
└────────────────────────────────────────────────────────────┘
```

---

#### **Passo 3: Usuário Executa `version` para Detalhes**

```bash
k8s-hpa-manager version
```

**Output:**

```
k8s-hpa-manager versão 1.1.0

🔍 Verificando updates...
🆕 Nova versão disponível: 1.1.0 → 1.2.0
📦 Download: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.0

📝 Release Notes (preview):
   # Release v1.2.0

   ## Novidades
   - Feature X implementada
   - Bug Y corrigido
   ... (ver mais em https://github.com/.../v1.2.0)
```

---

#### **Passo 4: Usuário Decide Atualizar**

**Opção A: Re-executar Instalador (Recomendado)**

```bash
# Clone + build + install da versão mais recente
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
```

**O instalador faz:**
1. Detecta instalação existente (v1.1.0)
2. Pergunta se quer sobrescrever → Usuário confirma
3. Clona repositório
4. Faz checkout da tag v1.2.0 (última disponível)
5. Compila com `-ldflags "-X .../updater.Version=1.2.0"`
6. Sobrescreve `/usr/local/bin/k8s-hpa-manager`
7. ✅ Aplicação agora está em v1.2.0

**Opção B: Download Manual de Release**

```bash
# Download binário pré-compilado
wget https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/download/v1.2.0/k8s-hpa-manager-linux-amd64

# Instalar
chmod +x k8s-hpa-manager-linux-amd64
sudo mv k8s-hpa-manager-linux-amd64 /usr/local/bin/k8s-hpa-manager
```

**Opção C: Git Pull Manual**

```bash
cd /path/to/Scale_HPA
git pull origin main
git checkout v1.2.0
make build
sudo cp build/k8s-hpa-manager /usr/local/bin/
```

---

#### **Passo 5: Verificar Atualização**

```bash
k8s-hpa-manager version
```

**Output (após atualização):**

```
k8s-hpa-manager versão 1.2.0

🔍 Verificando updates...
✅ Você está usando a versão mais recente!
```

---

## ⚙️ Configurações e Controle

### Desabilitar Verificação Automática

```bash
# Desabilitar completamente
k8s-hpa-manager --check-updates=false

# Ou definir permanentemente (adicionar ao alias/script)
alias k8s-hpa-manager='k8s-hpa-manager --check-updates=false'
```

### Forçar Verificação (Ignorar Cache)

```bash
# Remover cache manualmente
rm ~/.k8s-hpa-manager/.update-check

# Próxima execução verificará novamente
k8s-hpa-manager
```

### Cache de Verificação

**Localização:** `~/.k8s-hpa-manager/.update-check`

**Comportamento:**
- Arquivo vazio (timestamp no mtime)
- Criado após cada verificação bem-sucedida
- Válido por **24 horas**
- Após 24h, próxima verificação acontece automaticamente

---

## 📊 Comparação de Versões

### Lógica de Comparação (Semantic Versioning)

```go
// Formato: MAJOR.MINOR.PATCH
// Exemplo: 1.2.3

// Comparação:
1. MAJOR diferente: 2.0.0 > 1.9.9 ✅
2. MAJOR igual, MINOR diferente: 1.5.0 > 1.4.9 ✅
3. MAJOR e MINOR iguais, PATCH diferente: 1.2.5 > 1.2.4 ✅
4. Todos iguais: 1.2.3 == 1.2.3 ❌ (não notifica)
```

### Sufixos e Variantes

```bash
# Versões com sufixo são normalizadas:
v1.2.3-dirty     → 1.2.3  (sufixo removido)
v1.2.3-24-gabc   → 1.2.3  (commits após tag, removido)
1.2.3            → 1.2.3  (prefixo "v" opcional)

# Versão "dev" nunca verifica updates:
dev              → Não verifica ❌
dev-abc123       → Não verifica ❌
```

---

## 🔔 Tipos de Notificação

### 1. TUI (Terminal Interface)

**Notificação no StatusContainer:**

```
┌─ Status e Informações ────────────────────────┐
│ [Updates] 🆕 Nova versão: 1.1.0 → 1.2.0       │
│ [Updates] 📦 https://github.com/.../v1.2.0    │
│ [Updates] 💡 Execute 'k8s-hpa-manager version'│
└────────────────────────────────────────────────┘
```

- ✅ Aparece **3 segundos** após startup
- ✅ Permanece visível durante toda a sessão
- ✅ Não bloqueia operações (continua usando normalmente)

### 2. Comando `version`

**Output detalhado no terminal:**

```
k8s-hpa-manager versão 1.1.0

🔍 Verificando updates...
🆕 Nova versão disponível: 1.1.0 → 1.2.0
📦 Download: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.0

📝 Release Notes (preview):
   [Primeiras 5 linhas das release notes]
   ... (ver mais no link acima)
```

### 3. Modo Web

⚠️ **Não há notificação automática no modo web.**

Para verificar manualmente:
```bash
k8s-hpa-manager version
```

---

## 🛠️ Troubleshooting

### Notificação Não Aparece

**Possíveis causas:**

1. **Cache válido (verificação recente)**
   ```bash
   # Verificar última verificação
   ls -la ~/.k8s-hpa-manager/.update-check

   # Se < 24h, remover para forçar
   rm ~/.k8s-hpa-manager/.update-check
   ```

2. **Versão "dev" (não verifica)**
   ```bash
   # Verificar versão atual
   k8s-hpa-manager version
   # Se mostrar "dev", compile com versão real:
   make build  # (usa git tags para versão)
   ```

3. **GitHub API inacessível**
   ```bash
   # Testar API manualmente
   curl https://api.github.com/repos/Paulo-Ribeiro-Log/Scale_HPA/releases/latest

   # Se retornar erro 404: release não publicada
   # Se timeout: problema de rede
   ```

4. **Release não publicada**
   ```bash
   # Verificar releases no GitHub
   curl https://api.github.com/repos/Paulo-Ribeiro-Log/Scale_HPA/releases

   # Lista vazia [] significa: apenas tags existem, sem releases
   ```

### "Você está usando a versão mais recente" (Mas Há Release Nova)

**Causa:** Binário compilado com versão errada.

```bash
# Verificar versão embutida no binário
k8s-hpa-manager version
# Output: k8s-hpa-manager versão X.X.X

# Verificar como foi compilado
strings /usr/local/bin/k8s-hpa-manager | grep -A2 "internal/updater.Version"

# Re-compilar corretamente
cd /path/to/repo
git checkout v1.1.0  # Tag correta
make build           # Injeta versão via -ldflags
```

### Erro de Rate Limiting (GitHub API)

**GitHub limita requisições anônimas:**
- **60 requests/hora** sem autenticação
- **5000 requests/hora** com token

**Solução (se necessário):**

```bash
# Criar token: https://github.com/settings/tokens/new
# Permissões: public_repo (leitura)

# Configurar token
mkdir -p ~/.k8s-hpa-manager
echo "ghp_seu_token_aqui" > ~/.k8s-hpa-manager/.github-token
chmod 600 ~/.k8s-hpa-manager/.github-token
```

---

## 📈 Fluxo de Versionamento Recomendado

### Para Mantenedores do Projeto

**1. Desenvolvimento (branch main/dev):**
```bash
git commit -m "feat: nova feature X"
# Versão: "dev" ou "1.1.0-5-gabc123"
```

**2. Preparar Release:**
```bash
# Criar tag
git tag v1.2.0
git push origin v1.2.0

# Build de release
make release  # Gera binários para múltiplas plataformas
```

**3. Publicar no GitHub:**
```bash
# Via web interface (mais fácil)
https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/new?tag=v1.2.0

# Ou via script automatizado
./create_release.sh  # Requer GITHUB_TOKEN
```

**4. Testar Notificação:**
```bash
# Build com versão antiga
git checkout v1.1.0
make build

# Verificar notificação
rm ~/.k8s-hpa-manager/.update-check  # Forçar verificação
./build/k8s-hpa-manager version

# Esperado:
# 🆕 Nova versão disponível: 1.1.0 → 1.2.0
```

---

## 🎯 Resumo do Comportamento

| Situação | Comportamento |
|----------|---------------|
| **Nova release publicada** | Notificação aparece no próximo startup (se > 24h do último check) |
| **Comando `version`** | Sempre verifica (ignora cache) e exibe resultado |
| **TUI startup** | Verifica em background após 3s (1x por dia) |
| **Modo web** | Não verifica automaticamente |
| **Cache válido** | Não verifica (aguarda 24h) |
| **Versão "dev"** | Nunca verifica |
| **Flag `--check-updates=false`** | Desabilita completamente |
| **Erro na API** | Ignora silenciosamente (não atrapalha UX) |
| **Rate limiting** | Use token em `~/.k8s-hpa-manager/.github-token` |

---

## 💡 Dicas para Usuários

### Verificar Updates Regularmente

```bash
# Adicionar ao cron (semanal)
# Toda segunda-feira às 9h
0 9 * * 1 k8s-hpa-manager version | grep -q "Nova versão" && notify-send "K8s HPA Manager" "Nova versão disponível!"
```

### Auto-Update Script

Ver: `auto-update.sh` (se criado)

```bash
# Script que verifica e atualiza automaticamente
~/.k8s-hpa-manager/scripts/auto-update.sh
```

---

**Dúvidas?** Abra uma issue: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/issues
