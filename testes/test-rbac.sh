#!/bin/bash

# test-rbac.sh
# Script para testar a implementação de RBAC com Azure AD
# Autor: Gerado automaticamente
# Uso: ./test-rbac.sh

set -e

# Cores
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║          RBAC Azure AD - Test Suite                   ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Teste 1: Verificar usuário atual
echo -e "${YELLOW}[1/5] Verificando usuário logado...${NC}"
USER_EMAIL=$(az account show --query user.name -o tsv 2>&1)
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ Usuário logado: $USER_EMAIL${NC}"
else
    echo -e "${RED}❌ Erro ao obter usuário logado${NC}"
    exit 1
fi
echo ""

# Teste 2: Buscar grupo VV_CLOUD_SRE
echo -e "${YELLOW}[2/5] Buscando grupo VV_CLOUD_SRE...${NC}"
GROUP_INFO=$(az ad group show --group vv_cloud_sre --query '{id:id,displayName:displayName}' -o json 2>&1)
if [ $? -eq 0 ]; then
    GROUP_ID=$(echo "$GROUP_INFO" | jq -r '.id')
    GROUP_NAME=$(echo "$GROUP_INFO" | jq -r '.displayName')
    echo -e "${GREEN}✅ Grupo encontrado:${NC}"
    echo -e "   Nome: $GROUP_NAME"
    echo -e "   ID:   $GROUP_ID"
else
    echo -e "${RED}❌ Erro ao buscar grupo VV_CLOUD_SRE${NC}"
    exit 1
fi
echo ""

# Teste 3: Obter grupos do usuário
echo -e "${YELLOW}[3/5] Obtendo grupos do usuário...${NC}"
USER_GROUPS=$(az ad user get-member-groups --id "$USER_EMAIL" -o json 2>&1)
if [ $? -eq 0 ]; then
    GROUP_COUNT=$(echo "$USER_GROUPS" | jq '. | length')
    echo -e "${GREEN}✅ Usuário pertence a $GROUP_COUNT grupos${NC}"
else
    echo -e "${RED}❌ Erro ao obter grupos do usuário${NC}"
    exit 1
fi
echo ""

# Teste 4: Verificar se usuário é SRE
echo -e "${YELLOW}[4/5] Verificando se usuário é SRE...${NC}"
IS_SRE=$(echo "$USER_GROUPS" | jq '.[] | select(.displayName == "VV_CLOUD_SRE") | .displayName' -r)
if [ -n "$IS_SRE" ]; then
    echo -e "${GREEN}✅ Usuário É MEMBRO do grupo VV_CLOUD_SRE${NC}"
    IS_SRE_FLAG=true
else
    echo -e "${YELLOW}⚠️  Usuário NÃO é membro do grupo VV_CLOUD_SRE${NC}"
    IS_SRE_FLAG=false
fi
echo ""

# Teste 5: Testar módulo Go
echo -e "${YELLOW}[5/5] Testando módulo Go RBAC...${NC}"
cd "$(dirname "$0")/.."
if go test -v ./internal/rbac -run TestCheckCurrentUserIsSRE 2>&1 | tee /tmp/rbac-test.log; then
    echo -e "${GREEN}✅ Testes Go passaram com sucesso${NC}"
else
    echo -e "${RED}❌ Testes Go falharam${NC}"
    echo -e "${YELLOW}Veja detalhes em: /tmp/rbac-test.log${NC}"
    exit 1
fi
echo ""

# Resumo
echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║                     RESUMO                             ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "Usuário:          ${GREEN}$USER_EMAIL${NC}"
echo -e "Grupo VV_CLOUD_SRE:  ${GREEN}$GROUP_ID${NC}"
echo -e "Total de grupos:  ${GREEN}$GROUP_COUNT${NC}"
echo -e "Status SRE:       $(if [ "$IS_SRE_FLAG" = true ]; then echo -e "${GREEN}✅ SIM${NC}"; else echo -e "${YELLOW}⚠️  NÃO${NC}"; fi)"
echo ""

if [ "$IS_SRE_FLAG" = true ]; then
    echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║  ✅ TESTE COMPLETO - Usuário tem permissões de SRE    ║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
else
    echo -e "${YELLOW}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${YELLOW}║  ⚠️  Usuário não é SRE - acesso somente leitura       ║${NC}"
    echo -e "${YELLOW}╚════════════════════════════════════════════════════════════╝${NC}"
fi

echo ""
echo -e "${BLUE}Próximos passos:${NC}"
echo -e "  1. Iniciar servidor: ${GREEN}./build/new-k8s-hpa web${NC}"
echo -e "  2. Acessar: ${GREEN}http://localhost:8080${NC}"
echo -e "  3. Verificar endpoint: ${GREEN}curl http://localhost:8080/api/v1/permissions${NC}"
echo ""
