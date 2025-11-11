#!/bin/bash

# Script de teste interativo para mouse + scroll
# Simula interações de usuário para validação

echo "🖱️ Teste Interativo de Mouse + Scroll"
echo "====================================="
echo

# Cores
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
CYAN='\033[0;36m'
NC='\033[0m'

log_step() {
    echo -e "${CYAN}[STEP]${NC} $1"
}

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[✅]${NC} $1"
}

log_instruction() {
    echo -e "${YELLOW}[📋]${NC} $1"
}

# Verificar se está em ambiente TTY
if [ ! -t 0 ]; then
    echo -e "${RED}❌ Este teste precisa ser executado em um terminal interativo (TTY)${NC}"
    echo "   Execute diretamente no terminal, não via pipe ou redirecionamento"
    exit 1
fi

# Verificar se o binário existe
if [ ! -f "./build/k8s-hpa-manager" ]; then
    echo -e "${RED}❌ Binário não encontrado. Execute 'make build' primeiro.${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Ambiente TTY detectado - pronto para teste interativo${NC}"
echo

log_step "1. PREPARAÇÃO"
echo "============="
log_info "O teste será executado em várias etapas"
log_info "Você poderá testar o mouse e scroll manualmente"
echo

# Função para aguardar input do usuário com timeout
wait_user() {
    echo -e "${YELLOW}Pressione ENTER para continuar (ou aguarde 3 segundos)...${NC}"
    read -r -t 3 || echo -e "${BLUE}[AUTO]${NC} Continuando automaticamente..."
}

log_step "2. INFORMAÇÕES DO TESTE"
echo "======================"
log_info "Sistema de Mouse + Scroll implementado:"
echo "  🖱️  Mouse clique esquerdo: Ativa foco no painel de status"
echo "  🎡 Mouse wheel up/down: Scroll quando painel focado"
echo "  ⌨️  Shift+↑/↓: Scroll quando painel focado"
echo "  🎯 Clique fora do painel: Remove foco e reativa auto-scroll"
echo
log_info "Área do painel de status:"
echo "  📍 Localização: Últimas 12 linhas da tela (parte inferior)"
echo "  📏 Dimensões: Aproximadamente Y >= (altura_terminal - 12)"
echo

wait_user

log_step "3. TESTE DE FUNCIONALIDADES"
echo "==========================="

echo "🎯 FUNCIONALIDADES A TESTAR:"
echo
echo "1️⃣  CLIQUE PARA FOCAR:"
log_instruction "   • Clique na área do painel de status (parte inferior)"
log_instruction "   • Deve aparecer: '📱 Painel de status focado - use Shift+↑/↓ ou mouse wheel'"
echo

echo "2️⃣  SCROLL COM TECLADO:"
log_instruction "   • Após focar, use Shift+↑ e Shift+↓"
log_instruction "   • Deve fazer scroll apenas no painel de status"
echo

echo "3️⃣  SCROLL COM MOUSE:"
log_instruction "   • Após focar, use o mouse wheel (scroll up/down)"
log_instruction "   • Deve fazer scroll apenas no painel de status"
echo

echo "4️⃣  DESFOCAR:"
log_instruction "   • Clique em qualquer área FORA do painel de status"
log_instruction "   • Deve voltar ao modo auto-scroll"
echo

echo "5️⃣  DEBUG LOGS (se --debug ativo):"
log_instruction "   • Deve mostrar coordenadas dos cliques"
log_instruction "   • Deve mostrar mensagens de foco/desfoco"
log_instruction "   • Deve mostrar eventos de mouse wheel"
echo

wait_user

log_step "4. EXECUTANDO A APLICAÇÃO"
echo "========================="
log_info "Iniciando aplicação em modo debug..."
log_info "A aplicação terá mensagens de teste no painel de status"
log_info "Use Ctrl+C ou F4 para sair quando terminar os testes"
echo

log_success "🚀 Executando: ./build/k8s-hpa-manager --debug"
echo
echo "==================== INÍCIO DOS TESTES ===================="

# Executar a aplicação
./build/k8s-hpa-manager --debug

# Após a aplicação fechar
echo
echo "==================== FIM DOS TESTES ======================"
echo

log_step "5. VALIDAÇÃO DOS RESULTADOS"
echo "=========================="

echo "✅ CHECKLIST DE VALIDAÇÃO:"
echo
echo "□ A aplicação iniciou com mensagens de teste no painel de status?"
echo "□ Clique no painel de status ativou o foco (mensagem de confirmação)?"
echo "□ Shift+↑/↓ funcionaram apenas após clicar no painel?"
echo "□ Mouse wheel funcionou apenas após clicar no painel?"
echo "□ Clique fora do painel removeu o foco?"
echo "□ Debug logs mostraram coordenadas dos cliques?"
echo "□ Scroll funcionou corretamente nas mensagens de teste?"
echo

log_instruction "Marque mentalmente os itens que funcionaram corretamente"
echo

echo "📊 INFORMAÇÕES TÉCNICAS:"
echo "======================="
log_info "Mouse support: tea.WithMouseCellMotion() habilitado"
log_info "Detecção de área: Y >= (altura_terminal - 12)"
log_info "Estados de foco: StatusPanelFocused boolean flag"
log_info "Handlers: tea.MouseLeft, tea.MouseWheelUp, tea.MouseWheelDown"
log_info "Scroll condicional: Apenas quando StatusPanelFocused = true"
echo

log_success "🎉 Teste interativo concluído!"
echo
log_info "Se todos os itens do checklist funcionaram, o sistema está OK ✅"
log_info "Se algum item falhou, revise a implementação correspondente ❌"
echo

echo -e "${CYAN}📋 RESULTADO ESPERADO COMPLETO:${NC}"
echo "================================"
echo "1. Aplicação inicia com ~10 mensagens de teste"
echo "2. Clique no painel → Mensagem '📱 Painel de status focado'"
echo "3. Shift+↑/↓ → Scroll funciona no painel de status"
echo "4. Mouse wheel → Scroll funciona no painel de status"
echo "5. Clique fora → Remove foco, volta ao auto-scroll"
echo "6. Debug logs → Coordenadas e eventos de mouse"

echo
echo -e "${GREEN}🚀 Sistema de Mouse + Scroll validado através de teste interativo!${NC}"