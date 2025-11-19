# 🪟 Suporte Windows - Limitações e Recomendações

## ⚠️ Status Atual do Suporte Windows

O binário Windows **compila corretamente**, mas tem **limitações significativas** que impedem seu uso completo.

---

## ✅ O que FUNCIONA no Windows

1. **Código Go é cross-platform**:
   - `filepath.Join()` - Caminhos Windows/Linux automáticos
   - `os.UserHomeDir()` - Detecta `C:\Users\username` automaticamente
   - SQLite - Funciona nativamente no Windows

2. **Funcionalidades básicas**:
   - Interface web (`new-k8s-hpa.exe web`)
   - Leitura de configurações em `%USERPROFILE%\.k8s-hpa-manager\`
   - Sessões salvas em `%USERPROFILE%\.k8s-hpa-manager\sessions\`

---

## ❌ O que NÃO FUNCIONA no Windows

### **1. Dependências Externas**

O aplicativo executa comandos externos que podem não existir no Windows:

```go
// internal/tui/app.go
cmd := exec.Command("kubectl", "drain", nodeName, ...)  // ❌ kubectl.exe precisa estar no PATH
cmd := exec.Command("az", "aks", "nodepool", ...)       // ❌ az.exe precisa estar no PATH
```

**Solução parcial**: Instalar Azure CLI e kubectl no Windows (disponíveis via `winget` ou Chocolatey)

---

### **2. Scripts Shell (.sh)**

Scripts de instalação/manutenção não funcionam no Windows:

```
❌ install-from-github.sh
❌ web-server.sh
❌ backup.sh / restore.sh
❌ create-release.sh
```

**Solução**: Criar versões PowerShell (.ps1) dos scripts críticos

---

### **3. Permissões de Arquivo**

```go
// internal/session/manager.go
os.MkdirAll(sessionDir, 0755)  // ⚠️ Permissões Unix ignoradas no Windows
```

Windows usa ACLs (Access Control Lists) ao invés de permissões Unix. O código funciona, mas não aplica permissões de segurança.

---

### **4. Interface TUI (Terminal)**

A interface Bubble Tea pode ter problemas no **CMD** e **PowerShell** padrão:

- ❌ Cores ANSI podem não funcionar
- ❌ Redimensionamento de terminal pode quebrar layout
- ❌ Teclas especiais (F1-F12) podem não ser detectadas

**Solução**: Usar **Windows Terminal** (moderno, suporta ANSI completo)

---

## 🎯 Cenários de Uso Recomendados

### **✅ Cenário 1: Apenas Interface Web (FUNCIONA)**

```powershell
# Download do binário
Invoke-WebRequest -Uri "https://github.com/.../new-k8s-hpa-windows-amd64.exe" -OutFile "new-k8s-hpa.exe"

# Executar servidor web
.\new-k8s-hpa.exe web

# Abrir browser
Start-Process "http://localhost:8080"
```

**Requisitos**:
- ✅ kubectl instalado (`winget install Kubernetes.kubectl`)
- ✅ Azure CLI instalado (`winget install Microsoft.AzureCLI`)
- ✅ Kubeconfig em `%USERPROFILE%\.kube\config`

**Limitações**:
- ⚠️ Apenas modo web (TUI não recomendado)
- ⚠️ Scripts de manutenção não funcionam

---

### **✅ Cenário 2: WSL2 (RECOMENDADO)**

Usar **WSL2 (Windows Subsystem for Linux)** para ambiente completo:

```powershell
# Instalar WSL2 (PowerShell como Admin)
wsl --install

# Dentro do WSL2 (Ubuntu)
curl -fsSL https://raw.githubusercontent.com/.../install-from-github.sh | bash
new-k8s-hpa
```

**Vantagens**:
- ✅ 100% funcional (ambiente Linux completo)
- ✅ TUI funciona perfeitamente
- ✅ Todos os scripts funcionam
- ✅ Performance nativa

---

### **❌ Cenário 3: Windows Nativo (NÃO RECOMENDADO)**

Executar TUI diretamente no Windows:

```powershell
.\new-k8s-hpa.exe
```

**Problemas esperados**:
- Interface quebrada no CMD/PowerShell
- Teclas F1-F12 podem não funcionar
- Cores podem não aparecer

---

## 🛠️ Instalação Completa no Windows (Via WSL2)

### **1. Instalar WSL2**

```powershell
# PowerShell como Admin
wsl --install
wsl --set-default-version 2
```

Reiniciar o computador.

---

### **2. Configurar Ubuntu no WSL2**

```bash
# Dentro do WSL2
sudo apt update
sudo apt install -y curl git build-essential

