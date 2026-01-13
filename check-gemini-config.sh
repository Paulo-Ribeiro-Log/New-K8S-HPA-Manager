#!/bin/bash
# Script de diagnóstico para configuração Gemini

echo "🔍 Verificando Configuração Gemini para Análise Preditiva"
echo ""

# 1. Verificar variável de ambiente
echo "1️⃣ Variável de ambiente GEMINI_API_KEY:"
if [ -z "$GEMINI_API_KEY" ]; then
    echo "   ❌ NÃO configurada"
else
    echo "   ✅ Configurada (${#GEMINI_API_KEY} caracteres)"
fi
echo ""

# 2. Verificar tokens salvos no banco
# ✅ Tokens ficam na mesma tabela do AI Diagnostics
DB_PATH="$HOME/.k8s-hpa-manager/ai_diagnostics.db"
echo "2️⃣ Tokens salvos no banco de dados:"
if [ -f "$DB_PATH" ]; then
    echo "   Banco: $DB_PATH"

    # Verificar se tem Gemini configurado (tabela: user_ai_tokens)
    GEMINI_CHECK=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM user_ai_tokens WHERE gemini_api_key IS NOT NULL AND gemini_api_key != ''" 2>/dev/null)

    if [ "$GEMINI_CHECK" = "1" ]; then
        PREFERRED=$(sqlite3 "$DB_PATH" "SELECT preferred_provider FROM user_ai_tokens LIMIT 1" 2>/dev/null)
        GEMINI_MODEL=$(sqlite3 "$DB_PATH" "SELECT gemini_model FROM user_ai_tokens LIMIT 1" 2>/dev/null)

        echo "   ✅ Gemini API Key configurada"
        echo "   📦 Modelo: $GEMINI_MODEL"
        echo "   ⭐ Provider Preferido: $PREFERRED"

        if [ "$PREFERRED" != "gemini" ]; then
            echo ""
            echo "   ⚠️  ATENÇÃO: Gemini está configurado mas NÃO é o provider preferido!"
            echo "   ➡️  Acesse AI Settings e selecione 'Gemini' como provider preferido"
        fi
    else
        echo "   ❌ Gemini API Key NÃO configurada no banco"
        echo ""
        echo "   📝 Para configurar:"
        echo "      1. Obtenha API key: https://aistudio.google.com/app/apikey"
        echo "      2. Acesse: http://localhost:8080 → AI Settings"
        echo "      3. Cole a API key e clique em 'Validar'"
        echo "      4. Selecione modelo e 'Salvar Configurações'"
    fi
else
    echo "   ❌ Banco de dados não encontrado: $DB_PATH"
fi
echo ""

# 3. Verificar conectividade Gemini API
echo "3️⃣ Testando conectividade com Gemini API:"
if command -v curl &> /dev/null; then
    if [ ! -z "$GEMINI_API_KEY" ]; then
        RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" \
            "https://generativelanguage.googleapis.com/v1beta/models?key=$GEMINI_API_KEY")

        if [ "$RESPONSE" = "200" ]; then
            echo "   ✅ API acessível (HTTP $RESPONSE)"
        else
            echo "   ❌ Erro ao acessar API (HTTP $RESPONSE)"
            if [ "$RESPONSE" = "400" ]; then
                echo "      Possível causa: API key inválida"
            fi
        fi
    else
        echo "   ⏭️  Pulando teste (API key não configurada)"
    fi
else
    echo "   ⏭️  curl não disponível"
fi
echo ""

# 4. Resumo e próximos passos
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📊 RESUMO"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ -f "$DB_PATH" ] && [ "$GEMINI_CHECK" = "1" ] && [ "$PREFERRED" = "gemini" ]; then
    echo "✅ Gemini está CONFIGURADO e ATIVO para análise preditiva!"
    echo ""
    echo "🚀 Próximos passos:"
    echo "   1. Iniciar servidor: ./build/new-k8s-hpa web"
    echo "   2. Abrir navegador: http://localhost:8080"
    echo "   3. Aba Deployments → Botão 'Análise Preditiva com IA'"
else
    echo "⚠️  Gemini NÃO está configurado corretamente"
    echo ""
    echo "📝 Passos necessários:"
    echo "   1. Obter API key: https://aistudio.google.com/app/apikey"
    echo "   2. Configurar via web: http://localhost:8080 → AI Settings"
    echo "   3. Ou via env: export GEMINI_API_KEY='AIza...'"
fi
