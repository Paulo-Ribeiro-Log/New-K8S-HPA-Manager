#!/bin/bash
# Script para validar token do GitHub

set -e

DB_PATH="$HOME/.k8s-hpa-manager/github-tokens.db"

echo "🔍 Validador de Token GitHub"
echo "=============================="
echo ""

# Verificar se o banco existe
if [ ! -f "$DB_PATH" ]; then
    echo "❌ Banco de dados não encontrado: $DB_PATH"
    exit 1
fi

# Listar emails salvos
echo "📧 Emails com tokens salvos:"
sqlite3 "$DB_PATH" "SELECT user_email FROM github_tokens;" 2>/dev/null || {
    echo "❌ Erro ao ler banco de dados"
    exit 1
}
echo ""

# Solicitar email
read -p "Digite o email para validar: " EMAIL

if [ -z "$EMAIL" ]; then
    echo "❌ Email não pode estar vazio"
    exit 1
fi

# Verificar se existe token para este email
COUNT=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM github_tokens WHERE user_email='$EMAIL';" 2>/dev/null)

if [ "$COUNT" -eq 0 ]; then
    echo "❌ Nenhum token encontrado para o email: $EMAIL"
    exit 1
fi

echo "✅ Token encontrado no banco de dados"
echo ""

# Solicitar token manualmente para teste
echo "Para validar, você precisa fornecer o token do GitHub."
read -p "Cole seu token GitHub (ghp_...): " TOKEN

if [ -z "$TOKEN" ]; then
    echo "❌ Token não pode estar vazio"
    exit 1
fi

echo ""
echo "🔐 Validando token com API do GitHub..."
echo ""

# Testar token com API do GitHub
RESPONSE=$(curl -s -w "\n%{http_code}" -H "Authorization: token $TOKEN" https://api.github.com/user)
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n-1)

echo "📊 Status HTTP: $HTTP_CODE"
echo ""

if [ "$HTTP_CODE" -eq 200 ]; then
    echo "✅ Token válido!"
    echo ""
    echo "👤 Informações do usuário:"
    echo "$BODY" | jq -r '"  Login: " + .login'
    echo "$BODY" | jq -r '"  Nome: " + (.name // "N/A")'
    echo "$BODY" | jq -r '"  Email: " + (.email // "N/A")'
    echo "$BODY" | jq -r '"  Company: " + (.company // "N/A")'
    echo ""
    
    # Rate limit
    RATE_RESPONSE=$(curl -s -H "Authorization: token $TOKEN" https://api.github.com/rate_limit)
    echo "📈 Rate Limit:"
    echo "$RATE_RESPONSE" | jq -r '.rate | "  Limite: " + (.limit|tostring) + "/hora"'
    echo "$RATE_RESPONSE" | jq -r '.rate | "  Usado: " + (.used|tostring)'
    echo "$RATE_RESPONSE" | jq -r '.rate | "  Restante: " + (.remaining|tostring)'
    
    # Verificar permissões
    echo ""
    echo "🔑 Escopos do token:"
    SCOPES=$(curl -s -I -H "Authorization: token $TOKEN" https://api.github.com/user | grep -i "x-oauth-scopes:" | cut -d: -f2 | tr -d '\r')
    if [ -z "$SCOPES" ]; then
        echo "  (Nenhum escopo específico - token clássico sem escopos ou fine-grained)"
    else
        echo "  $SCOPES"
    fi
    
    echo ""
    echo "✅ Validação completa!"
    
elif [ "$HTTP_CODE" -eq 401 ]; then
    echo "❌ Token inválido ou expirado"
    echo ""
    echo "Resposta da API:"
    echo "$BODY" | jq '.'
    
elif [ "$HTTP_CODE" -eq 403 ]; then
    echo "⚠️ Token válido mas sem permissões ou rate limit excedido"
    echo ""
    echo "Resposta da API:"
    echo "$BODY" | jq '.'
    
else
    echo "❌ Erro inesperado (HTTP $HTTP_CODE)"
    echo ""
    echo "Resposta da API:"
    echo "$BODY" | jq '.' 2>/dev/null || echo "$BODY"
fi

echo ""
echo "📝 Teste de acesso a repositório específico:"
read -p "Organização (ex: viavarejo-internal): " ORG
read -p "Repositório (ex: vv-retira-geolocalizacao): " REPO

if [ -n "$ORG" ] && [ -n "$REPO" ]; then
    echo ""
    echo "🔍 Testando acesso a $ORG/$REPO..."
    
    REPO_RESPONSE=$(curl -s -w "\n%{http_code}" -H "Authorization: token $TOKEN" "https://api.github.com/repos/$ORG/$REPO")
    REPO_HTTP_CODE=$(echo "$REPO_RESPONSE" | tail -n1)
    REPO_BODY=$(echo "$REPO_RESPONSE" | head -n-1)
    
    if [ "$REPO_HTTP_CODE" -eq 200 ]; then
        echo "✅ Acesso ao repositório OK!"
        echo "$REPO_BODY" | jq -r '"  Nome: " + .full_name'
        echo "$REPO_BODY" | jq -r '"  Privado: " + (.private|tostring)'
        echo "$REPO_BODY" | jq -r '"  Visibilidade: " + (.visibility // "N/A")'
    elif [ "$REPO_HTTP_CODE" -eq 404 ]; then
        echo "❌ Repositório não encontrado ou sem acesso"
        echo ""
        echo "Possíveis causas:"
        echo "  - Repositório não existe"
        echo "  - Token não tem permissão para este repositório"
        echo "  - Token não tem acesso à organização"
    else
        echo "❌ Erro ao acessar repositório (HTTP $REPO_HTTP_CODE)"
        echo "$REPO_BODY" | jq '.' 2>/dev/null || echo "$REPO_BODY"
    fi
fi

echo ""
echo "=============================="
echo "✅ Validação concluída"
