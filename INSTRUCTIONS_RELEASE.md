# 📦 Como Criar Releases com Binários Pré-Compilados

Este guia mostra como publicar releases no GitHub com binários pré-compilados para que usuários possam instalar **sem precisar de Go** na máquina.

---

## 🎯 Objetivo

Criar releases no GitHub que permitem instalação direta via:

```bash
# Download direto do binário (sem compilar)
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.5/new-k8s-hpa-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

---

## 📋 Pré-requisitos

1. **GitHub Token** configurado (ver `GITHUB_TOKEN_SETUP.md`)
2. **Go instalado** (para compilar os binários)
3. **jq instalado** (para processar JSON)
4. **curl instalado** (para upload dos binários)

---

## 🚀 Passo a Passo Completo

### **1. Criar Release Notes**

Crie um arquivo `RELEASE_NOTES_v1.0.5.md` (substitua `1.0.5` pela sua versão):

```markdown
# Release 1.0.5 - Cordon/Drain para Node Pools

## 🎉 Novidades

- ✅ Sistema de Cordon/Drain para Node Pools
- ✅ Validação automática de pods antes de drain
- ✅ Timeout configurável para drain
- ✅ Feedback visual durante operação

## 🐛 Correções

- ✅ Corrigido bug em aplicação de node pools com autoscaling
- ✅ Melhorado tratamento de erros em operações Azure

## 📦 Instalação

### Linux (amd64)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.5/new-k8s-hpa-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### macOS (Intel)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.5/new-k8s-hpa-darwin-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### macOS (Apple Silicon M1/M2)
```bash
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.5/new-k8s-hpa-darwin-arm64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

### Windows
⚠️ **Windows não é suportado via binários pré-compilados.**

**Use WSL2** para funcionalidade completa:
```bash
# Dentro do WSL2 (Ubuntu)
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.5/new-k8s-hpa-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
```

📖 Ver `WINDOWS_SUPPORT.md` para detalhes sobre WSL2

## 🔗 Links

- [Documentação completa](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager)
- [Reportar bugs](https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/issues)
```

---

### **2. Commit e Push de Mudanças**

```bash
# Certifique-se de que todas as mudanças estão commitadas
git add .
git commit -m "feat: add cordon/drain system for node pools"
git push origin main
```

---

### **3. Criar Tag Git**

```bash
# Criar tag anotada
git tag -a v1.0.5 -m "Release 1.0.5 - Cordon/Drain para Node Pools"

# Push da tag para GitHub
git push origin v1.0.5
```

**💡 Dica:** Use versionamento semântico (MAJOR.MINOR.PATCH):
- **MAJOR**: Mudanças incompatíveis com versões anteriores
- **MINOR**: Novas funcionalidades compatíveis
- **PATCH**: Correções de bugs

---

### **4. Compilar Binários para Todas as Plataformas**

```bash
# Compila para Linux, macOS (Intel/ARM) e Windows
make release
```

Isso vai criar os binários em `build/release/`:
```
build/release/
├── new-k8s-hpa-linux-amd64         # Linux (82 MB)
├── new-k8s-hpa-darwin-amd64        # macOS Intel (82 MB)
├── new-k8s-hpa-darwin-arm64        # macOS Apple Silicon (80 MB)
└── new-k8s-hpa-windows-amd64.exe   # Windows (82 MB)
```

---

### **5. Criar Release no GitHub**

```bash
# Opção 1: Versão detectada automaticamente da tag git
./create-release.sh

# Opção 2: Especificar versão manualmente
./create-release.sh 1.0.5
```

**O que o script faz:**
1. ✅ Verifica se o token GitHub está configurado
2. ✅ Lê o arquivo `RELEASE_NOTES_v1.0.5.md`
3. ✅ Cria a release no GitHub
4. ✅ Faz upload dos 4 binários compilados
5. ✅ Exibe URL da release criada

**Output esperado:**
```
════════════════════════════════════════════════════════════
  Criando Release no GitHub
════════════════════════════════════════════════════════════

📦 Versão: 1.0.5
🏷️  Tag: v1.0.5
📝 Release name: 1.0.5 - Cordon/Drain para Node Pools
📄 Release notes: RELEASE_NOTES_v1.0.5.md

Criar release v1.0.5 no GitHub? (S/n): s

🚀 Criando release v1.0.5 no GitHub...
✅ Release criado (ID: 123456789)

📤 Fazendo upload dos binários...
  • Uploading new-k8s-hpa-linux-amd64...
    ✅ new-k8s-hpa-linux-amd64 uploaded
  • Uploading new-k8s-hpa-darwin-amd64...
    ✅ new-k8s-hpa-darwin-amd64 uploaded
  • Uploading new-k8s-hpa-darwin-arm64...
    ✅ new-k8s-hpa-darwin-arm64 uploaded
  • Uploading new-k8s-hpa-windows-amd64.exe...
    ✅ new-k8s-hpa-windows-amd64.exe uploaded

════════════════════════════════════════════════════════════
  ✅ Release publicada com sucesso!
════════════════════════════════════════════════════════════

🔗 URL: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/tag/v1.0.5

📋 Próximos passos:
  1. Verificar release: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases
  2. Testar instalação: curl ... | bash
  3. Anunciar nova versão
```

