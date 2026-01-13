#!/bin/bash
#
# Script de diagnóstico para certificados CA customizados
# Verifica se os certificados estão configurados corretamente para Health Check
#

set -e

# Cores para output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Diagnóstico de Certificados CA Customizados ===${NC}\n"

# 1. Verificar variável de ambiente
echo -e "${BLUE}1. Verificando variável de ambiente CUSTOM_CA_BUNDLE...${NC}"
if [ -n "$CUSTOM_CA_BUNDLE" ]; then
    echo -e "${GREEN}✓ CUSTOM_CA_BUNDLE definida: $CUSTOM_CA_BUNDLE${NC}"
    if [ -f "$CUSTOM_CA_BUNDLE" ]; then
        echo -e "${GREEN}✓ Arquivo existe${NC}"
    else
        echo -e "${RED}✗ Arquivo não encontrado: $CUSTOM_CA_BUNDLE${NC}"
    fi
else
    echo -e "${YELLOW}⚠ CUSTOM_CA_BUNDLE não definida (usando locais padrão)${NC}"
fi
echo ""

# 2. Verificar locais padrão
echo -e "${BLUE}2. Verificando locais padrão de certificados...${NC}"
POSSIBLE_PATHS=(
    "/etc/ssl/certs/ca-certificates.crt"
    "/etc/pki/tls/certs/ca-bundle.crt"
    "/etc/ssl/ca-bundle.pem"
    "/usr/local/share/ca-certificates/ca-bundle.crt"
    "./ca-certificates.crt"
)

FOUND_CA=""
for path in "${POSSIBLE_PATHS[@]}"; do
    if [ -f "$path" ]; then
        echo -e "${GREEN}✓ Encontrado: $path${NC}"
        if [ -z "$FOUND_CA" ]; then
            FOUND_CA="$path"
        fi
    else
        echo -e "${YELLOW}  Não encontrado: $path${NC}"
    fi
done
echo ""

# 3. Validar formato PEM
if [ -n "$FOUND_CA" ] || [ -n "$CUSTOM_CA_BUNDLE" ]; then
    CERT_FILE="${CUSTOM_CA_BUNDLE:-$FOUND_CA}"
    echo -e "${BLUE}3. Validando formato PEM do certificado: $CERT_FILE${NC}"

    if grep -q "BEGIN CERTIFICATE" "$CERT_FILE"; then
        CERT_COUNT=$(grep -c "BEGIN CERTIFICATE" "$CERT_FILE")
        echo -e "${GREEN}✓ Formato PEM válido ($CERT_COUNT certificados encontrados)${NC}"

        # Tentar validar com openssl
        if command -v openssl &> /dev/null; then
            if openssl x509 -in "$CERT_FILE" -text -noout &> /dev/null; then
                echo -e "${GREEN}✓ Certificado validado pelo OpenSSL${NC}"
            else
                echo -e "${YELLOW}⚠ OpenSSL reportou problemas (pode ser bundle com múltiplos certificados)${NC}"
            fi
        fi
    else
        echo -e "${RED}✗ Formato PEM inválido (não contém 'BEGIN CERTIFICATE')${NC}"
        echo -e "${YELLOW}  Talvez seja DER? Tente converter: openssl x509 -inform DER -in cert.der -out cert.pem${NC}"
    fi
else
    echo -e "${RED}✗ Nenhum certificado CA encontrado${NC}"
    echo -e "${YELLOW}  Recomendação: Instalar certificado CA da empresa${NC}"
    echo -e "${YELLOW}  Ver: docs/guides/CUSTOM_CA_CERTIFICATES.md${NC}"
fi
echo ""

# 4. Testar conectividade TLS
echo -e "${BLUE}4. Testando conectividade TLS com endpoint problemático...${NC}"
TEST_URL="${1:-https://login-entrega.viavarejo.com.br/}"

echo -e "${BLUE}   URL de teste: $TEST_URL${NC}"

