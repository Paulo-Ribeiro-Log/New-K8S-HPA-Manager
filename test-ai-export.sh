#!/bin/bash
# Script para testar exportação de relatórios AI Diagnostics
# Uso: ./test-ai-export.sh <analysis_id>

set -e

ANALYSIS_ID="${1:-}"
API_URL="http://localhost:8080/api/v1"

if [ -z "$ANALYSIS_ID" ]; then
    echo "❌ Erro: Analysis ID não fornecido"
    echo "Uso: $0 <analysis_id>"
    echo ""
    echo "Para obter IDs disponíveis, execute:"
    echo "  curl -s $API_URL/ai/history | jq -r '.[] | .id + \" - \" + .resource_name'"
    exit 1
fi

echo "🔍 Testando exportação de AI Diagnostics..."
echo "Analysis ID: $ANALYSIS_ID"
echo ""

# Criar diretório de saída
OUTPUT_DIR="./test-exports"
mkdir -p "$OUTPUT_DIR"

# Timestamp para nomes de arquivo
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

# Testar PDF
echo "📄 Testando exportação PDF..."
PDF_FILE="$OUTPUT_DIR/diagnostico_${TIMESTAMP}.pdf"
curl -s -o "$PDF_FILE" "$API_URL/ai/report/$ANALYSIS_ID/pdf"

if [ $? -eq 0 ] && [ -f "$PDF_FILE" ] && [ -s "$PDF_FILE" ]; then
    SIZE=$(ls -lh "$PDF_FILE" | awk '{print $5}')
    echo "✅ PDF gerado com sucesso: $PDF_FILE ($SIZE)"

    # Verificar se é um PDF válido
    if file "$PDF_FILE" | grep -q "PDF"; then
        echo "   ✅ Arquivo é um PDF válido"
    else
        echo "   ⚠️  Arquivo pode não ser um PDF válido"
        echo "   Conteúdo: $(head -c 100 "$PDF_FILE")"
    fi
else
    echo "❌ Falha ao gerar PDF"
    cat "$PDF_FILE" 2>/dev/null || echo "   (arquivo vazio ou não encontrado)"
    exit 1
fi

echo ""

# Testar Markdown
echo "📝 Testando exportação Markdown..."
MD_FILE="$OUTPUT_DIR/diagnostico_${TIMESTAMP}.md"
curl -s -o "$MD_FILE" "$API_URL/ai/report/$ANALYSIS_ID/markdown"

if [ $? -eq 0 ] && [ -f "$MD_FILE" ] && [ -s "$MD_FILE" ]; then
    SIZE=$(ls -lh "$MD_FILE" | awk '{print $5}')
    LINES=$(wc -l < "$MD_FILE")
    echo "✅ Markdown gerado com sucesso: $MD_FILE ($SIZE, $LINES linhas)"

    # Verificar estrutura do Markdown
    echo "   Verificando estrutura..."

    if grep -q "# ANALISE DE DIAGNOSTICO - AI" "$MD_FILE"; then
        echo "   ✅ Header principal encontrado"
    else
        echo "   ⚠️  Header principal não encontrado"
    fi

    if grep -q "## METADADOS DO RECURSO" "$MD_FILE"; then
        echo "   ✅ Seção METADADOS DO RECURSO encontrada"
    else
        echo "   ⚠️  Seção METADADOS DO RECURSO não encontrada"
    fi

    if grep -q "## SUMARIO EXECUTIVO" "$MD_FILE"; then
        echo "   ✅ Seção SUMARIO EXECUTIVO encontrada"
    else
        echo "   ⚠️  Seção SUMARIO EXECUTIVO não encontrada"
    fi

    if grep -q "## ANALISE DE CAUSA RAIZ" "$MD_FILE"; then
        echo "   ✅ Seção ANALISE DE CAUSA RAIZ encontrada"
    else
        echo "   ℹ️  Seção ANALISE DE CAUSA RAIZ não encontrada (pode ser opcional)"
    fi

    if grep -q "## ACOES RECOMENDADAS" "$MD_FILE"; then
        echo "   ✅ Seção ACOES RECOMENDADAS encontrada"
    else
        echo "   ℹ️  Seção ACOES RECOMENDADAS não encontrada (pode ser opcional)"
    fi

    # Mostrar preview
    echo ""
    echo "   📋 Preview (primeiras 30 linhas):"
    echo "   ────────────────────────────────────────"
    head -30 "$MD_FILE" | sed 's/^/   /'
    echo "   ────────────────────────────────────────"
else
    echo "❌ Falha ao gerar Markdown"
    cat "$MD_FILE" 2>/dev/null || echo "   (arquivo vazio ou não encontrado)"
    exit 1
fi

echo ""

# Testar CSV
echo "📊 Testando exportação CSV..."
CSV_FILE="$OUTPUT_DIR/diagnostico_${TIMESTAMP}.csv"
curl -s -o "$CSV_FILE" "$API_URL/ai/report/$ANALYSIS_ID/csv"

if [ $? -eq 0 ] && [ -f "$CSV_FILE" ] && [ -s "$CSV_FILE" ]; then
    SIZE=$(ls -lh "$CSV_FILE" | awk '{print $5}')
    LINES=$(wc -l < "$CSV_FILE")
    echo "✅ CSV gerado com sucesso: $CSV_FILE ($SIZE, $LINES linhas)"

    # Mostrar preview
    echo ""
    echo "   📋 Preview (primeiras 20 linhas):"
    echo "   ────────────────────────────────────────"
    head -20 "$CSV_FILE" | sed 's/^/   /'
    echo "   ────────────────────────────────────────"
else
    echo "❌ Falha ao gerar CSV"
    cat "$CSV_FILE" 2>/dev/null || echo "   (arquivo vazio ou não encontrado)"
    exit 1
fi

echo ""
echo "✅ Todos os testes passaram com sucesso!"
echo "📁 Arquivos salvos em: $OUTPUT_DIR/"
echo ""
echo "Para visualizar:"
echo "  PDF:      xdg-open $PDF_FILE"
echo "  Markdown: cat $MD_FILE"
echo "  CSV:      cat $CSV_FILE | column -t -s,"
