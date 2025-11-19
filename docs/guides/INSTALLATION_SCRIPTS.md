# 📦 Scripts de Instalação - Guia Completo

[← Voltar ao CLAUDE.md principal](../../CLAUDE.md)

---

## 🎯 Visão Geral

Existem **dois scripts de instalação** disponíveis para o New K8s HPA Manager, cada um com propósitos diferentes:

1. **`install-from-github.sh`** - Instalação via release (recomendado para produção)
2. **`install-from-main.sh`** - Instalação via branch main (para desenvolvimento)

---

## 📋 Comparação dos Scripts

| Característica | `install-from-github.sh` | `install-from-main.sh` |
|----------------|--------------------------|------------------------|
| **Fonte** | GitHub Releases (binários pré-compilados) | Branch `main` (código-fonte) |
| **Requer Go?** | ❌ Não | ✅ Sim (Go 1.23+) |
| **Requer Git?** | ❌ Não | ✅ Sim |
| **Tempo de instalação** | ~30 segundos | ~3-5 minutos (compilação) |
| **Estabilidade** | ✅ Alta (versões testadas) | ⚠️ Média (código em desenvolvimento) |
| **Uso recomendado** | Produção, usuários finais | Desenvolvimento, testes |
| **Tamanho download** | ~50-100 MB (binário) | ~5-10 MB (código-fonte) |
| **Plataformas** | Linux amd64, macOS Intel/ARM | Linux amd64, macOS Intel/ARM |

---

## 🚀 Script 1: install-from-github.sh

### Uso Recomendado:
✅ **Produção** - Usuários finais
✅ **Servidores** - Ambientes de produção
✅ **CI/CD** - Pipelines automatizadas

### Características:
- ✅ Baixa binários pré-compilados das GitHub Releases
- ✅ Instalação rápida (~30 segundos)
- ✅ Não requer Go ou compilação
- ✅ Versões estáveis e testadas
- ✅ Versionamento semântico (v1.0.6, v1.0.7, etc.)

### Como Usar:

```bash
# Instalação direta
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash

# Download local e execução
wget https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh
chmod +x install-from-github.sh
./install-from-github.sh
```

### Fluxo de Instalação:

```
1. Detectar plataforma (OS/Arch)
   ├─ Linux amd64 → new-k8s-hpa-linux-amd64
   ├─ macOS Intel → new-k8s-hpa-darwin-amd64
   └─ macOS ARM  → new-k8s-hpa-darwin-arm64

2. Buscar última release via GitHub API
   └─ Fallback: v1.0.7 se API falhar

3. Baixar binário pré-compilado
   └─ URL: github.com/.../releases/download/{version}/new-k8s-hpa-{platform}

4. Instalar em /usr/local/bin
   └─ Requer sudo se necessário

5. Executar autodiscover (opcional)
   └─ Detecta clusters do kubeconfig
```

---

## 🛠️ Script 2: install-from-main.sh

### Uso Recomendado:
⚠️ **Desenvolvimento** - Desenvolvedores e testadores
⚠️ **Features experimentais** - Testar código mais recente
⚠️ **Debugging** - Versões com símbolos de debug

### Características:
- ✅ Clona código-fonte da branch `main`
- ✅ Compila binário localmente
- ✅ Versão mais recente do código (pode conter bugs)
- ✅ Ideal para testar features não lançadas
- ⚠️ Requer Go 1.23+ instalado
- ⚠️ Requer Git instalado

### Como Usar:

```bash
# Instalação direta
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-main.sh | bash

# Download local e execução
wget https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-main.sh
chmod +x install-from-main.sh
./install-from-main.sh
```

### Fluxo de Instalação:

```
1. Verificar requisitos
   ├─ Git instalado?
   ├─ Go 1.23+ instalado?
   ├─ kubectl (opcional)
   └─ Azure CLI (opcional)

2. Clonar repositório
   └─ git clone --depth 1 --branch main
       └─ Destino: /tmp/new-k8s-hpa-install

3. Compilar binário
   ├─ cd Scale_HPA/
   ├─ Injetar versão: dev-main-{commit}
   └─ go build -ldflags "-X main.Version=..."

4. Instalar em /usr/local/bin
   └─ Requer sudo se necessário

5. Executar autodiscover (opcional)
   └─ Detecta clusters do kubeconfig

6. Limpeza
   └─ rm -rf /tmp/new-k8s-hpa-install
```

