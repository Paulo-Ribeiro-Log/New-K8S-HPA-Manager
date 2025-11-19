#!/bin/bash
# Script para criar release v1.0.1 no GitHub
# Uso: ./create-release-v1.0.1.sh

set -e

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║          GitHub Release v1.0.1 Creator                   ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# 1. Verificar se gh está autenticado
echo -e "${BLUE}🔐 Verificando autenticação GitHub CLI...${NC}"
if ! gh auth status >/dev/null 2>&1; then
    echo -e "${YELLOW}⚠️  GitHub CLI não autenticado${NC}"
    echo ""
    echo -e "${BLUE}Para autenticar:${NC}"
    echo "  gh auth login"
    echo ""
    echo -e "${YELLOW}Ou crie a release manualmente em:${NC}"
    echo "  https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/new?tag=v1.0.1"
    echo ""
    echo -e "${BLUE}Binários disponíveis em:${NC}"
    echo "  ./build/release/"
    ls -lh build/release/
    exit 1
fi

echo -e "${GREEN}✅ GitHub CLI autenticado${NC}"
echo ""

# 2. Verificar se binários existem
echo -e "${BLUE}📦 Verificando binários...${NC}"
if [ ! -d "build/release" ]; then
    echo -e "${RED}❌ Diretório build/release não encontrado${NC}"
    echo ""
    echo -e "${YELLOW}Execute primeiro:${NC}"
    echo "  make release"
    exit 1
fi

BINARIES=(
    "k8s-hpa-manager-linux-amd64"
    "k8s-hpa-manager-darwin-amd64"
    "k8s-hpa-manager-darwin-arm64"
    "k8s-hpa-manager-windows-amd64.exe"
)

for binary in "${BINARIES[@]}"; do
    if [ ! -f "build/release/$binary" ]; then
        echo -e "${RED}❌ Binário não encontrado: $binary${NC}"
        exit 1
    fi
    echo -e "${GREEN}✅ $binary${NC}"
done

echo ""

# 3. Verificar se tag existe
echo -e "${BLUE}🏷️  Verificando tag v1.0.1...${NC}"
if ! git tag | grep -q "^v1.0.1$"; then
    echo -e "${RED}❌ Tag v1.0.1 não encontrada${NC}"
    echo ""
    echo -e "${YELLOW}Crie a tag primeiro:${NC}"
    echo '  git tag -a v1.0.1 -m "Release v1.0.1"'
    echo '  git push origin v1.0.1'
    exit 1
fi
echo -e "${GREEN}✅ Tag v1.0.1 encontrada${NC}"
echo ""

# 4. Verificar se release notes existe
echo -e "${BLUE}📝 Verificando release notes...${NC}"
if [ ! -f "RELEASE_NOTES_v1.0.1.md" ]; then
    echo -e "${RED}❌ RELEASE_NOTES_v1.0.1.md não encontrado${NC}"
    exit 1
fi
echo -e "${GREEN}✅ RELEASE_NOTES_v1.0.1.md encontrado${NC}"
echo ""

# 5. Criar release
echo -e "${BLUE}🚀 Criando release v1.0.1 no GitHub...${NC}"
echo ""

gh release create v1.0.1 \
  --title "v1.0.1 - Fix Build Vendor + Documentação Organizada" \
  --notes-file RELEASE_NOTES_v1.0.1.md \
  build/release/k8s-hpa-manager-linux-amd64 \
  build/release/k8s-hpa-manager-darwin-amd64 \
  build/release/k8s-hpa-manager-darwin-arm64 \
  build/release/k8s-hpa-manager-windows-amd64.exe

if [ $? -eq 0 ]; then
    echo ""
    echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║         ✅ Release v1.0.1 criado com sucesso!             ║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${BLUE}🔗 URL:${NC} https://github.com/Paulo-Ribeiro-Log/New-K8S-HPA-Manager/releases/tag/v1.0.1"
    echo ""
    echo -e "${BLUE}📦 Binários anexados:${NC}"
    echo "  - k8s-hpa-manager-linux-amd64 (89 MB)"
    echo "  - k8s-hpa-manager-darwin-amd64 (89 MB)"
    echo "  - k8s-hpa-manager-darwin-arm64 (86 MB)"
    echo "  - k8s-hpa-manager-windows-amd64.exe (89 MB)"
    echo ""
else
    echo ""
    echo -e "${RED}❌ Erro ao criar release${NC}"
    exit 1
fi
