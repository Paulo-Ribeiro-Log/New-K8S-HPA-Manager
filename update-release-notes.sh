#!/bin/bash
set -e

if [ -z "$GITHUB_TOKEN" ]; then
    echo "❌ GITHUB_TOKEN não definido"
    echo "Execute: export GITHUB_TOKEN='seu_token'"
    exit 1
fi

echo "📝 Atualizando release notes v1.0.3..."

# Obter ID da release
RELEASE_ID=$(curl -s \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  https://api.github.com/repos/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/tags/v1.0.3 | jq -r '.id')

if [ "$RELEASE_ID" = "null" ] || [ -z "$RELEASE_ID" ]; then
    echo "❌ Erro ao obter ID da release"
    exit 1
fi

echo "Release ID: $RELEASE_ID"

# Ler release notes corrigidas
RELEASE_NOTES=$(cat RELEASE_NOTES_v1.0.3.md | jq -Rs .)

# Atualizar release
curl -s -X PATCH \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "https://api.github.com/repos/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/$RELEASE_ID" \
  -d "{\"body\": $RELEASE_NOTES}" > /dev/null

echo "✅ Release notes atualizadas!"
echo "🔗 https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/tag/v1.0.3"
