package layout

import (
	"strings"
)

// PanelConfig define as configurações de um painel
type PanelConfig struct {
	MinWidth   int  // Largura mínima
	MinHeight  int  // Altura mínima (sem bordas)
	MaxHeight  int  // Altura máxima (sem bordas)
	WithBorder bool // Se tem bordas (+2 linhas)
}

// LayoutManager gerencia o sistema de layout e espaçamento
type LayoutManager struct {
	// Configurações padrão dos painéis
	StandardPanel PanelConfig
	StatusPanel   PanelConfig

	// Espaçamento base entre painéis superiores e inferior
	BaseSpacing int
}

// NewLayoutManager cria um novo gerenciador de layout com as especificações do projeto
func NewLayoutManager() *LayoutManager {
	return &LayoutManager{
		// Painéis padrão (HPAs Disponíveis, HPAs Selecionados, etc.)
		StandardPanel: PanelConfig{
			MinWidth:   70, // 70 colunas
			MinHeight:  18, // 18 linhas de conteúdo
			MaxHeight:  35, // 35 linhas máximo (com scroll)
			WithBorder: true, // +2 linhas para bordas = 20 linhas total
		},

		// Painel Status e Informações
		StatusPanel: PanelConfig{
			MinWidth:   140, // 140 colunas
			MinHeight:  8,   // 8 linhas de conteúdo
			MaxHeight:  8,   // Fixo (não responsivo)
			WithBorder: true, // +2 linhas para bordas = 10 linhas total
		},

		// Espaçamento base entre painéis superiores e Status
		BaseSpacing: 20, // 20 linhas de espaçamento
	}
}

// CalculateSpacing calcula o espaçamento dinâmico entre painéis superiores e Status
func (lm *LayoutManager) CalculateSpacing(panel1Height, panel2Height int) string {
	// Altura máxima entre os dois painéis superiores
	maxPanelHeight := panel1Height
	if panel2Height > maxPanelHeight {
		maxPanelHeight = panel2Height
	}

	// Altura base esperada (mínima com bordas)
	baseHeight := lm.StandardPanel.MinHeight + 2 // 18 + 2 = 20

	// Calcular diferença
	difference := maxPanelHeight - baseHeight

	// Espaçamento dinâmico: base menos o crescimento dos painéis
	spacing := lm.BaseSpacing - difference

	// Garantir espaçamento mínimo de 1 linha
	if spacing < 1 {
		spacing = 1
	}

	// Gerar string de espaçamento
	return strings.Repeat("\n", spacing)
}

// GetStandardPanelDimensions retorna as dimensões padrão para painéis normais
func (lm *LayoutManager) GetStandardPanelDimensions() (width, height int) {
	return lm.StandardPanel.MinWidth, lm.StandardPanel.MinHeight + 2 // +2 para bordas
}

// GetStatusPanelDimensions retorna as dimensões do painel Status
func (lm *LayoutManager) GetStatusPanelDimensions() (width, height int) {
	return lm.StatusPanel.MinWidth, lm.StatusPanel.MinHeight + 2 // +2 para bordas
}

// CalculateResponsivePanelHeight calcula a altura responsiva de um painel baseado no conteúdo
func (lm *LayoutManager) CalculateResponsivePanelHeight(contentLines int) int {
	// Se o conteúdo cabe na altura mínima
	if contentLines <= lm.StandardPanel.MinHeight {
		return lm.StandardPanel.MinHeight + 2 // +2 para bordas
	}

	// Se excede o máximo, usar altura máxima (scroll será necessário)
	if contentLines > lm.StandardPanel.MaxHeight {
		return lm.StandardPanel.MaxHeight + 2 // +2 para bordas
	}

	// Caso contrário, usar altura baseada no conteúdo
	return contentLines + 2 // +2 para bordas
}

// IsScrollNeeded verifica se o scroll é necessário para um painel
func (lm *LayoutManager) IsScrollNeeded(contentLines int) bool {
	return contentLines > lm.StandardPanel.MaxHeight
}

// CalculateVisibleLines calcula quantas linhas são visíveis em um painel com scroll
func (lm *LayoutManager) CalculateVisibleLines(panelHeight int) int {
	return panelHeight - 2 // -2 para bordas
}

// GetMaxContentLines retorna o número máximo de linhas de conteúdo suportadas
func (lm *LayoutManager) GetMaxContentLines() int {
	return lm.StandardPanel.MaxHeight
}

// GetMinContentLines retorna o número mínimo de linhas de conteúdo
func (lm *LayoutManager) GetMinContentLines() int {
	return lm.StandardPanel.MinHeight
}