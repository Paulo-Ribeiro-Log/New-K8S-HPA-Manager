#!/bin/bash

# Script de teste para conexão com Nexus
# Usage: ./test-nexus-connection.sh <username> <password>

if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <username> <password>"
    echo "Example: $0 paulo.ribeiro mypassword"
    exit 1
fi

USERNAME="$1"
PASSWORD="$2"
BASE_URL="https://nexus.viavarejo.com.br"

echo "=================================================="
echo "🧪 Teste de Conexão com Nexus"
echo "=================================================="
echo ""
echo "Base URL: $BASE_URL"
echo "Username: $USERNAME"
echo ""

# Teste 1: Verificar se o Nexus está acessível
echo "📡 Teste 1: Verificando conectividade..."
if curl -s --max-time 10 "$BASE_URL" > /dev/null; then
    echo "✅ Nexus está acessível"
else
    echo "❌ Nexus NÃO está acessível"
    echo "   Verifique sua conexão VPN"
    exit 1
fi
echo ""

# Teste 2: Verificar autenticação
echo "🔐 Teste 2: Testando autenticação..."
STATUS=$(curl -s -o /dev/null -w "%{http_code}" -u "$USERNAME:$PASSWORD" "$BASE_URL/service/rest/v1/status")
if [ "$STATUS" = "200" ]; then
    echo "✅ Autenticação bem-sucedida (Status: $STATUS)"
elif [ "$STATUS" = "401" ]; then
    echo "❌ Autenticação FALHOU - Credenciais inválidas (Status: $STATUS)"
    exit 1
else
    echo "⚠️  Status inesperado: $STATUS"
fi
echo ""

# Teste 3: Listar repositories
echo "📦 Teste 3: Listando repositories..."
REPOS=$(curl -s -u "$USERNAME:$PASSWORD" "$BASE_URL/service/rest/v1/repositories" | grep -o '"name":"[^"]*"' | head -5)
if [ -n "$REPOS" ]; then
    echo "✅ Repositories encontrados:"
    echo "$REPOS" | sed 's/"name":"//g' | sed 's/"//g' | sed 's/^/   - /'
else
    echo "⚠️  Nenhum repository encontrado ou sem permissão"
fi
echo ""

# Teste 4: Verificar workspace repository
echo "🗂️  Teste 4: Verificando repository 'workspace'..."
WORKSPACE_EXISTS=$(curl -s -u "$USERNAME:$PASSWORD" "$BASE_URL/service/rest/v1/repositories" | grep -c '"workspace"')
if [ "$WORKSPACE_EXISTS" -gt 0 ]; then
    echo "✅ Repository 'workspace' encontrado"
else
    echo "⚠️  Repository 'workspace' NÃO encontrado"
    echo "   Verifique se o nome está correto"
fi
echo ""

# Teste 5: Exemplo de download
echo "📥 Teste 5: Exemplo de URL de download"
echo "   Para baixar um arquivo, a URL deve ser:"
echo "   $BASE_URL/repository/workspace/<release>/<version>/<env>/helm-values/<type>-values.yaml"
echo ""
echo "   Exemplo:"
echo "   $BASE_URL/repository/workspace/meu-app/v1.0.0/prd/helm-values/base-values.yaml"
echo ""

echo "=================================================="
echo "✅ Testes concluídos!"
echo "=================================================="
echo ""
echo "📝 Próximos passos:"
echo "   1. Anote as informações acima"
echo "   2. Use essas credenciais na configuração do Nexus"
echo "   3. Certifique-se de usar o formato correto da URL"
echo ""
