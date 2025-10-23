# 📦 Guia: Publicar Release v1.2.0

## ✅ Preparação Completa

- ✅ Tag v1.2.0 criada localmente
- ✅ Binário Linux amd64 compilado (81MB)
- ✅ Release notes criadas (`RELEASE_NOTES_v1.2.0.md`)
- ✅ Documentação atualizada (README.md, CLAUDE.md)

---

## 🚀 Passos para Publicar

### 1️⃣ Push da Tag para GitHub

```bash
cd /home/paulo/Scripts/Scripts\ GO/Scale_HPA/Scale_HPA

# Fazer push da tag
git push origin v1.2.0
```

**Esperado:**
```
To github.com:Paulo-Ribeiro-Log/Scale_HPA.git
 * [new tag]         v1.2.0 -> v1.2.0
```

---

### 2️⃣ Publicar Release via Interface Web (Método Mais Fácil)

#### Passo A: Acessar Página de Criação

Abra no navegador:
```
https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/new?tag=v1.2.0
```

#### Passo B: Preencher Informações

1. **Choose a tag**: `v1.2.0` (já selecionado)
2. **Release title**: `Release v1.2.0 - Complete Installation and Update System`
3. **Describe this release**: Copiar conteúdo de `RELEASE_NOTES_v1.2.0.md`

#### Passo C: Upload do Binário

1. Arrastar ou clicar em "Attach binaries by dropping them here"
2. Selecionar arquivo:
   ```
   /home/paulo/Scripts/Scripts GO/Scale_HPA/Scale_HPA/build/release/k8s-hpa-manager-linux-amd64
   ```
3. Aguardar upload completar

#### Passo D: Publicar

1. Verificar que **"Set as a pre-release"** está **DESMARCADO**
2. Clicar em **"Publish release"** (botão verde)

---

### 3️⃣ Verificar Publicação

Após publicar, verificar em:
```
https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.0
```

**Deve mostrar:**
- ✅ Release v1.2.0
- ✅ Release notes completas
- ✅ Binário `k8s-hpa-manager-linux-amd64` disponível para download
- ✅ Assets do Source code (zip/tar.gz) gerados automaticamente

---

### 4️⃣ Testar Sistema de Updates

#### Teste 1: Verificação Manual

```bash
# Com binário v1.1.0 (se tiver)
./build/k8s-hpa-manager version

# Esperado:
# k8s-hpa-manager versão 1.1.0
# 🔍 Verificando updates...
# 🆕 Nova versão disponível: 1.1.0 → 1.2.0
# 📦 Download: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.0
```

#### Teste 2: Auto-Update

```bash
# Simular atualização
~/.k8s-hpa-manager/scripts/auto-update.sh --dry-run

# Esperado:
# ⚠️  MODO DRY RUN - Nenhuma alteração será feita
# ℹ️  Verificando atualizações disponíveis...
# ⚠️  Nova versão disponível: 1.1.0 → 1.2.0
# [DRY RUN] Simulando download e instalação...
```

#### Teste 3: Instalação do Zero

```bash
# Em terminal limpo ou máquina de teste
curl -fsSL https://raw.githubusercontent.com/Paulo-Ribeiro-Log/Scale_HPA/main/install-from-github.sh | bash

# Esperado:
# 🏗️  K8s HPA Manager - Instalador Completo
# [...]
# ✅ Instalação concluída com sucesso!
# Versão instalada: 1.2.0
```

---

## 🔄 Alternativa: Publicar via API (Se Tiver Token)

### Método com Script Automatizado

```bash
# Configurar token (se necessário)
export GITHUB_TOKEN="ghp_seu_token_aqui"

# Executar script de release
cd /home/paulo/Scripts/Scripts\ GO/Scale_HPA/Scale_HPA
./create_release.sh
```

**Nota:** Requer editar `create_release.sh` para mudar versão de v1.1.0 para v1.2.0

### Método Manual via curl

```bash
# Criar release
curl -X POST \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github.v3+json" \
  https://api.github.com/repos/Paulo-Ribeiro-Log/Scale_HPA/releases \
  -d @- <<'EOF'
{
  "tag_name": "v1.2.0",
  "name": "Release v1.2.0 - Complete Installation and Update System",
  "body": "$(cat RELEASE_NOTES_v1.2.0.md)",
  "draft": false,
  "prerelease": false
}
EOF

# Upload do binário (após obter release_id)
RELEASE_ID=123456  # Substituir pelo ID retornado acima
curl -X POST \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Content-Type: application/octet-stream" \
  "https://uploads.github.com/repos/Paulo-Ribeiro-Log/Scale_HPA/releases/$RELEASE_ID/assets?name=k8s-hpa-manager-linux-amd64" \
  --data-binary @build/release/k8s-hpa-manager-linux-amd64
```

---

## 📝 Checklist Final

Antes de publicar, verificar:

- [ ] Tag v1.2.0 criada: `git tag -l | grep v1.2.0`
- [ ] Binário compilado: `ls -lh build/release/k8s-hpa-manager-linux-amd64`
- [ ] Versão correta no binário: `./build/release/k8s-hpa-manager-linux-amd64 version`
- [ ] Release notes prontas: `cat RELEASE_NOTES_v1.2.0.md`
- [ ] Documentação atualizada: README.md e CLAUDE.md

Após publicar:

- [ ] Release visível em: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.2.0
- [ ] Binário disponível para download
- [ ] Comando `k8s-hpa-manager version` detecta update (em v1.1.0)
- [ ] Instalador funciona: `curl ... install-from-github.sh | bash`

---

## 🐛 Troubleshooting

### Erro: "tag v1.2.0 not found"

```bash
# Verificar se tag existe localmente
git tag -l | grep v1.2.0

# Se não existir, criar
git tag v1.2.0 -m "Release v1.2.0"

# Push da tag
git push origin v1.2.0
```

### Erro: "Failed to upload asset"

- Verificar tamanho do arquivo (máximo 2GB)
- Verificar conexão de internet
- Tentar novamente via interface web

### Binário não tem versão correta

```bash
# Recompilar com versão correta
cd /home/paulo/Scripts/Scripts\ GO/Scale_HPA/Scale_HPA
GOOS=linux GOARCH=amd64 go build -ldflags "-X k8s-hpa-manager/internal/updater.Version=1.2.0" -o build/release/k8s-hpa-manager-linux-amd64 .

# Verificar
./build/release/k8s-hpa-manager-linux-amd64 version
```

---

## 🎉 Após Publicação

1. **Anunciar no README** (se necessário)
2. **Testar instalação** em máquina limpa
3. **Testar auto-update** de v1.1.0 → v1.2.0
4. **Monitorar issues** de instalação

---

**Boa sorte com a publicação!** 🚀
