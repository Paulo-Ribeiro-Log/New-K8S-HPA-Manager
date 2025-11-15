package components

import (
	"fmt"
	"strconv"
	"strings"

	"k8s-hpa-manager/internal/models"

	"github.com/charmbracelet/lipgloss"
)

// SequenceConfigModal representa o modal de configuração de sequenciamento
type SequenceConfigModal struct {
	nodePools []*models.NodePool // Node pools marcados (*1 e *2)

	// Opções de operação
	CordonEnabled bool
	DrainEnabled  bool

	// Opções de drain (essenciais)
	IgnoreDaemonsets   bool
	DeleteEmptyDirData bool
	Force              bool
	GracePeriod        string // String para edição, será parseado
	Timeout            string

	// Opções avançadas (collapsed por padrão)
	ShowAdvanced           bool
	DisableEviction        bool
	SkipWaitTimeout        string
	PodSelector            string
	DryRun                 bool
	ChunkSize              string

	// Estado de navegação
	FocusedField int // Índice do campo focado
	MaxField     int // Total de campos (ajusta com ShowAdvanced)
}

// NewSequenceConfigModal cria novo modal com defaults
func NewSequenceConfigModal(nodePools []*models.NodePool) *SequenceConfigModal {
	defaults := models.DefaultDrainOptions()

	return &SequenceConfigModal{
		nodePools: nodePools,

		// Defaults recomendados
		CordonEnabled:      true,
		DrainEnabled:       true,
		IgnoreDaemonsets:   defaults.IgnoreDaemonsets,
		DeleteEmptyDirData: defaults.DeleteEmptyDirData,
		Force:              defaults.Force,
		GracePeriod:        fmt.Sprintf("%d", defaults.GracePeriod),
		Timeout:            defaults.Timeout,

		// Avançadas
		ShowAdvanced:    false,
		DisableEviction: defaults.DisableEviction,
		SkipWaitTimeout: fmt.Sprintf("%d", defaults.SkipWaitForDeleteTimeout),
		PodSelector:     defaults.PodSelector,
		DryRun:          defaults.DryRun,
		ChunkSize:       fmt.Sprintf("%d", defaults.ChunkSize),

		FocusedField: 0,
		MaxField:     9, // Atualizado dinamicamente
	}
}