# Instalar kubectl
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# Instalar Azure CLI
curl -sL https://aka.ms/InstallAzureCLIDeb | sudo bash
```

---

### **3. Copiar Kubeconfig do Windows para WSL2**

```bash
# Dentro do WSL2
mkdir -p ~/.kube
cp /mnt/c/Users/SEU_USUARIO/.kube/config ~/.kube/config
chmod 600 ~/.kube/config
```

---

### **4. Instalar new-k8s-hpa**

```bash
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/main/install-from-github.sh | bash
```

---

### **5. Executar**

```bash
# TUI
new-k8s-hpa

# Ou interface web
new-k8s-hpa web
```

Abrir browser Windows em: `http://localhost:8080`

---

## 📊 Comparação de Métodos no Windows

| Método | TUI | Web | Scripts | Performance | Recomendado |
|--------|-----|-----|---------|-------------|-------------|
| **Windows Nativo** | ❌ Quebrado | ✅ Funciona | ❌ Não | Médio | ❌ Não |
| **WSL2** | ✅ Perfeito | ✅ Perfeito | ✅ Sim | Nativo | ✅ **SIM** |
| **Docker Desktop** | ❌ Não | ✅ Funciona | ❌ Não | Médio | ⚠️ Alternativa |

---

## 🐳 Alternativa: Docker (Futuro)

Para simplificar instalação no Windows, criar imagem Docker:

```dockerfile
# Dockerfile (exemplo futuro)
FROM golang:1.23-alpine
WORKDIR /app
COPY . .
RUN go build -o new-k8s-hpa
EXPOSE 8080
CMD ["./new-k8s-hpa", "web"]
```

```powershell
# Windows
docker run -p 8080:8080 -v ${HOME}/.kube:/root/.kube new-k8s-hpa
```

**Status**: 🔜 Não implementado ainda

---

## 🎯 Recomendações Finais

### **Para Usuários Windows:**

1. ✅ **Use WSL2** - Experiência completa e nativa
2. ⚠️ **Apenas Web no Windows nativo** - Limitado mas funcional
3. ❌ **Evite TUI no Windows nativo** - Interface quebrada

### **Para Desenvolvedores:**

1. **Não remover binário Windows** - Web funciona
2. **Documentar limitações claramente** - Este arquivo
3. **Priorizar WSL2 nas instruções** - Melhor experiência
4. **Futuro**: Criar scripts PowerShell para paridade

---

## 📝 Atualização do Template de Release Notes

Adicionar seção de instalação Windows:

```markdown
### Windows (⚠️ Limitações - Leia WINDOWS_SUPPORT.md)

**Recomendado: Use WSL2**
```bash
# Dentro do WSL2
curl -L https://github.com/.../new-k8s-hpa-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

**Windows Nativo (Apenas Web)**
```powershell
# Download
Invoke-WebRequest -Uri "https://github.com/.../new-k8s-hpa-windows-amd64.exe" -OutFile "new-k8s-hpa.exe"

# Executar (apenas modo web)
.\new-k8s-hpa.exe web
```

⚠️ **TUI não funciona no Windows nativo** - Use WSL2 para funcionalidade completa.
```

---

## ✅ Conclusão

**Windows é suportado**, mas com ressalvas:

- ✅ **Código compila** e binário funciona
- ✅ **Interface web** funciona perfeitamente
- ⚠️ **TUI quebrado** no CMD/PowerShell nativo
- ✅ **WSL2 = 100% funcional** (recomendado)

**Decisão**: Manter binário Windows na release, mas **documentar limitações** e **recomendar WSL2**.
