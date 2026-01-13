# Configuração do GitHub Token

Este guia explica como configurar seu GitHub token de forma segura para criar releases.

## 🔒 Segurança

Seu token GitHub é **extremamente sensível** e nunca deve ser commitado ao repositório. Por isso:

- ✅ Arquivos `.env`, `*.token`, `github_token.txt` e `secrets.sh` estão no `.gitignore`
- ✅ Você pode salvar o token localmente sem risco de commit acidental
- ✅ Script `setup-github-token.sh` facilita a configuração

## 🚀 Método Recomendado (Script Automatizado)

```bash
# Execute o script auxiliar
./setup-github-token.sh

# O script irá:
# 1. Solicitar seu token GitHub
# 2. Validar o formato do token
# 3. Salvar em github_token.txt (ignorado pelo git)
# 4. Configurar permissões seguras (600)
# 5. Testar o token com a API GitHub
# 6. Mostrar instruções de uso
```

## 📝 Método Manual

### 1. Criar GitHub Token

1. Acesse: https://github.com/settings/tokens/new
2. **Token name**: `K8S HPA Manager Releases`
3. **Expiration**: Escolha a validade desejada (recomendado: 90 days)
4. **Scopes necessários**:
   - ☑️ `repo` (Full control of private repositories)
     - Inclui: `repo:status`, `repo_deployment`, `public_repo`, `repo:invite`, `security_events`
5. Clique em **"Generate token"**
6. **COPIE o token imediatamente** (você só verá uma vez!)

### 2. Salvar Token Localmente

Escolha uma das opções:

**Opção A - Arquivo de token (recomendado)**:
```bash
# Salvar token em arquivo
echo "seu_token_aqui" > github_token.txt

# Proteger arquivo (apenas você pode ler)
chmod 600 github_token.txt

# Exportar para uso
export GITHUB_TOKEN=$(cat github_token.txt)
```

**Opção B - Arquivo .env**:
```bash
# Criar arquivo .env
echo "export GITHUB_TOKEN='seu_token_aqui'" > .env

# Proteger arquivo
chmod 600 .env

# Carregar variável
source .env
```

**Opção C - Arquivo secrets.sh**:
```bash
# Criar arquivo de secrets
cat > secrets.sh << 'EOF'
#!/bin/bash
export GITHUB_TOKEN="seu_token_aqui"
EOF

# Proteger arquivo
chmod 600 secrets.sh

# Carregar variável
source secrets.sh
```

### 3. Tornar Permanente (Opcional)

Para não precisar exportar o token toda vez:

```bash
# Adicionar ao ~/.bashrc
echo 'export GITHUB_TOKEN=$(cat ~/Scripts/Scripts\ GO/New-K8s-HPA-Manager/Scale_HPA/github_token.txt)' >> ~/.bashrc

# Recarregar bashrc
source ~/.bashrc
```

## ✅ Verificar Configuração

```bash
# Método 1: Verificar variável de ambiente
echo $GITHUB_TOKEN

# Método 2: Testar com API GitHub
curl -H "Authorization: Bearer $GITHUB_TOKEN" https://api.github.com/user

# Deve retornar JSON com seu username
```

## 🎯 Usar Token para Criar Release

### Script Genérico (Recomendado):
```bash
# O script busca automaticamente o token em múltiplas localizações
./create-release.sh 1.0.5

# Ou deixe o script detectar a versão via git tag
git tag v1.0.5
./create-release.sh
```

### Scripts Específicos de Versão:
```bash
# Para versões específicas (se existirem)
./create-release-v1.0.4.sh
```

**O script genérico `create-release.sh`**:
- ✅ Busca token automaticamente (github_token.txt, .env, secrets.sh)
- ✅ Funciona para qualquer versão
- ✅ Detecta versão via git tag ou argumento
- ✅ Verifica se binários existem
- ✅ Pede confirmação antes de criar release
- ✅ Faz upload automático dos binários

## 🔄 Renovar Token Expirado

Quando seu token expirar:

```bash
# Opção 1: Usar script (recomendado)
./setup-github-token.sh

# Opção 2: Manual
# 1. Gerar novo token em: https://github.com/settings/tokens
# 2. Sobrescrever arquivo existente:
echo "novo_token_aqui" > github_token.txt
```

## 🗑️ Revogar Token

Se você suspeitar que o token foi comprometido:

1. Acesse: https://github.com/settings/tokens
2. Localize o token "K8S HPA Manager Releases"
3. Clique em **"Delete"**
4. Gere um novo token seguindo este guia

```bash
# Remover token local
rm github_token.txt
# ou
rm .env
# ou
rm secrets.sh
```

## ❌ O Que NÃO Fazer

- ❌ **NUNCA** commitar o token no Git
- ❌ **NUNCA** compartilhar o token por email/chat
- ❌ **NUNCA** usar o token em logs ou prints de tela
- ❌ **NUNCA** adicionar o token diretamente em scripts versionados

## 🛡️ Boas Práticas

- ✅ Use tokens com **escopo mínimo necessário** (apenas `repo`)
- ✅ Configure **expiração** (90 dias recomendado)
- ✅ **Revogue** tokens não utilizados
- ✅ Use **permissões 600** nos arquivos de token
- ✅ **Revogue imediatamente** se suspeitar de vazamento

## 📋 Troubleshooting

### Erro: "GITHUB_TOKEN não definido"
```bash
# Verificar se variável está exportada
echo $GITHUB_TOKEN

# Se vazio, exportar novamente
export GITHUB_TOKEN=$(cat github_token.txt)
```

### Erro: "API rate limit exceeded"
```bash
# Verificar rate limit
curl -H "Authorization: Bearer $GITHUB_TOKEN" https://api.github.com/rate_limit

# Se sem token configurado: 60 requests/hora
# Com token: 5000 requests/hora
```

### Erro: "Bad credentials"
```bash
# Token inválido ou expirado
# 1. Verificar token em: https://github.com/settings/tokens
# 2. Gerar novo token
# 3. Atualizar arquivo local
```

## 🔗 Links Úteis

- **Criar token**: https://github.com/settings/tokens/new
- **Gerenciar tokens**: https://github.com/settings/tokens
- **GitHub API Docs**: https://docs.github.com/en/rest/authentication
- **Rate Limits**: https://docs.github.com/en/rest/rate-limit

---

**Nota**: Este arquivo pode ser commitado ao repositório, pois não contém informações sensíveis - apenas instruções.
