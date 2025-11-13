#!/bin/bash
set -e

if [ -z "$GITHUB_TOKEN" ]; then
    echo "❌ GITHUB_TOKEN não definido"
    echo "Execute: export GITHUB_TOKEN='seu_token'"
    exit 1
fi

OWNER="Paulo-Ribeiro-Log"
REPO="New-K8S-HPA-Manager"
TAG="v1.0.3"

echo "🔄 Atualizando binários da release v1.0.3..."

# Obter ID da release
RELEASE_ID=$(curl -s \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  https://api.github.com/repos/$OWNER/$REPO/releases/tags/$TAG | jq -r '.id')

if [ "$RELEASE_ID" = "null" ] || [ -z "$RELEASE_ID" ]; then
    echo "❌ Erro ao obter ID da release"
    exit 1
fi

echo "Release ID: $RELEASE_ID"

# Deletar assets antigos
echo "🗑️  Deletando assets antigos..."
ASSETS=$(curl -s \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  https://api.github.com/repos/$OWNER/$REPO/releases/$RELEASE_ID/assets)

echo "$ASSETS" | jq -r '.[].id' | while read ASSET_ID; do
    curl -s -X DELETE \
      -H "Authorization: Bearer $GITHUB_TOKEN" \
      "https://api.github.com/repos/$OWNER/$REPO/releases/assets/$ASSET_ID"
    echo "  ✓ Asset $ASSET_ID deletado"
done

# Obter upload URL
UPLOAD_URL=$(curl -s \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  https://api.github.com/repos/$OWNER/$REPO/releases/$RELEASE_ID | jq -r '.upload_url' | sed 's/{?name,label}//')

# Upload novos binários
BINARIES=(
    "build/release/new-k8s-hpa-linux-amd64"
    "build/release/new-k8s-hpa-darwin-amd64"
    "build/release/new-k8s-hpa-darwin-arm64"
    "build/release/new-k8s-hpa-windows-amd64.exe"
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

    echo "  ✅ $FILENAME uploaded"
done

echo ""
echo "🎉 Binários atualizados com versão 1.0.3!"
echo "🔗 https://github.com/$OWNER/$REPO/releases/tag/$TAG"
