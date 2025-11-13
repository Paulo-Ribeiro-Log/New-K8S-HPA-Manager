#!/bin/bash
set -e

OWNER="Paulo-Ribeiro-Log"
REPO="New-K8S-HPA-Manager"
TAG="v1.0.3"
RELEASE_NAME="v1.0.3 - Correções de Repositório e Versão no Header"

# Verificar token
if [ -z "$GITHUB_TOKEN" ]; then
    echo "❌ GITHUB_TOKEN não definido"
    echo ""
    echo "Configure seu token:"
    echo "  1. Crie token em: https://github.com/settings/tokens/new"
    echo "  2. Scope necessário: 'repo' (full control)"
    echo "  3. Execute: export GITHUB_TOKEN='seu_token_aqui'"
    echo "  4. Execute novamente este script"
    echo ""
    echo "Ou crie manualmente em:"
    echo "  https://github.com/$OWNER/$REPO/releases/new?tag=$TAG"
    exit 1
fi

echo "🚀 Criando release $TAG no GitHub..."

# Ler release notes
RELEASE_NOTES=$(cat RELEASE_NOTES_v1.0.3.md | jq -Rs .)

# Criar release
RESPONSE=$(curl -s -X POST \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "https://api.github.com/repos/$OWNER/$REPO/releases" \
  -d "{
    \"tag_name\": \"$TAG\",
    \"name\": \"$RELEASE_NAME\",
    \"body\": $RELEASE_NOTES,
    \"draft\": false,
    \"prerelease\": false
  }")

# Verificar se release foi criado
RELEASE_ID=$(echo "$RESPONSE" | jq -r '.id')
if [ "$RELEASE_ID" = "null" ] || [ -z "$RELEASE_ID" ]; then
    echo "❌ Erro ao criar release"
    echo "$RESPONSE" | jq .
    exit 1
fi

echo "✅ Release criado (ID: $RELEASE_ID)"
UPLOAD_URL=$(echo "$RESPONSE" | jq -r '.upload_url' | sed 's/{?name,label}//')

# Upload binários
BINARIES=(
    "build/release/k8s-hpa-manager-linux-amd64"
    "build/release/k8s-hpa-manager-darwin-amd64"
    "build/release/k8s-hpa-manager-darwin-arm64"
    "build/release/k8s-hpa-manager-windows-amd64.exe"
)

for binary in "${BINARIES[@]}"; do
    FILENAME=$(basename "$binary")
    echo "📤 Uploading $FILENAME..."
    
    curl -s -X POST \
      -H "Accept: application/vnd.github+json" \
      -H "Authorization: Bearer $GITHUB_TOKEN" \
      -H "X-GitHub-Api-Version: 2022-11-28" \
      -H "Content-Type: application/octet-stream" \
      "$UPLOAD_URL?name=$FILENAME" \
      --data-binary "@$binary" > /dev/null
    
    echo "✅ $FILENAME uploaded"
done

echo ""
echo "🎉 Release v1.0.3 publicada com sucesso!"
echo "🔗 URL: https://github.com/$OWNER/$REPO/releases/tag/$TAG"
