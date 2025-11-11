#!/bin/bash
# Quick Start para Interface Web POC
# Execute este script para continuar de onde paramos

set -e

echo "════════════════════════════════════════════════════════════"
echo "  k8s-hpa-manager - Web Interface POC - Quick Start"
echo "════════════════════════════════════════════════════════════"
echo ""

# Cores
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Verificar diretório
if [ ! -f "go.mod" ]; then
    echo -e "${RED}❌ Erro: Execute este script da raiz do projeto${NC}"
    exit 1
fi

echo -e "${BLUE}📍 Diretório correto detectado${NC}"
echo ""

# Verificar arquivos da POC
echo -e "${BLUE}🔍 Verificando arquivos da POC...${NC}"

files_to_check=(
    "internal/web/server.go"
    "internal/web/middleware/auth.go"
    "internal/web/handlers/clusters.go"
    "internal/web/handlers/namespaces.go"
    "internal/web/handlers/hpas.go"
    "internal/web/static/index.html"
    "cmd/web.go"
    "WEB_POC_STATUS.md"
    "WEB_INTERFACE_DESIGN.md"
    "CONTINUE_AQUI.md"
)

missing_files=0
for file in "${files_to_check[@]}"; do
    if [ -f "$file" ]; then
        echo -e "${GREEN}  ✓ $file${NC}"
    else
        echo -e "${RED}  ✗ $file${NC}"
        missing_files=$((missing_files + 1))
    fi
done

if [ $missing_files -gt 0 ]; then
    echo ""
    echo -e "${RED}❌ $missing_files arquivo(s) faltando!${NC}"
    echo -e "${YELLOW}💡 Leia WEB_POC_STATUS.md para entender o estado atual${NC}"
    exit 1
fi

echo ""
echo -e "${GREEN}✅ Todos os arquivos da POC presentes!${NC}"
echo ""

# Verificar dependências
echo -e "${BLUE}📦 Verificando dependências Go...${NC}"
if ! go mod verify > /dev/null 2>&1; then
    echo -e "${YELLOW}⚠️  Dependências desatualizadas, executando go mod tidy...${NC}"
    go mod tidy
    echo -e "${GREEN}✅ Dependências atualizadas${NC}"
else
    echo -e "${GREEN}✅ Dependências OK${NC}"
fi

echo ""

# Build
echo -e "${BLUE}🔨 Compilando aplicação...${NC}"
echo -e "${YELLOW}⏱️  Isso pode demorar 1-2 minutos...${NC}"
echo ""

if go build -o ./build/k8s-hpa-manager . ; then
    echo ""
    echo -e "${GREEN}✅ Build completo!${NC}"
    echo ""
else
    echo ""
    echo -e "${RED}❌ Build falhou!${NC}"
    echo ""
    echo -e "${YELLOW}💡 Tente estas alternativas:${NC}"
    echo "   1. go clean -cache && go build -o ./build/k8s-hpa-manager ."
    echo "   2. go build -gcflags=\"-N -l\" -o ./build/k8s-hpa-manager ."
    echo "   3. go build -i -o ./build/k8s-hpa-manager ."
    exit 1
fi

# Verificar binário
if [ ! -f "./build/k8s-hpa-manager" ]; then
    echo -e "${RED}❌ Binário não encontrado em ./build/${NC}"
    exit 1
fi

echo -e "${GREEN}📦 Binário gerado: ./build/k8s-hpa-manager${NC}"
echo ""

# Instruções de uso
echo "════════════════════════════════════════════════════════════"
echo "  🎉 POC Pronta para Teste!"
echo "════════════════════════════════════════════════════════════"
echo ""
echo -e "${BLUE}🚀 Para iniciar o servidor web:${NC}"
echo ""
echo "   ./build/k8s-hpa-manager web --port 8080"
echo ""
echo -e "${BLUE}🔐 Token padrão (POC):${NC}"
echo ""
echo "   poc-token-123"
echo ""
echo -e "${BLUE}🌐 Acessar via navegador:${NC}"
echo ""
echo "   http://localhost:8080"
echo ""
echo -e "${BLUE}🧪 Testar API com curl:${NC}"
echo ""
echo "   # Health check (sem auth)"
echo "   curl http://localhost:8080/health"
echo ""
echo "   # Clusters (com auth)"
echo "   curl -H 'Authorization: Bearer poc-token-123' \\"
echo "     http://localhost:8080/api/v1/clusters"
echo ""
echo "   # Namespaces (com auth)"
echo "   curl -H 'Authorization: Bearer poc-token-123' \\"
echo "     'http://localhost:8080/api/v1/namespaces?cluster=NOME_CLUSTER'"
echo ""
echo "   # HPAs (com auth)"
echo "   curl -H 'Authorization: Bearer poc-token-123' \\"
echo "     'http://localhost:8080/api/v1/hpas?cluster=NOME_CLUSTER&namespace=NAMESPACE'"
echo ""
echo -e "${BLUE}📚 Documentação:${NC}"
echo ""
echo "   - WEB_POC_STATUS.md      # Status detalhado da POC"
echo "   - WEB_INTERFACE_DESIGN.md # Design completo"
echo "   - CONTINUE_AQUI.md       # Guia de continuidade"
echo ""
echo -e "${BLUE}📝 Modo TUI (existente):${NC}"
echo ""
echo "   ./build/k8s-hpa-manager"
echo ""
echo "════════════════════════════════════════════════════════════"
echo ""
echo -e "${GREEN}✨ Tudo pronto! Execute o comando acima para iniciar.${NC}"
echo ""
