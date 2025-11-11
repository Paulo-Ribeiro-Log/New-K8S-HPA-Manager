#!/bin/bash
set -e

echo "🚀 Criando Release v1.1.0 no GitHub..."
echo ""

# Verificar se token está configurado
if [ -z "$GITHUB_TOKEN" ]; then
    echo "❌ GITHUB_TOKEN não está definido"
    echo ""
    echo "Opções:"
    echo "1. Definir variável de ambiente: export GITHUB_TOKEN='seu_token_aqui'"
    echo "2. Criar arquivo: echo 'seu_token_aqui' > ~/.k8s-hpa-manager/.github-token"
    echo "3. Criar release manualmente via web:"
    echo "   https://github.com/Paulo-Ribeiro-Log/Scale_HPA/releases/new?tag=v1.1.0"
    echo ""
    exit 1
fi

# Ler release notes
RELEASE_NOTES=$(cat RELEASE_NOTES_v1.1.0.md)

# Criar release via API
echo "📦 Criando release v1.1.0..."
RESPONSE=$(curl -s -X POST \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github.v3+json" \
  https://api.github.com/repos/Paulo-Ribeiro-Log/Scale_HPA/releases \
  -d @- <<EOF
{
  "tag_name": "v1.1.0",
  "name": "Release v1.1.0",
  "body": $(echo "$RELEASE_NOTES" | jq -Rs .),
  "draft": false,
  "prerelease": false
}
EOF
)

# Verificar se criou com sucesso
RELEASE_ID=$(echo "$RESPONSE" | jq -r '.id')
if [ "$RELEASE_ID" = "null" ] || [ -z "$RELEASE_ID" ]; then
    echo "❌ Erro ao criar release:"
    echo "$RESPONSE" | jq '.'
    exit 1
fi

echo "✅ Release criada com sucesso! ID: $RELEASE_ID"
echo ""

# Upload do binário Linux
echo "📤 Fazendo upload do binário Linux amd64..."
UPLOAD_URL=$(echo "$RESPONSE" | jq -r '.upload_url' | sed 's/{?name,label}//')

curl -s -X POST \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Content-Type: application/octet-stream" \
  "${UPLOAD_URL}?name=k8s-hpa-manager-linux-amd64" \
  --data-binary @./build/release/k8s-hpa-manager-linux-amd64 > /dev/null

echo "✅ Upload completo!"
echo ""

# Exibir URL da release
RELEASE_URL=$(echo "$RESPONSE" | jq -r '.html_url')
echo "🌐 Release publicada em:"
echo "   $RELEASE_URL"
echo ""
echo "🎉 Release v1.1.0 publicada com sucesso!"