// Render renderiza o modal
func (m *SequenceConfigModal) Render(width, height int) string {
	// Estilos
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39")). // Azul
		MarginBottom(1)

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("208")). // Laranja
		MarginTop(1)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")). // Cinza
		Italic(true)

	separatorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	var b strings.Builder

	// Título
	b.WriteString(titleStyle.Render("⚙️  Node Pool Sequencing - Configuração"))
	b.WriteString("\n\n")

	// Node Pools Selecionados
	b.WriteString(sectionStyle.Render("📋 Node Pools Selecionados:"))
	b.WriteString("\n")
	for _, pool := range m.nodePools {
		var changeDesc string
		if pool.PreDrainChanges != nil {
			changeDesc = fmt.Sprintf(" (PRE-DRAIN: autoscaling=%v, count=%d, min=%d, max=%d)",
				pool.PreDrainChanges.Autoscaling,
				pool.PreDrainChanges.NodeCount,
				pool.PreDrainChanges.MinNodes,
				pool.PreDrainChanges.MaxNodes)
		} else if pool.PostDrainChanges != nil {
			changeDesc = fmt.Sprintf(" (POST-DRAIN: autoscaling=%v, count=%d, min=%d, max=%d)",
				pool.PostDrainChanges.Autoscaling,
				pool.PostDrainChanges.NodeCount,
				pool.PostDrainChanges.MinNodes,
				pool.PostDrainChanges.MaxNodes)
		}
		b.WriteString(fmt.Sprintf("  *%d: %s%s\n", pool.SequenceOrder, pool.Name, changeDesc))
	}
	b.WriteString("\n")

	// Separator
	b.WriteString(separatorStyle.Render(strings.Repeat("─", 70)))
	b.WriteString("\n")

	// Operações de Transição
	b.WriteString(sectionStyle.Render("⚙️  Operações de Transição:"))
	b.WriteString("\n\n")

	// Cordon Enabled
	cordonCheck := " "
	if m.CordonEnabled {
		cordonCheck = "✓"
	}
	cordonStyle := lipgloss.NewStyle()
	if m.FocusedField == 0 {
		cordonStyle = cordonStyle.Background(lipgloss.Color("236"))
	}
	b.WriteString(cordonStyle.Render(fmt.Sprintf("  [%s] Habilitar Cordon", cordonCheck)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("      Marca nodes como unschedulable antes do drain"))
	b.WriteString("\n\n")

	// Drain Enabled
	drainCheck := " "
	if m.DrainEnabled {
		drainCheck = "✓"
	}
	drainStyle := lipgloss.NewStyle()
	if m.FocusedField == 1 {
		drainStyle = drainStyle.Background(lipgloss.Color("236"))
	}
	b.WriteString(drainStyle.Render(fmt.Sprintf("  [%s] Habilitar Drain", drainCheck)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("      Remove pods gracefully e os migra para destino"))
	b.WriteString("\n")

	// Separator
	b.WriteString("\n")
	b.WriteString(separatorStyle.Render(strings.Repeat("─", 70)))
	b.WriteString("\n")

	// Opções de Drain (Essenciais)
	b.WriteString(sectionStyle.Render("🔧 Opções de Drain:"))
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("  ESSENCIAIS:"))
	b.WriteString("\n\n")

	// IgnoreDaemonsets
	daemonCheck := " "
	if m.IgnoreDaemonsets {
		daemonCheck = "✓"
	}
	daemonStyle := lipgloss.NewStyle()
	if m.FocusedField == 2 {
		daemonStyle = daemonStyle.Background(lipgloss.Color("236"))
	}
	b.WriteString(daemonStyle.Render(fmt.Sprintf("  [%s] --ignore-daemonsets", daemonCheck)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("      Ignora DaemonSets (recomendado)"))
	b.WriteString("\n\n")

	// DeleteEmptyDirData
	emptyCheck := " "
	if m.DeleteEmptyDirData {
		emptyCheck = "✓"
	}
	emptyStyle := lipgloss.NewStyle()
	if m.FocusedField == 3 {
		emptyStyle = emptyStyle.Background(lipgloss.Color("236"))
	}
	b.WriteString(emptyStyle.Render(fmt.Sprintf("  [%s] --delete-emptydir-data", emptyCheck)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("      Permite deletar pods com volumes emptyDir"))
	b.WriteString("\n\n")

	// Force
	forceCheck := " "
	if m.Force {
		forceCheck = "✓"
	}
	forceStyle := lipgloss.NewStyle()
	if m.FocusedField == 4 {
		forceStyle = forceStyle.Background(lipgloss.Color("236"))
	}
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	b.WriteString(forceStyle.Render(fmt.Sprintf("  [%s] --force", forceCheck)))
	b.WriteString("\n")
	b.WriteString(warningStyle.Render("      ⚠️  Força remoção de pods standalone (use com cuidado!)"))
	b.WriteString("\n\n")

	// Grace Period
	graceStyle := lipgloss.NewStyle()
	if m.FocusedField == 5 {
		graceStyle = graceStyle.Background(lipgloss.Color("236"))
	}
	b.WriteString(graceStyle.Render(fmt.Sprintf("  Grace Period: [%s] segundos", m.GracePeriod)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("      Tempo de espera antes de forçar terminação"))
	b.WriteString("\n\n")

	// Timeout
	timeoutStyle := lipgloss.NewStyle()
	if m.FocusedField == 6 {
		timeoutStyle = timeoutStyle.Background(lipgloss.Color("236"))
	}
	b.WriteString(timeoutStyle.Render(fmt.Sprintf("  Timeout: [%s] (ex: 5m, 300s, 10m)", m.Timeout)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("      Timeout total da operação"))
	b.WriteString("\n\n")

	// Opções Avançadas (Accordion)
	advancedStyle := lipgloss.NewStyle()
	if m.FocusedField == 7 {
		advancedStyle = advancedStyle.Background(lipgloss.Color("236"))
	}
	expandIcon := "▶"
	if m.ShowAdvanced {
		expandIcon = "▼"
	}
	b.WriteString(advancedStyle.Render(fmt.Sprintf("  %s Mostrar opções avançadas (pressione 'A')...", expandIcon)))
	b.WriteString("\n")

	if m.ShowAdvanced {
		b.WriteString("\n")
		// TODO: Renderizar campos avançados
		b.WriteString("  (Opções avançadas - TODO)")
		b.WriteString("\n")
	}

	// Separator
	b.WriteString("\n")
	b.WriteString(separatorStyle.Render(strings.Repeat("─", 70)))
	b.WriteString("\n")

	// Fluxo de Execução (Preview)
	b.WriteString(sectionStyle.Render("📊 Fluxo de Execução:"))
	b.WriteString("\n\n")

	origin := m.nodePools[0]
	dest := m.nodePools[1]
	if origin.SequenceOrder == 2 {
		origin, dest = dest, origin
	}

	b.WriteString("  1️⃣  FASE PRE-DRAIN\n")
	b.WriteString(fmt.Sprintf("      Ajustar %s (destino) para receber pods\n", dest.Name))
	if dest.PreDrainChanges != nil {
		b.WriteString(fmt.Sprintf("      → Autoscaling=%v, NodeCount=%d, Min=%d, Max=%d\n",
			dest.PreDrainChanges.Autoscaling,
			dest.PreDrainChanges.NodeCount,
			dest.PreDrainChanges.MinNodes,
			dest.PreDrainChanges.MaxNodes))
	}
	b.WriteString("\n")

	b.WriteString("  2️⃣  AGUARDAR NODES READY (30s)\n")
	b.WriteString("      Aguardar nodes do destino ficarem Ready\n\n")

	b.WriteString("  3️⃣  CORDON\n")
	b.WriteString(fmt.Sprintf("      Marcar nodes do %s (origem) como unschedulable\n\n", origin.Name))

	b.WriteString("  4️⃣  DRAIN\n")
	b.WriteString(fmt.Sprintf("      Migrar pods de %s → %s\n", origin.Name, dest.Name))
	flags := []string{}
	if m.IgnoreDaemonsets {
		flags = append(flags, "--ignore-daemonsets")
	}
	if m.DeleteEmptyDirData {
		flags = append(flags, "--delete-emptydir-data")
	}
	if m.Force {
		flags = append(flags, "--force")
	}
	if len(flags) > 0 {
		b.WriteString(fmt.Sprintf("      Com flags: %s\n", strings.Join(flags, " ")))
	}
	b.WriteString("\n")

	b.WriteString("  5️⃣  FASE POST-DRAIN\n")
	b.WriteString(fmt.Sprintf("      Ajustar %s (origem) para desligar\n", origin.Name))
	if origin.PostDrainChanges != nil {
		b.WriteString(fmt.Sprintf("      → Autoscaling=%v, NodeCount=%d\n",
			origin.PostDrainChanges.Autoscaling,
			origin.PostDrainChanges.NodeCount))
	}
	b.WriteString("\n")

	// Separator
	b.WriteString(separatorStyle.Render(strings.Repeat("─", 70)))
	b.WriteString("\n\n")

	// Footer
	b.WriteString(helpStyle.Render("  [Esc] Cancelar  [Tab] Próximo Campo  [Space] Toggle  [Enter] Executar  [A] Avançadas"))

	return b.String()
}

// ToDrainOptions converte configuração do modal para DrainOptions
func (m *SequenceConfigModal) ToDrainOptions() (*models.DrainOptions, error) {
	// Parsear grace period
	gracePeriod, err := strconv.Atoi(m.GracePeriod)
	if err != nil {
		return nil, fmt.Errorf("grace period inválido: %w", err)
	}

	// Parsear skip wait timeout
	skipWait, err := strconv.Atoi(m.SkipWaitTimeout)
	if err != nil {
		skipWait = 20 // Default
	}

	// Parsear chunk size
	chunkSize, err := strconv.Atoi(m.ChunkSize)
	if err != nil {
		chunkSize = 1 // Default
	}

	opts := &models.DrainOptions{
		IgnoreDaemonsets:         m.IgnoreDaemonsets,
		DeleteEmptyDirData:       m.DeleteEmptyDirData,
		Force:                    m.Force,
		GracePeriod:              gracePeriod,
		Timeout:                  m.Timeout,
		DisableEviction:          m.DisableEviction,
		SkipWaitForDeleteTimeout: skipWait,
		PodSelector:              m.PodSelector,
		DryRun:                   m.DryRun,
		ChunkSize:                chunkSize,
	}

	// Validar
	// Note: Usar função de validação do kubernetes client
	// if err := kubernetes.ValidateDrainOptions(opts); err != nil {
	// 	return nil, err
	// }

	return opts, nil
}

// HandleKey trata input do teclado
func (m *SequenceConfigModal) HandleKey(key string) {
	switch key {
	case "tab":
		m.FocusedField = (m.FocusedField + 1) % (m.MaxField + 1)
	case "shift+tab":
		m.FocusedField--
		if m.FocusedField < 0 {
			m.FocusedField = m.MaxField
		}
	case " ": // Space - toggle checkbox
		m.toggleCurrentField()
	case "a", "A": // Toggle opções avançadas
		m.ShowAdvanced = !m.ShowAdvanced
		if m.ShowAdvanced {
			m.MaxField = 14 // Total com avançadas
		} else {
			m.MaxField = 9 // Apenas essenciais
		}
	}
}

// toggleCurrentField alterna o campo focado (checkboxes)
func (m *SequenceConfigModal) toggleCurrentField() {
	switch m.FocusedField {
	case 0:
		m.CordonEnabled = !m.CordonEnabled
	case 1:
		m.DrainEnabled = !m.DrainEnabled
	case 2:
		m.IgnoreDaemonsets = !m.IgnoreDaemonsets
	case 3:
		m.DeleteEmptyDirData = !m.DeleteEmptyDirData
	case 4:
		m.Force = !m.Force
	// 5 e 6 são campos de texto (grace period e timeout)
	// 7 é o toggle de avançadas
	// TODO: Implementar toggles para campos avançados
	}
}

// SetFieldValue define valor de um campo de texto
func (m *SequenceConfigModal) SetFieldValue(value string) {
	switch m.FocusedField {
	case 5: // Grace Period
		m.GracePeriod = value
	case 6: // Timeout
		m.Timeout = value
	// TODO: Campos avançados
	}
}

// GetFieldValue retorna valor atual do campo focado
func (m *SequenceConfigModal) GetFieldValue() string {
	switch m.FocusedField {
	case 5:
		return m.GracePeriod
	case 6:
		return m.Timeout
	default:
		return ""
	}
}