# Testar com curl (usa system cert pool + custom CA)
if command -v curl &> /dev/null; then
    echo -n "   curl (sem custom CA): "
    if curl -s -o /dev/null -w "%{http_code}" "$TEST_URL" &> /dev/null; then
        echo -e "${GREEN}✓ Sucesso${NC}"
    else
        echo -e "${RED}✗ Falhou${NC}"
    fi

    if [ -n "$CERT_FILE" ]; then
        echo -n "   curl (com custom CA): "
        if curl --cacert "$CERT_FILE" -s -o /dev/null -w "%{http_code}" "$TEST_URL" &> /dev/null; then
            echo -e "${GREEN}✓ Sucesso${NC}"
        else
            echo -e "${RED}✗ Falhou${NC}"
        fi
    fi
fi

# Testar com openssl s_client
if command -v openssl &> /dev/null; then
    echo -n "   openssl s_client: "
    if echo | openssl s_client -connect "$(echo "$TEST_URL" | sed 's|https://||' | sed 's|/||'):443" -servername "$(echo "$TEST_URL" | sed 's|https://||' | sed 's|/||')" &> /dev/null; then
        echo -e "${GREEN}✓ Conexão TLS estabelecida${NC}"
    else
        echo -e "${RED}✗ Falha na conexão TLS${NC}"
    fi
fi
echo ""

# 5. Informações sobre o certificado do servidor
echo -e "${BLUE}5. Informações do certificado do servidor...${NC}"
if command -v openssl &> /dev/null; then
    HOST=$(echo "$TEST_URL" | sed 's|https://||' | sed 's|/||')
    echo -e "${BLUE}   Obtendo certificado de $HOST...${NC}"

    CERT_INFO=$(echo | openssl s_client -showcerts -connect "$HOST:443" -servername "$HOST" 2>/dev/null | openssl x509 -noout -subject -issuer -dates 2>/dev/null || echo "")

    if [ -n "$CERT_INFO" ]; then
        echo "$CERT_INFO" | while IFS= read -r line; do
            echo -e "${GREEN}   $line${NC}"
        done
    else
        echo -e "${RED}   ✗ Não foi possível obter certificado do servidor${NC}"
    fi
fi
echo ""

# 6. Recomendações
echo -e "${BLUE}=== Recomendações ===${NC}"

if [ -z "$FOUND_CA" ] && [ -z "$CUSTOM_CA_BUNDLE" ]; then
    echo -e "${YELLOW}⚠ Nenhum certificado CA customizado encontrado${NC}"
    echo ""
    echo "Passos recomendados:"
    echo "1. Obter o certificado CA raiz da Via Varejo:"
    echo "   - Exportar do navegador (acesse $TEST_URL → certificado → exportar raiz)"
    echo "   - Ou usar OpenSSL: openssl s_client -showcerts -connect login-entrega.viavarejo.com.br:443 2>/dev/null | openssl x509 -outform PEM > viavarejo-ca.crt"
    echo ""
    echo "2. Instalar o certificado (escolha um método):"
    echo ""
    echo "   Opção A - Variável de ambiente (desenvolvimento):"
    echo "   export CUSTOM_CA_BUNDLE=/caminho/para/viavarejo-ca.crt"
    echo ""
    echo "   Opção B - Sistema (Debian/Ubuntu):"
    echo "   sudo cp viavarejo-ca.crt /usr/local/share/ca-certificates/"
    echo "   sudo update-ca-certificates"
    echo ""
    echo "   Opção C - Local (desenvolvimento):"
    echo "   cp viavarejo-ca.crt ./ca-certificates.crt"
    echo ""
    echo "3. Reiniciar o servidor web:"
    echo "   ./build/new-k8s-hpa web"
    echo ""
elif [ -n "$FOUND_CA" ] || [ -n "$CUSTOM_CA_BUNDLE" ]; then
    echo -e "${GREEN}✓ Certificado CA encontrado: ${CERT_FILE}${NC}"
    echo ""
    echo "Se ainda houver erros de TLS:"
    echo "1. Verifique se o certificado é da CA raiz correta"
    echo "2. Verifique o formato PEM (deve conter 'BEGIN CERTIFICATE')"
    echo "3. Teste manualmente: curl --cacert $CERT_FILE $TEST_URL"
    echo "4. Verifique logs do backend: tail -f /tmp/k8s-hpa-manager-web-*.log | grep -i 'ca\|tls\|certificate'"
fi

echo ""
echo -e "${BLUE}Documentação completa: docs/guides/CUSTOM_CA_CERTIFICATES.md${NC}"
