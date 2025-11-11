#!/bin/bash

# Script de restore para k8s-hpa-manager
# Uso: ./restore.sh [nome_do_backup]

# Cores para output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Verificar se foi fornecido o nome do backup
if [ -z "$1" ]; then
    echo -e "${RED}❌ Erro: Nome do backup não fornecido${NC}"
    echo ""
    echo "Uso: ./restore.sh [nome_do_backup]"
    echo ""
    echo "Backups disponíveis:"
    ls -1d backups/backup_* 2>/dev/null | sed 's|backups/||' | nl
    exit 1
fi

BACKUP_NAME="$1"
BACKUP_DIR="backups/$BACKUP_NAME"

# Verificar se o backup existe
if [ ! -d "$BACKUP_DIR" ]; then
    echo -e "${RED}❌ Erro: Backup não encontrado: $BACKUP_DIR${NC}"
    echo ""
    echo "Backups disponíveis:"
    ls -1d backups/backup_* 2>/dev/null | sed 's|backups/||' | nl
    exit 1
fi

# Mostrar informações do backup
echo -e "${BLUE}📦 Informações do Backup:${NC}"
if [ -f "$BACKUP_DIR/backup_info.txt" ]; then
    cat "$BACKUP_DIR/backup_info.txt"
else
    echo "Backup criado em: $(stat -c %y "$BACKUP_DIR" | cut -d'.' -f1)"
fi
echo ""

# Confirmar restauração
echo -e "${YELLOW}⚠️  ATENÇÃO: Esta operação irá sobrescrever os arquivos atuais!${NC}"
echo ""
read -p "Deseja continuar com a restauração? (sim/não): " CONFIRM

if [ "$CONFIRM" != "sim" ]; then
    echo -e "${BLUE}ℹ️  Restauração cancelada${NC}"
    exit 0
fi

# Criar backup do estado atual antes de restaurar
echo ""
echo -e "${BLUE}📦 Criando backup do estado atual antes da restauração...${NC}"
./backup.sh "pre_restore_$(date +%H%M%S)"

# Restaurar arquivos
echo ""
echo -e "${BLUE}🔄 Restaurando arquivos...${NC}"

# Remover arquivos atuais
rm -rf internal cmd main.go go.mod go.sum makefile CLAUDE.md 2>/dev/null

# Copiar arquivos do backup
cp -r "$BACKUP_DIR"/* .

# Recompilar
echo ""
echo -e "${BLUE}🔨 Recompilando...${NC}"
make build

if [ $? -eq 0 ]; then
    echo ""
    echo -e "${GREEN}✅ Restauração concluída com sucesso!${NC}"
    echo ""
    echo "📍 Backup restaurado: $BACKUP_NAME"
    echo "🔨 Binário recompilado em: ./build/k8s-hpa-manager"
else
    echo ""
    echo -e "${RED}❌ Erro durante a compilação${NC}"
    echo "Os arquivos foram restaurados, mas houve um problema na compilação."
    exit 1
fi