---

### **6. Verificar Release no GitHub**

Acesse: https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases

Você deve ver:
- ✅ Release v1.0.5 publicada
- ✅ 4 binários disponíveis para download
- ✅ Release notes formatadas
- ✅ Instruções de instalação

---

### **7. Testar Instalação**

**Teste em uma máquina limpa (sem Go):**

```bash
# Linux
curl -L https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/download/v1.0.5/new-k8s-hpa-linux-amd64 -o new-k8s-hpa
chmod +x new-k8s-hpa
sudo mv new-k8s-hpa /usr/local/bin/
new-k8s-hpa version
```

Deve exibir:
```
new-k8s-hpa versão 1.0.5
```

---

## 🔄 Workflow Completo Resumido

```bash
# 1. Criar release notes
nano RELEASE_NOTES_v1.0.5.md

# 2. Commit e tag
git add .
git commit -m "feat: nova funcionalidade"
git tag -a v1.0.5 -m "Release 1.0.5"
git push origin main
git push origin v1.0.5

# 3. Compilar binários
make release

# 4. Criar release no GitHub
./create-release.sh

# 5. Verificar e testar
# https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases
```

---

## 📊 Comparação: Install Script vs Release Binaries

| Método | Pré-requisitos | Tempo de Instalação | Tamanho Download | Vantagens |
|--------|----------------|---------------------|------------------|-----------|
| **install-from-github.sh** | Go, Git, make | ~2 minutos | ~200 MB | Sempre a versão mais recente da main |
| **Release Binary** | Nenhum | ~10 segundos | ~82 MB | Instalação rápida, sem dependências |

---

## 🎯 Quando Usar Cada Método

### **Use install-from-github.sh quando:**
- ✅ Desenvolvimento ativo
- ✅ Quer a versão mais recente da main
- ✅ Tem Go instalado
- ✅ Precisa compilar com modificações locais

### **Use Release Binary quando:**
- ✅ Produção
- ✅ Quer versão estável e testada
- ✅ Instalação rápida sem dependências
- ✅ Distribuição para equipe (sem Go)

---

## 🐛 Troubleshooting

### **Erro: "GITHUB_TOKEN não encontrado"**
```bash
./setup-github-token.sh
```

Ver: `GITHUB_TOKEN_SETUP.md`

---

### **Erro: "Binários ausentes"**
```bash
make release
```

Certifique-se de que Go está instalado e configurado.

---

### **Erro: "tag already exists"**
```bash
# Deletar tag local
git tag -d v1.0.5

# Deletar tag remota
git push origin :refs/tags/v1.0.5

# Recriar tag
git tag -a v1.0.5 -m "Release 1.0.5"
git push origin v1.0.5
```

---

### **Erro: "release already exists"**
Acesse GitHub → Releases → Deletar release existente antes de recriar.

---

## 📚 Referências

- [GitHub Releases API](https://docs.github.com/en/rest/releases/releases)
- [Semantic Versioning](https://semver.org/)
- [Go Cross-Compilation](https://www.digitalocean.com/community/tutorials/how-to-build-go-executables-for-multiple-platforms-on-ubuntu-16-04)

---

## 💡 Dicas

1. **Sempre teste** os binários antes de publicar
2. **Use versionamento semântico** (MAJOR.MINOR.PATCH)
3. **Escreva release notes detalhadas** (o que mudou, como instalar)
4. **Anuncie novas versões** para usuários (email, Slack, etc)
5. **Mantenha releases antigas** disponíveis (rollback se necessário)

---

## ✅ Checklist de Release

- [ ] Código commitado e testado
- [ ] Release notes criadas (`RELEASE_NOTES_v*.md`)
- [ ] Tag Git criada (`git tag -a v1.0.5`)
- [ ] Binários compilados (`make release`)
- [ ] Release criada no GitHub (`./create-release.sh`)
- [ ] Binários testados em plataforma alvo
- [ ] Documentação atualizada (se necessário)
- [ ] Usuários notificados sobre nova versão

---

**🎉 Pronto! Agora você tem um sistema completo de releases com binários pré-compilados.**
