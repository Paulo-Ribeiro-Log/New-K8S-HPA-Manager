#!/bin/bash

# Script de Validação da Estrutura Web
# Verifica se todos os arquivos foram reorganizados corretamente

echo "╔═══════════════════════════════════════════════════════════════╗"
echo "║           Validação da Estrutura Web - k8s-hpa-manager       ║"
echo "╚═══════════════════════════════════════════════════════════════╝"
echo ""

ERRORS=0

# Função para verificar existência de arquivo/diretório
check_exists() {
    if [ -e "$1" ]; then
        echo "✅ $1"
    else
        echo "❌ FALTANDO: $1"
        ((ERRORS++))
    fi
}

# Função para verificar que NÃO existe
check_not_exists() {
    if [ ! -e "$1" ]; then
        echo "✅ $1 (não existe, como esperado)"
    else
        echo "⚠️  ATENÇÃO: $1 ainda existe (deveria ser removido)"
    fi
}

echo "📁 Verificando estrutura de diretórios..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

check_exists "internal/web/frontend"
check_exists "internal/web/frontend/src"
check_exists "internal/web/frontend/public"
check_exists "internal/web/static"
check_exists "internal/web/static/.gitkeep"

echo ""
echo "📄 Verificando arquivos principais do frontend..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

check_exists "internal/web/frontend/package.json"
check_exists "internal/web/frontend/vite.config.ts"
check_exists "internal/web/frontend/tsconfig.json"
check_exists "internal/web/frontend/tailwind.config.ts"
check_exists "internal/web/frontend/.gitignore"
check_exists "internal/web/frontend/README.md"

echo ""
echo "📄 Verificando código fonte..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

check_exists "internal/web/frontend/src/App.tsx"
check_exists "internal/web/frontend/src/main.tsx"
check_exists "internal/web/frontend/src/index.css"
check_exists "internal/web/frontend/src/pages/Index.tsx"
check_exists "internal/web/frontend/src/components/Header.tsx"
check_exists "internal/web/frontend/src/lib/utils.ts"

echo ""
echo "🔧 Verificando configurações do projeto..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

check_exists "makefile"
check_exists ".gitignore"
check_exists "CLAUDE.md"

echo ""
echo "📚 Verificando documentação..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

check_exists "Docs/WEB_INTEGRATION.md"
check_exists "REORGANIZATION_SUMMARY.md"

echo ""
echo "🗑️  Verificando limpeza..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

check_not_exists "new-web-page"

echo ""
echo "🔍 Verificando Vite config..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if grep -q 'outDir: "../static"' internal/web/frontend/vite.config.ts; then
    echo "✅ Vite build output configurado corretamente"
else
    echo "❌ Vite build output NÃO está configurado para ../static"
    ((ERRORS++))
fi

echo ""
echo "🔍 Verificando .gitignore..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if grep -q 'new-web-page/' .gitignore; then
    echo "✅ new-web-page/ está no .gitignore"
else
    echo "❌ new-web-page/ NÃO está no .gitignore"
    ((ERRORS++))
fi

if grep -q 'internal/web/static/\*' .gitignore; then
    echo "✅ internal/web/static/* está no .gitignore"
else
    echo "❌ internal/web/static/* NÃO está no .gitignore"
    ((ERRORS++))
fi

echo ""
echo "🔍 Verificando Makefile..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

for target in web-install web-dev web-build web-clean build-web; do
    if grep -q "^\.PHONY: $target" makefile; then
        echo "✅ Target '$target' encontrado"
    else
        echo "❌ Target '$target' NÃO encontrado"
        ((ERRORS++))
    fi
done

echo ""
echo "╔═══════════════════════════════════════════════════════════════╗"
if [ $ERRORS -eq 0 ]; then
    echo "║               ✅ VALIDAÇÃO COMPLETA COM SUCESSO! ✅           ║"
    echo "╚═══════════════════════════════════════════════════════════════╝"
    echo ""
    echo "🎉 Todos os arquivos estão no lugar correto!"
    echo "🚀 Estrutura pronta para desenvolvimento."
    echo ""
    echo "📋 Próximos passos:"
    echo "   1. make web-install    # Instalar dependências"
    echo "   2. make web-dev        # Dev server"
    echo "   3. make build-web      # Build produção"
    exit 0
else
    echo "║              ⚠️  VALIDAÇÃO COM ERROS: $ERRORS ⚠️               ║"
    echo "╚═══════════════════════════════════════════════════════════════╝"
    echo ""
    echo "❌ Foram encontrados $ERRORS problema(s)."
    echo "📝 Revise os erros acima e corrija antes de prosseguir."
    exit 1
fi
