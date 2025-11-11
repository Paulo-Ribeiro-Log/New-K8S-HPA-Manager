#!/bin/bash

# Script para visualizar dados do banco de monitoramento de forma legível
# Localização: ~/.k8s-hpa-manager/monitoring.db

set -e

DB_PATH="$HOME/.k8s-hpa-manager/monitoring.db"

# Cores para output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Função para exibir header
print_header() {
    echo -e "\n${BOLD}${BLUE}═══════════════════════════════════════════════════════════════${NC}"
    echo -e "${BOLD}${BLUE}  $1${NC}"
    echo -e "${BOLD}${BLUE}═══════════════════════════════════════════════════════════════${NC}\n"
}

# Função para exibir seção
print_section() {
    echo -e "\n${BOLD}${GREEN}▶ $1${NC}\n"
}

# Verifica se banco existe
if [ ! -f "$DB_PATH" ]; then
    echo -e "${RED}❌ Banco de dados não encontrado: $DB_PATH${NC}"
    echo -e "${YELLOW}💡 Execute a aplicação primeiro para criar o banco.${NC}"
    exit 1
fi

print_header "📊 MONITORAMENTO HPA-WATCHDOG - VISUALIZADOR DE BANCO DE DADOS"

echo -e "${BOLD}📁 Localização:${NC} $DB_PATH"
echo -e "${BOLD}📏 Tamanho:${NC} $(du -h "$DB_PATH" | cut -f1)"
echo -e "${BOLD}📅 Última modificação:${NC} $(stat -c %y "$DB_PATH" | cut -d'.' -f1)"

# Estatísticas gerais
print_header "📈 ESTATÍSTICAS GERAIS"

echo -e "${BOLD}Total de snapshots:${NC}"
sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM hpa_snapshots;" 2>/dev/null || echo "0"

echo -e "\n${BOLD}Snapshots por cluster:${NC}"
sqlite3 "$DB_PATH" "SELECT cluster, COUNT(*) as total FROM hpa_snapshots GROUP BY cluster ORDER BY total DESC;" -header -column 2>/dev/null || echo "Nenhum dado"

echo -e "\n${BOLD}HPAs únicos monitorados:${NC}"
sqlite3 "$DB_PATH" "SELECT COUNT(DISTINCT cluster || '/' || namespace || '/' || hpa_name) FROM hpa_snapshots;" 2>/dev/null || echo "0"

# Status de baseline
print_header "📋 STATUS DE BASELINE (Coleta Histórica 3 dias)"

echo -e "${BOLD}HPAs com baseline pronto:${NC}"
sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM hpa_snapshots WHERE baseline_ready = 1;" 2>/dev/null || echo "0"

echo -e "\n${BOLD}HPAs aguardando baseline:${NC}"
sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM hpa_snapshots WHERE baseline_ready = 0 OR baseline_ready IS NULL;" 2>/dev/null || echo "0"

echo -e "\n${BOLD}Últimos baselines coletados:${NC}"
sqlite3 "$DB_PATH" <<EOF -header -column 2>/dev/null || echo "Nenhum dado"
SELECT
    cluster,
    namespace,
    hpa_name,
    datetime(last_baseline_scan, 'localtime') as ultimo_scan,
    CASE baseline_ready
        WHEN 1 THEN '✅ Pronto'
        ELSE '⏳ Pendente'
    END as status
FROM hpa_snapshots
WHERE last_baseline_scan IS NOT NULL
ORDER BY last_baseline_scan DESC
LIMIT 10;
EOF

# Snapshots recentes
print_header "📸 SNAPSHOTS RECENTES (Últimos 20)"

sqlite3 "$DB_PATH" <<EOF -header -column 2>/dev/null || echo "Nenhum dado"
SELECT
    cluster,
    namespace,
    hpa_name,
    min_replicas as min,
    max_replicas as max,
    current_replicas as atual,
    target_cpu_percent as cpu_target,
    datetime(timestamp, 'localtime') as timestamp
FROM hpa_snapshots
ORDER BY timestamp DESC
LIMIT 20;
EOF

# HPAs com baseline completo
print_header "✅ HPAs COM BASELINE COMPLETO"

sqlite3 "$DB_PATH" <<EOF -header -column 2>/dev/null || echo "Nenhum HPA com baseline completo ainda"
SELECT
    cluster,
    namespace,
    hpa_name,
    datetime(last_baseline_scan, 'localtime') as baseline_scan,
    ROUND((julianday('now') - julianday(last_baseline_scan)) * 24, 1) as horas_desde_scan
FROM hpa_snapshots
WHERE baseline_ready = 1
ORDER BY last_baseline_scan DESC;
EOF

