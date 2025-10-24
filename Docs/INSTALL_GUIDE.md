# 🚀 Guia de Instalação - K8s HPA Manager

## Instalação Rápida (Recomendado)

### Método 1: Script Automatizado (Clone + Build + Install)

```bash
# Download e execute o instalador
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
```

**Ou baixe e execute manualmente:**

```bash
# Download
wget https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh

# Tornar executável
chmod +x install-from-github.sh

# Executar
./install-from-github.sh
```

**O que o script faz:**
- ✅ Verifica requisitos (Go, Git, kubectl, Azure CLI)
- ✅ Clona o repositório do GitHub
- ✅ Compila a aplicação com versão automática
- ✅ Instala globalmente em `/usr/local/bin/`
- ✅ Copia scripts utilitários para `~/.k8s-hpa-manager/scripts/`
- ✅ Cria atalhos convenientes (`k8s-hpa-web`)
- ✅ Testa a instalação

---

## Requisitos

### Obrigatórios:
- **Go 1.23+** - [Download](https://golang.org/dl/)
- **Git** - Para clonar o repositório
- **kubectl** - Cliente Kubernetes
- **Acesso ao cluster** - Kubeconfig configurado em `~/.kube/config`

### Opcionais (para features específicas):
- **Azure CLI** - Para operações de Node Pools AKS
- **jq** - Para scripts de parsing JSON

### Verificar requisitos:

```bash
# Go
go version
# Esperado: go version go1.23.x ou superior

# Git
git --version

# kubectl
kubectl version --client

# Azure CLI (opcional)
az --version
```

---

## Métodos de Instalação

### Método 1: Script Automatizado (Recomendado)

✅ **Vantagens:**
- Instalação completa em um único comando
- Verifica requisitos automaticamente
- Copia scripts utilitários
- Cria atalhos convenientes
- Fácil para novos usuários

```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash
```

---

### Método 2: Instalação Manual (Controle Total)

#### 2.1 Clone o Repositório

```bash
git clone https://github.com/Paulo-Ribeiro-Log/Scale_HPA.git
cd Scale_HPA
```

#### 2.2 Build a Aplicação

```bash
# Com Make
make build

# Ou diretamente com Go
go build -o build/k8s-hpa-manager .
```

#### 2.3 Instale Globalmente

```bash
# Com script de instalação
./install.sh

# Ou manualmente
sudo cp build/k8s-hpa-manager /usr/local/bin/
sudo chmod +x /usr/local/bin/k8s-hpa-manager
```

#### 2.4 Copie Scripts Utilitários (Opcional)

```bash
mkdir -p ~/.k8s-hpa-manager/scripts
cp web-server.sh uninstall.sh backup.sh restore.sh ~/.k8s-hpa-manager/scripts/
chmod +x ~/.k8s-hpa-manager/scripts/*.sh
```

---

### Método 3: Download de Release (Quando Disponível)

```bash
# Download do binário Linux
wget https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/download/v1.1.0/k8s-hpa-manager-linux-amd64

# Tornar executável
chmod +x k8s-hpa-manager-linux-amd64

# Mover para /usr/local/bin
sudo mv k8s-hpa-manager-linux-amd64 /usr/local/bin/k8s-hpa-manager
```

---

## Configuração Inicial

### 1. Configurar Kubeconfig

```bash
# Verificar kubeconfig
kubectl config get-contexts

# Se necessário, adicionar clusters
az aks get-credentials --name CLUSTER_NAME --resource-group RG_NAME
```

### 2. Azure Login (Se usar Node Pools)

```bash
az login
```

### 3. Auto-descobrir Clusters

```bash
k8s-hpa-manager autodiscover
```

Isso irá:
- Escanear seu kubeconfig
- Extrair resource groups e subscriptions
- Gerar `~/.k8s-hpa-manager/clusters-config.json`

### 4. Testar Instalação

```bash
# TUI (Terminal Interface)
k8s-hpa-manager

# Servidor Web
k8s-hpa-manager web
# Acesse: http://localhost:8080
```

---

## Scripts Utilitários Incluídos

Após instalação via `install-from-github.sh`, os scripts ficam em:
**`~/.k8s-hpa-manager/scripts/`**

### web-server.sh

Gerencia o servidor web:

```bash
# Com atalho (se criado)
k8s-hpa-web start        # Iniciar servidor (porta 8080)
k8s-hpa-web stop         # Parar servidor
k8s-hpa-web restart      # Reiniciar servidor
k8s-hpa-web status       # Ver status
k8s-hpa-web logs         # Ver logs em tempo real

# Ou diretamente
~/.k8s-hpa-manager/scripts/web-server.sh start
~/.k8s-hpa-manager/scripts/web-server.sh 8081 start  # Porta customizada
```

### uninstall.sh

Desinstala a aplicação:

```bash
~/.k8s-hpa-manager/scripts/uninstall.sh
```

Remove:
- Binário em `/usr/local/bin/k8s-hpa-manager`
- (Opcional) Dados de sessão em `~/.k8s-hpa-manager/`

### backup.sh / restore.sh

Backup e restore do código (desenvolvimento):

```bash
# Criar backup
~/.k8s-hpa-manager/scripts/backup.sh "descrição"

# Listar backups
~/.k8s-hpa-manager/scripts/restore.sh

# Restaurar backup
~/.k8s-hpa-manager/scripts/restore.sh backup_name
```

### rebuild-web.sh

Rebuild da interface web:

```bash
~/.k8s-hpa-manager/scripts/rebuild-web.sh -b  # Build completo
```

---

## Comandos Principais

### TUI (Terminal Interface)

```bash
k8s-hpa-manager                      # Iniciar TUI
k8s-hpa-manager --debug              # Modo debug
k8s-hpa-manager --demo               # Modo demo (mostra features)
k8s-hpa-manager --kubeconfig PATH    # Kubeconfig customizado
```

### Web Interface

```bash
k8s-hpa-manager web                  # Background (porta 8080)
k8s-hpa-manager web -f               # Foreground (logs no terminal)
k8s-hpa-manager web --port 8081      # Porta customizada
```

### Utilitários

```bash
k8s-hpa-manager version              # Ver versão e verificar updates
k8s-hpa-manager autodiscover         # Auto-descobrir clusters
k8s-hpa-manager --help               # Ajuda completa
```

---

## Verificação de Instalação

### Teste Completo:

```bash
# 1. Verificar binário
which k8s-hpa-manager
# Esperado: /usr/local/bin/k8s-hpa-manager

# 2. Verificar versão
k8s-hpa-manager version
# Esperado: k8s-hpa-manager versão X.X.X

# 3. Verificar scripts
ls ~/.k8s-hpa-manager/scripts/
# Esperado: web-server.sh uninstall.sh backup.sh restore.sh

# 4. Testar TUI (modo demo)
k8s-hpa-manager --demo
# Esperado: Lista de features implementadas

# 5. Testar servidor web
k8s-hpa-web start
curl http://localhost:8080/health
# Esperado: {"status":"ok"}
k8s-hpa-web stop
```

---

## Troubleshooting

### Problema: "Go not found"

```bash
# Instalar Go
wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz

# Adicionar ao PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verificar
go version
```

### Problema: "Binary not found in PATH"

```bash
# Verificar PATH
echo $PATH | grep /usr/local/bin

# Se não estiver, adicionar
echo 'export PATH=$PATH:/usr/local/bin' >> ~/.bashrc
source ~/.bashrc

# Ou reiniciar terminal
```

### Problema: "Permission denied"

```bash
# Instalação requer sudo para /usr/local/bin
# O script pede automaticamente, mas você pode:
sudo ./install-from-github.sh
```

### Problema: "Azure CLI not found"

```bash
# Azure CLI é opcional (apenas para Node Pools)
# Instalar:
curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash

# Ou continue sem (HPAs funcionarão normalmente)
```

---

## Desinstalação

### Método 1: Script Automatizado

```bash
~/.k8s-hpa-manager/scripts/uninstall.sh
```

### Método 2: Manual

```bash
# Remover binário
sudo rm /usr/local/bin/k8s-hpa-manager
sudo rm /usr/local/bin/k8s-hpa-web  # Se criado

# Remover scripts e dados (opcional)
rm -rf ~/.k8s-hpa-manager/
```

---

## Atualização

Para atualizar para uma versão mais nova:

```bash
# Método 1: Re-executar instalador
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash

# Método 2: Manual
cd /path/to/Scale_HPA
git pull origin main
make build
sudo cp build/k8s-hpa-manager /usr/local/bin/

# Método 3: Download de release
# Ver instruções em: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases
```

---

## Suporte

- **Documentação**: Ver `CLAUDE.md` no repositório
- **Issues**: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/issues
- **Releases**: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases

---

**Happy managing!** 🚀
