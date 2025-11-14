#!/bin/bash
# Script para criar release v1.0.1 via GitHub API REST
# Uso: export GITHUB_TOKEN='seu_token'; ./create-release-api.sh

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║     GitHub Release v1.0.1 Creator (REST API)            ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Configurações
OWNER="Paulo-Ribeiro-Log"
REPO="New-K8S-HPA-Manager"
TAG="v1.0.1"
RELEASE_NAME="v1.0.1 - Fix Build Vendor + Documentação Organizada"

# 1. Verificar token
if [ -z "$GITHUB_TOKEN" ]; then
    echo -e "${RED}❌ GITHUB_TOKEN não definido${NC}"
    echo ""
    echo -e "${YELLOW}Configure seu token:${NC}"
    echo "  1. Crie token em: https://github.com/settings/tokens/new"
    echo "  2. Scope necessário: 'repo' (full control)"
    echo "  3. Execute: export GITHUB_TOKEN='seu_token_aqui'"
    echo "  4. Execute novamente: ./create-release-api.sh"
    echo ""
    exit 1
fi

echo -e "${GREEN}✅ GITHUB_TOKEN encontrado${NC}"
echo ""

# 2. Verificar se binários existem
echo -e "${BLUE}📦 Verificando binários...${NC}"
BINARIES=(
    "build/release/k8s-hpa-manager-linux-amd64"
    "build/release/k8s-hpa-manager-darwin-amd64"
    "build/release/k8s-hpa-manager-darwin-arm64"
    "build/release/k8s-hpa-manager-windows-amd64.exe"
)

for binary in "${BINARIES[@]}"; do
    if [ ! -f "$binary" ]; then
        echo -e "${RED}❌ Binário não encontrado: $binary${NC}"
        exit 1
    fi
    SIZE=$(du -h "$binary" | cut -f1)
    echo -e "${GREEN}✅ $(basename $binary) ($SIZE)${NC}"
done
echo ""

# 3. Ler release notes
echo -e "${BLUE}📝 Lendo release notes...${NC}"
if [ ! -f "RELEASE_NOTES_v1.0.1.md" ]; then
    echo -e "${RED}❌ RELEASE_NOTES_v1.0.1.md não encontrado${NC}"
    exit 1
fi

RELEASE_NOTES=$(cat RELEASE_NOTES_v1.0.1.md)
echo -e "${GREEN}✅ Release notes carregadas ($(wc -l < RELEASE_NOTES_v1.0.1.md) linhas)${NC}"
echo ""

# 4. Criar release no GitHub
echo -e "${BLUE}🚀 Criando release v1.0.1...${NC}"

# Escape JSON (replace newlines with \n, escape quotes)
RELEASE_NOTES_JSON=$(echo "$RELEASE_NOTES" | jq -Rs .)

# Criar payload JSON
PAYLOAD=$(cat <<EOF
{
  "tag_name": "$TAG",
  "name": "$RELEASE_NAME",
  "body": $RELEASE_NOTES_JSON,
  "draft": false,
  "prerelease": false
}
EOF
)

# Criar release via API
RESPONSE=$(curl -s -X POST \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "https://api.github.com/repos/$OWNER/$REPO/releases" \
  -d "$PAYLOAD")

# Verificar se release foi criado
RELEASE_ID=$(echo "$RESPONSE" | jq -r '.id')
if [ "$RELEASE_ID" = "null" ] || [ -z "$RELEASE_ID" ]; then
    echo -e "${RED}❌ Erro ao criar release${NC}"
    echo "$RESPONSE" | jq .
    exit 1
fi

echo -e "${GREEN}✅ Release criado (ID: $RELEASE_ID)${NC}"
UPLOAD_URL=$(echo "$RESPONSE" | jq -r '.upload_url' | sed 's/{?name,label}//')
echo ""

# 5. Upload de binários
echo -e "${BLUE}📤 Fazendo upload de binários...${NC}"

for binary in "${BINARIES[@]}"; do
    FILENAME=$(basename "$binary")
    echo -e "${BLUE}  Uploading $FILENAME...${NC}"

    UPLOAD_RESPONSE=$(curl -s -X POST \
      -H "Accept: application/vnd.github+json" \
      -H "Authorization: Bearer $GITHUB_TOKEN" \
      -H "X-GitHub-Api-Version: 2022-11-28" \
      -H "Content-Type: application/octet-stream" \
      "$UPLOAD_URL?name=$FILENAME" \
      --data-binary "@$binary")

    ASSET_ID=$(echo "$UPLOAD_RESPONSE" | jq -r '.id')
    if [ "$ASSET_ID" = "null" ] || [ -z "$ASSET_ID" ]; then
        echo -e "${RED}  ❌ Erro ao fazer upload de $FILENAME${NC}"
        echo "$UPLOAD_RESPONSE" | jq .
    else
        SIZE=$(echo "$UPLOAD_RESPONSE" | jq -r '.size')
        SIZE_MB=$(echo "scale=2; $SIZE / 1024 / 1024" | bc)
        echo -e "${GREEN}  ✅ $FILENAME uploaded (${SIZE_MB} MB)${NC}"
    fi
done

echo ""
echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║         ✅ Release v1.0.1 criado com sucesso!             ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}🔗 URL:${NC} https://github.com/$OWNER/$REPO/releases/tag/$TAG"
echo ""
echo -e "${BLUE}📦 Binários anexados:${NC}"
echo "  - k8s-hpa-manager-linux-amd64"
echo "  - k8s-hpa-manager-darwin-amd64"
echo "  - k8s-hpa-manager-darwin-arm64"
echo "  - k8s-hpa-manager-windows-amd64.exe"
echo ""
