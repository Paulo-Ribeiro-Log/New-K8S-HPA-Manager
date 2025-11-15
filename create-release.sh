#!/bin/bash

# Script genérico para criar releases no GitHub
# Uso: ./create-release.sh [versão]
# Exemplo: ./create-release.sh 1.0.5

set -e

OWNER="Paulo-Ribeiro-Log"
REPO="New-K8S-HPA-Manager"

# Função para buscar token
find_github_token() {
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    
    # 1. Verificar variável de ambiente
    if [ -n "$GITHUB_TOKEN" ]; then
        return 0
    fi
    
    # 2. Procurar em github_token.txt
    if [ -f "$SCRIPT_DIR/github_token.txt" ]; then
        export GITHUB_TOKEN=$(cat "$SCRIPT_DIR/github_token.txt")
        return 0
    fi
    
    # 3. Procurar em .env
    if [ -f "$SCRIPT_DIR/.env" ]; then
        source "$SCRIPT_DIR/.env"
        return 0
    fi
    
    # 4. Procurar em secrets.sh
    if [ -f "$SCRIPT_DIR/secrets.sh" ]; then
        source "$SCRIPT_DIR/secrets.sh"
        return 0
    fi
    
    return 1
}

# Buscar token
if ! find_github_token; then
    echo "❌ GITHUB_TOKEN não encontrado"
    echo ""
    echo "═══════════════════════════════════════════════════════════"
    echo "  Configure seu GitHub Token"
    echo "═══════════════════════════════════════════════════════════"
    echo ""
    echo "📚 MÉTODO RECOMENDADO:"
    echo "  ./setup-github-token.sh"
    echo ""
    echo "📖 Documentação: GITHUB_TOKEN_SETUP.md"
    echo ""
    exit 1
fi

# Obter versão
if [ -z "$1" ]; then
    # Tentar detectar versão do git tag
    VERSION=$(git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')
    if [ -z "$VERSION" ]; then
        echo "❌ Versão não especificada e não detectada via git tags"
        echo ""
        echo "Uso: $0 <versão>"
        echo "Exemplo: $0 1.0.5"
        echo ""
        echo "Ou crie uma tag git primeiro:"
        echo "  git tag v1.0.5"
        echo "  $0"
        exit 1
    fi
    echo "ℹ️  Versão detectada via git tag: $VERSION"
else
    VERSION="$1"
fi

TAG="v$VERSION"
RELEASE_NOTES_FILE="RELEASE_NOTES_v${VERSION}.md"

# Verificar se release notes existe
if [ ! -f "$RELEASE_NOTES_FILE" ]; then
    echo "❌ Arquivo de release notes não encontrado: $RELEASE_NOTES_FILE"
    echo ""
    echo "Crie o arquivo primeiro com as notas da release."
    exit 1
fi

# Detectar release name do arquivo de release notes (primeira linha após #)
RELEASE_NAME=$(grep -m 1 "^# Release" "$RELEASE_NOTES_FILE" | sed 's/^# Release //')
if [ -z "$RELEASE_NAME" ]; then
    RELEASE_NAME="v${VERSION}"
fi

echo "════════════════════════════════════════════════════════════"
echo "  Criando Release no GitHub"
echo "════════════════════════════════════════════════════════════"
echo ""
echo "📦 Versão: $VERSION"
echo "🏷️  Tag: $TAG"
echo "📝 Release name: $RELEASE_NAME"
echo "📄 Release notes: $RELEASE_NOTES_FILE"
echo ""

# Verificar se binários existem
BINARIES=(
    "build/release/new-k8s-hpa-linux-amd64"
    "build/release/new-k8s-hpa-darwin-amd64"
    "build/release/new-k8s-hpa-darwin-arm64"
)

missing_binaries=()
for binary in "${BINARIES[@]}"; do
    if [ ! -f "$binary" ]; then
        missing_binaries+=("$binary")
    fi
done

if [ ${#missing_binaries[@]} -gt 0 ]; then
    echo "⚠️  Binários ausentes:"
    for binary in "${missing_binaries[@]}"; do
        echo "  • $binary"
    done
    echo ""
    echo "Execute: make release"
    echo ""
    read -p "Deseja continuar sem upload de binários? (s/N): " -r continue_without_binaries
    if [[ ! $continue_without_binaries =~ ^[Ss]$ ]]; then
        exit 1
    fi
    echo ""
fi

# Confirmar antes de criar
read -p "Criar release $TAG no GitHub? (S/n): " -r confirm
if [[ $confirm =~ ^[Nn]$ ]]; then
    echo "❌ Cancelado"
    exit 0
fi

echo ""
echo "🚀 Criando release $TAG no GitHub..."

# Ler release notes
RELEASE_NOTES=$(cat "$RELEASE_NOTES_FILE" | jq -Rs .)

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

# Upload binários (se existirem)
if [ ${#missing_binaries[@]} -eq 0 ]; then
    UPLOAD_URL=$(echo "$RESPONSE" | jq -r '.upload_url' | sed 's/{?name,label}//')
    
    echo ""
    echo "📤 Fazendo upload dos binários..."
    
    for binary in "${BINARIES[@]}"; do
        FILENAME=$(basename "$binary")
        echo "  • Uploading $FILENAME..."
        
        curl -s -X POST \
          -H "Accept: application/vnd.github+json" \
          -H "Authorization: Bearer $GITHUB_TOKEN" \
          -H "X-GitHub-Api-Version: 2022-11-28" \
          -H "Content-Type: application/octet-stream" \
          "$UPLOAD_URL?name=$FILENAME" \
          --data-binary "@$binary" > /dev/null
        
        echo "    ✅ $FILENAME uploaded"
    done
fi

echo ""
echo "════════════════════════════════════════════════════════════"
echo "  ✅ Release publicada com sucesso!"
echo "════════════════════════════════════════════════════════════"
echo ""
echo "🔗 URL: https://github.com/$OWNER/$REPO/releases/tag/$TAG"
echo ""
echo "📋 Próximos passos:"
echo "  1. Verificar release: https://github.com/$OWNER/$REPO/releases"
echo "  2. Testar instalação: curl ... | bash"
echo "  3. Anunciar nova versão"
echo ""
