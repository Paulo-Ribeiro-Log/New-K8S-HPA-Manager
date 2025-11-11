#!/bin/bash

# Script de backup automático para k8s-hpa-manager
# Uso: ./backup.sh [descrição opcional]

# Cores para output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Obter descrição opcional
DESCRIPTION="${1:-manual}"
DESCRIPTION=$(echo "$DESCRIPTION" | tr ' ' '_')

# Criar nome do backup com timestamp
BACKUP_NAME="backup_$(date +%Y%m%d_%H%M%S)_${DESCRIPTION}"
BACKUP_DIR="backups/$BACKUP_NAME"

# Criar diretório de backup
mkdir -p "$BACKUP_DIR"

# Copiar arquivos principais
echo -e "${BLUE}📦 Criando backup...${NC}"
cp -r internal cmd main.go go.mod go.sum makefile "$BACKUP_DIR/" 2>/dev/null

# Copiar CLAUDE.md se existir
if [ -f "CLAUDE.md" ]; then
    cp CLAUDE.md "$BACKUP_DIR/"
fi

# Criar arquivo de metadados
cat > "$BACKUP_DIR/backup_info.txt" << EOF
Backup criado em: $(date '+%Y-%m-%d %H:%M:%S')
Descrição: $DESCRIPTION
Branch atual: $(git branch --show-current 2>/dev/null || echo "N/A")
Último commit: $(git log -1 --oneline 2>/dev/null || echo "N/A")
Usuário: $(whoami)
Hostname: $(hostname)
EOF

# Calcular tamanho do backup
BACKUP_SIZE=$(du -sh "$BACKUP_DIR" | cut -f1)

# Mostrar resultado
echo -e "${GREEN}✅ Backup criado com sucesso!${NC}"
echo ""
echo "📍 Localização: $BACKUP_DIR"
echo "📊 Tamanho: $BACKUP_SIZE"
echo ""
echo "Arquivos incluídos:"
ls -1 "$BACKUP_DIR"
echo ""
echo "ℹ️  Para restaurar este backup, use: ./restore.sh $BACKUP_NAME"

# Limpar backups antigos (manter apenas os 10 mais recentes)
BACKUP_COUNT=$(ls -1d backups/backup_* 2>/dev/null | wc -l)
if [ "$BACKUP_COUNT" -gt 10 ]; then
    echo ""
    echo "🧹 Limpando backups antigos (mantendo os 10 mais recentes)..."
    ls -1td backups/backup_* | tail -n +11 | xargs rm -rf
    echo "✅ Limpeza concluída"
fi