---

## 🔧 Requisitos

### install-from-github.sh
```bash
# Obrigatórios
- curl (para download)
- Sistema: Linux amd64 ou macOS (Intel/ARM)

# Opcionais (recomendados)
- kubectl (para operações K8s)
- Azure CLI (para node pools)
```

### install-from-main.sh
```bash
# Obrigatórios
- Git
- Go 1.23+
- Sistema: Linux amd64 ou macOS (Intel/ARM)

# Opcionais (recomendados)
- kubectl (para operações K8s)
- Azure CLI (para node pools)
```

---

## 📊 Exemplos de Uso

### Cenário 1: Instalação em Servidor de Produção

```bash
# Use install-from-github.sh (versão estável)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash

# Verificar versão instalada
new-k8s-hpa version
# Saída: K8s HPA Manager v1.0.7 (build: 2025-11-15, commit: abc1234)
```

### Cenário 2: Testar Feature Nova (Desenvolvimento)

```bash
# Use install-from-main.sh (código mais recente)
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-main.sh | bash

# Verificar versão instalada
new-k8s-hpa version
# Saída: K8s HPA Manager dev-main-9f5b1a9 (build: 2025-11-19, commit: 9f5b1a9)
```

### Cenário 3: CI/CD Pipeline

```yaml
# .github/workflows/deploy.yml
- name: Install K8s HPA Manager
  run: |
    curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
    new-k8s-hpa version
```

---

## 🆘 Troubleshooting

### Erro: "Go não encontrado" (install-from-main.sh)

```bash
# Instalar Go 1.23+
wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
go version
```

### Erro: "Falha ao baixar binário" (install-from-github.sh)

```bash
# Verificar se release existe
# Acesse: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases

# Se não houver release recente, use install-from-main.sh
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-main.sh | bash
```

### Erro: "Permissão negada"

```bash
# Script precisa de sudo para instalar em /usr/local/bin
# Será solicitado automaticamente durante instalação

# Alternativa: instalar em diretório local
mkdir -p ~/bin
export PATH=$PATH:~/bin
# Editar script e mudar INSTALL_PATH="/usr/local/bin" para INSTALL_PATH="$HOME/bin"
```

---

## 📝 Versionamento

### install-from-github.sh
```bash
# Formato: v{major}.{minor}.{patch}
# Exemplos: v1.0.6, v1.0.7, v1.1.0

new-k8s-hpa version
# K8s HPA Manager v1.0.7 (build: 2025-11-15_14:30:22, commit: abc1234)
```

### install-from-main.sh
```bash
# Formato: dev-{branch}-{commit}
# Exemplo: dev-main-9f5b1a9

new-k8s-hpa version
# K8s HPA Manager dev-main-9f5b1a9 (build: 2025-11-19_10:15:33, commit: 9f5b1a9)
```

---

## 🔄 Atualização

### Atualizar de Release para Release

```bash
# Simplesmente re-execute o script
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
# O script detecta versão anterior e substitui automaticamente
```

### Atualizar de Main para Main

```bash
# Re-execute para obter código mais recente
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-main.sh | bash
# Compila versão mais recente da branch main
```

### Migrar de Main para Release (Estabilizar)

```bash
# Simplesmente use o script de release
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
# Substitui versão dev por versão estável
```

---

## 🎯 Recomendações

### Para Usuários Finais:
✅ Use **`install-from-github.sh`**
- Versões estáveis e testadas
- Instalação rápida
- Suporte oficial

### Para Desenvolvedores:
✅ Use **`install-from-main.sh`**
- Acesso a features experimentais
- Testar correções antes da release
- Contribuir com feedback

### Para CI/CD:
✅ Use **`install-from-github.sh`**
- Builds determinísticos
- Versões fixas
- Confiabilidade

---

## 📚 Links Úteis

- **Releases**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases
- **Branch Main**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/tree/main
- **Issues**: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/issues
- **Documentação**: [CLAUDE.md](../../CLAUDE.md)

---

[← Voltar ao CLAUDE.md principal](../../CLAUDE.md)