# Métricas de baseline (se houver)
print_header "📊 PREVIEW DE MÉTRICAS DE BASELINE"

echo -e "${YELLOW}Mostrando primeiras 100 linhas de metrics_json (se houver dados)...${NC}\n"

sqlite3 "$DB_PATH" <<EOF 2>/dev/null || echo "Nenhuma métrica de baseline salva ainda"
SELECT
    cluster || '/' || namespace || '/' || hpa_name as hpa,
    SUBSTR(metrics_json, 1, 100) as preview_metricas
FROM hpa_snapshots
WHERE metrics_json IS NOT NULL AND metrics_json != ''
LIMIT 5;
EOF

# Estatísticas de timestamps
print_header "⏰ ESTATÍSTICAS DE TIMESTAMPS"

sqlite3 "$DB_PATH" <<EOF -header -column 2>/dev/null || echo "Nenhum dado"
SELECT
    'Primeiro snapshot' as tipo,
    datetime(MIN(timestamp), 'localtime') as data
FROM hpa_snapshots
UNION ALL
SELECT
    'Último snapshot' as tipo,
    datetime(MAX(timestamp), 'localtime') as data
FROM hpa_snapshots;
EOF

# Schema da tabela
print_header "🗂️  SCHEMA DA TABELA hpa_snapshots"

sqlite3 "$DB_PATH" ".schema hpa_snapshots" 2>/dev/null || echo "Erro ao ler schema"

# Menu interativo (opcional)
print_header "🔧 CONSULTAS CUSTOMIZADAS"

echo -e "${BOLD}Opções disponíveis:${NC}"
echo -e "  ${GREEN}1)${NC} Ver todos os HPAs de um cluster específico"
echo -e "  ${GREEN}2)${NC} Ver histórico completo de um HPA"
echo -e "  ${GREEN}3)${NC} Exportar dados para CSV"
echo -e "  ${GREEN}4)${NC} Consulta SQL customizada"
echo -e "  ${GREEN}5)${NC} Sair"
echo ""

read -p "Escolha uma opção (1-5): " option

case $option in
    1)
        read -p "Digite o nome do cluster: " cluster_name
        print_section "HPAs do cluster: $cluster_name"
        sqlite3 "$DB_PATH" <<EOF -header -column
SELECT
    namespace,
    hpa_name,
    min_replicas,
    max_replicas,
    current_replicas,
    CASE baseline_ready WHEN 1 THEN '✅' ELSE '⏳' END as baseline,
    datetime(timestamp, 'localtime') as ultimo_snapshot
FROM hpa_snapshots
WHERE cluster = '$cluster_name'
ORDER BY namespace, hpa_name, timestamp DESC;
EOF
        ;;
    2)
        read -p "Digite o cluster: " cluster_name
        read -p "Digite o namespace: " namespace_name
        read -p "Digite o nome do HPA: " hpa_name
        print_section "Histórico: $cluster_name/$namespace_name/$hpa_name"
        sqlite3 "$DB_PATH" <<EOF -header -column
SELECT
    datetime(timestamp, 'localtime') as data,
    min_replicas,
    max_replicas,
    current_replicas,
    target_cpu_percent,
    CASE baseline_ready WHEN 1 THEN '✅ Pronto' ELSE '⏳ Pendente' END as baseline_status
FROM hpa_snapshots
WHERE cluster = '$cluster_name'
  AND namespace = '$namespace_name'
  AND hpa_name = '$hpa_name'
ORDER BY timestamp DESC;
EOF
        ;;
    3)
        CSV_FILE="$HOME/monitoring-export-$(date +%Y%m%d-%H%M%S).csv"
        print_section "Exportando para: $CSV_FILE"
        sqlite3 "$DB_PATH" <<EOF
.headers on
.mode csv
.output $CSV_FILE
SELECT * FROM hpa_snapshots ORDER BY timestamp DESC;
.quit
EOF
        echo -e "${GREEN}✅ Exportado com sucesso!${NC}"
        echo -e "Arquivo: $CSV_FILE"
        ;;
    4)
        echo -e "${YELLOW}Digite sua consulta SQL (termine com ponto-e-vírgula):${NC}"
        read -p "> " custom_query
        sqlite3 "$DB_PATH" "$custom_query" -header -column
        ;;
    5)
        echo -e "${GREEN}👋 Até logo!${NC}"
        exit 0
        ;;
    *)
        echo -e "${RED}Opção inválida${NC}"
        ;;
esac

echo ""
print_header "✅ VISUALIZAÇÃO CONCLUÍDA"
echo -e "${YELLOW}💡 Execute novamente para ver dados atualizados${NC}\n"
