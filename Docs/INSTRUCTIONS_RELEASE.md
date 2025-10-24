# 📦 Instruções para Publicar Release v1.1.0

## Opção 1: Via Interface Web do GitHub (Mais Fácil)

1. **Acesse a página de criação de releases:**
   ```
   https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/new?tag=v1.1.0
   ```

2. **Preencha os campos:**
   - **Choose a tag**: `v1.1.0` (deve aparecer automaticamente)
   - **Release title**: `Release v1.1.0`
   - **Describe this release**: Copie o conteúdo de `RELEASE_NOTES_v1.1.0.md`

3. **Upload do binário (opcional):**
   - Clique em "Attach binaries by dropping them here"
   - Arraste o arquivo: `./build/release/k8s-hpa-manager-linux-amd64`
   - Ou clique para selecionar do disco

4. **Publicar:**
   - Desmarque "Set as a pre-release" (já está como release oficial)
   - Clique em **"Publish release"**

5. **Pronto!** ✅
   - A release estará disponível em: `https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.1.0`
   - O sistema de updates funcionará automaticamente

---

## Opção 2: Via Script Automatizado

### Pré-requisitos:
- Token do GitHub com permissões `repo`
- `jq` instalado (`sudo apt install jq`)

### Passos:

1. **Criar token do GitHub:**
   ```
   https://github.com/settings/tokens/new
   ```
   - Nome: "k8s-hpa-manager releases"
   - Permissões: `repo` (Full control of private repositories)
   - Copiar token gerado

2. **Configurar token:**
   ```bash
   # Opção A: Variável de ambiente
   export GITHUB_TOKEN="ghp_seu_token_aqui"

   # Opção B: Arquivo (mais seguro)
   mkdir -p ~/.k8s-hpa-manager
   echo "ghp_seu_token_aqui" > ~/.k8s-hpa-manager/.github-token
   chmod 600 ~/.k8s-hpa-manager/.github-token
   export GITHUB_TOKEN=$(cat ~/.k8s-hpa-manager/.github-token)
   ```

3. **Executar script:**
   ```bash
   ./create_release.sh
   ```

4. **Resultado esperado:**
   ```
   🚀 Criando Release v1.1.0 no GitHub...
   📦 Criando release v1.1.0...
   ✅ Release criada com sucesso! ID: 123456
   📤 Fazendo upload do binário Linux amd64...
   ✅ Upload completo!
   🌐 Release publicada em:
      https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/tag/v1.1.0
   🎉 Release v1.1.0 publicada com sucesso!
   ```

---

## Verificação Pós-Publicação

Após publicar a release, teste o sistema de updates:

```bash
# Rebuild com versão 1.1.0
make build

# Verificar versão
./build/k8s-hpa-manager version
# Esperado: "k8s-hpa-manager versão 1.1.0"
# Esperado: "✅ Você está usando a versão mais recente!"

# Forçar verificação (resetar cache)
rm ~/.k8s-hpa-manager/.update-check
./build/k8s-hpa-manager version
# Deve buscar da API e confirmar que está atualizado
```

---

## Troubleshooting

### Erro: "tag v1.1.0 not found"
- **Causa**: Tag não foi pushed para o GitHub
- **Solução**:
  ```bash
  git tag v1.1.0
  git push origin v1.1.0
  ```

### Erro: "401 Unauthorized"
- **Causa**: Token inválido ou sem permissões
- **Solução**: Criar novo token com permissão `repo`

### Erro: "422 Unprocessable Entity"
- **Causa**: Release já existe
- **Solução**: Deletar release existente ou criar com tag diferente

---

## Próximos Passos

Após publicar v1.1.0, para criar v1.2.0 no futuro:

```bash
# 1. Criar nova tag
git tag v1.2.0
git push origin v1.2.0

# 2. Build com nova versão
make build

# 3. Gerar binários release
make release

# 4. Criar release
# - Via web: https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/new?tag=v1.2.0
# - Via script: ./create_release.sh (atualizar versão no script antes)
```

---

**Boa sorte!** 🚀
