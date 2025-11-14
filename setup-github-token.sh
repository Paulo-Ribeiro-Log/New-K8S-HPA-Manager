#!/bin/bash

# Script para configurar GitHub token de forma segura
# O token será salvo em github_token.txt (ignorado pelo git)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOKEN_FILE="$SCRIPT_DIR/github_token.txt"

echo "═══════════════════════════════════════════════════════════"
echo "  Configuração de GitHub Token para Releases"
echo "═══════════════════════════════════════════════════════════"
echo ""

# Verificar se token já existe
if [ -f "$TOKEN_FILE" ]; then
    echo "⚠️  Token já existe em: $TOKEN_FILE"
    read -p "Deseja sobrescrever? (s/N): " -r overwrite
    if [[ ! $overwrite =~ ^[Ss]$ ]]; then
        echo "✅ Mantendo token existente"
        echo ""
        echo "Para usar o token, execute:"
        echo "  export GITHUB_TOKEN=\$(cat $TOKEN_FILE)"
        echo ""
        echo "Ou adicione ao seu ~/.bashrc:"
        echo "  echo 'export GITHUB_TOKEN=\$(cat $TOKEN_FILE)' >> ~/.bashrc"
        exit 0
    fi
fi

echo "📝 Como obter seu GitHub token:"
echo ""
echo "1. Acesse: https://github.com/settings/tokens/new"
echo "2. Token name: 'K8S HPA Manager Releases'"
echo "3. Expiration: Escolha validade desejada"
echo "4. Scopes necessários:"
echo "   ☑️  repo (Full control of private repositories)"
echo "5. Clique em 'Generate token'"
echo "6. COPIE o token (você só verá uma vez!)"
echo ""

read -p "Cole seu GitHub token aqui: " -r token

if [ -z "$token" ]; then
    echo "❌ Token vazio. Abortando."
    exit 1
fi

# Validar formato do token (deve começar com ghp_ ou github_pat_)
if [[ ! $token =~ ^(ghp_|github_pat_) ]]; then
    echo "⚠️  Aviso: Token não parece ter o formato correto"
    echo "   Tokens GitHub normalmente começam com 'ghp_' ou 'github_pat_'"
    read -p "Continuar mesmo assim? (s/N): " -r continue_anyway
    if [[ ! $continue_anyway =~ ^[Ss]$ ]]; then
        echo "❌ Abortando."
        exit 1
    fi
fi

# Salvar token
echo "$token" > "$TOKEN_FILE"
chmod 600 "$TOKEN_FILE"  # Apenas owner pode ler/escrever

echo ""
echo "✅ Token salvo com sucesso em: $TOKEN_FILE"
echo "🔒 Permissões configuradas (600 - apenas você pode ler)"
echo ""
echo "════════════════════════════════════════════════════════════"
echo "  Como usar o token"
echo "════════════════════════════════════════════════════════════"
echo ""
echo "Opção 1 - Exportar manualmente (válido apenas na sessão atual):"
echo "  export GITHUB_TOKEN=\$(cat $TOKEN_FILE)"
echo ""
echo "Opção 2 - Adicionar ao ~/.bashrc (permanente):"
echo "  echo 'export GITHUB_TOKEN=\$(cat $TOKEN_FILE)' >> ~/.bashrc"
echo "  source ~/.bashrc"
echo ""
echo "Opção 3 - Usar inline no comando:"
echo "  GITHUB_TOKEN=\$(cat $TOKEN_FILE) ./create-release-v1.0.4.sh"
echo ""
echo "════════════════════════════════════════════════════════════"
echo "  Testar configuração"
echo "════════════════════════════════════════════════════════════"
echo ""

read -p "Deseja testar o token agora? (S/n): " -r test_token
if [[ ! $test_token =~ ^[Nn]$ ]]; then
    export GITHUB_TOKEN="$token"

    echo "🔍 Testando token com GitHub API..."

    response=$(curl -s -H "Authorization: Bearer $GITHUB_TOKEN" \
        https://api.github.com/user)

    if echo "$response" | grep -q '"login"'; then
        username=$(echo "$response" | grep -o '"login": *"[^"]*"' | cut -d'"' -f4)
        echo "✅ Token válido! Autenticado como: $username"
    else
        echo "❌ Falha ao validar token"
        echo "Resposta da API: $response"
        exit 1
    fi
fi

echo ""
echo "✅ Configuração completa!"
echo ""
echo "Próximos passos:"
echo "1. Exportar token: export GITHUB_TOKEN=\$(cat $TOKEN_FILE)"
echo "2. Executar release: ./create-release-v1.0.4.sh"
echo ""
