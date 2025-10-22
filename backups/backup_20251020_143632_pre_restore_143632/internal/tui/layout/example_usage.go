package layout

import (
	"fmt"
	"strings"
)

// Este arquivo demonstra como usar o módulo de layout

// ExampleHPAScreen mostra como renderizar uma tela de HPAs usando o novo sistema
func ExampleHPAScreen() string {
	// 1. Criar o gerenciador de layout
	layoutMgr := NewLayoutManager()

	// 2. Preparar dados dos painéis
	hpaListContent := []string{
		"📁 HPAs Disponíveis - Cluster: akspriv-dev",
		"📊 Total: 15 | Modificados: 3",
		"",
		"🎯 api-gateway-hpa",
		"   Min: 2 | Max: 10 | Current: 4",
		"🎯 user-service-hpa",
		"   Min: 1 | Max: 5 | Current: 2",
		"🎯 order-service-hpa",
		"   Min: 3 | Max: 15 | Current: 8",
		// ... mais HPAs
	}

	hpaSelectedContent := []string{
		"───────── api-services ─────────",
		"🎯 api-gateway-hpa",
		"   Min: 2✨ | Max: 10 | Current: 4",
		"   Rollout: ✅",
		"",
		"───────── user-services ─────────",
		"🎯 user-service-hpa",
		"   Min: 1 | Max: 5✨ | Current: 2",
		"   Rollout: ❌",
		// ... mais HPAs selecionados
	}

	statusContent := `🔄 Status: Configurações modificadas
📊 Progress:
  ━━━━━━━━━━ 100% Deployment rollout completed
  ╌╌╌╌╌╌╌╌╌╌  45% StatefulSet rollout in progress

✅ Successo: 3 HPAs aplicados com sucesso
⚠️  Warning: Token Azure expira em 15 minutos`

	// 3. Criar painéis responsivos
	leftPanel := NewResponsivePanel("HPAs Disponíveis", hpaListContent, PrimaryColor, layoutMgr)
	rightPanel := NewResponsivePanel("HPAs Selecionados", hpaSelectedContent, SuccessColor, layoutMgr)
	statusPanel := NewFixedPanel("📊 Status e Informações", statusContent, SecondaryColor, layoutMgr)

	// 4. Usar o LayoutBuilder para construir a tela
	sessionInfo := TitleStyle.Render("🎯 HPAs - Namespace: api-services") + "\n\n"
	helpText := HelpStyle.Render("↑↓ Navegar • SPACE Selecionar • TAB Alternar painel • Ctrl+D Aplicar • ? Ajuda • ESC Voltar")

	layout := NewLayoutBuilder(layoutMgr).
		SetSessionInfo(sessionInfo).
		SetHelpText(helpText).
		AddPanel(leftPanel.Render(), leftPanel.GetActualHeight()).
		AddPanel(rightPanel.Render(), rightPanel.GetActualHeight())

	return layout.BuildTwoColumn(statusPanel.Render())
}

// ExampleCronJobScreen mostra como usar para a tela de CronJobs
func ExampleCronJobScreen() string {
	layoutMgr := NewLayoutManager()

	cronJobContent := []string{
		"📅 Cluster: akspriv-prod",
		"📊 Total: 8 | Selecionados: 2",
		"",
		"Controles: [Space] Selecionar [Enter] Editar [ESC] Voltar",
		"",
		"● 🟢 sre-tools/rollout-scheduler-0",
		"     Schedule: 0 2 * * * - executa todo dia às 2:00 AM",
		"     Última: 27/09 02:00 (Success)",
		"     Função: rollout-scheduler (harbor01.viavarejo.com.br/go-spinnaker/bitnami/kubectl:latest)",
		"     Faz rollout restart do deployment vv-estoque-hub-api no namespace regionalizacao-prd",
		"",
		"◯ 🔴 backup-tools/database-backup",
		"     Schedule: 0 0 * * 1 - executa toda segunda-feira à meia-noite",
		"     Última: 23/09 00:00 (Success)",
		"     Função: pg_dump backup automático",
		"",
	}

	statusContent := `📅 CronJobs Status:
✅ 6 ativos | 🔴 2 suspensos | 🟡 0 falharam

🔄 Última sincronização: 27/09 09:15
⏰ Próxima execução: rollout-scheduler em 16h45m`

	// Single column layout para CronJobs
	mainPanel := NewResponsivePanel("CronJobs Disponíveis", cronJobContent, PrimaryColor, layoutMgr)
	statusPanel := NewFixedPanel("📊 Status e Informações", statusContent, SecondaryColor, layoutMgr)

	sessionInfo := TitleStyle.Render("📅 CronJobs - Cluster: akspriv-prod") + "\n\n"
	helpText := HelpStyle.Render("↑↓ Navegar • SPACE Selecionar • ENTER Editar • F9 CronJobs • ? Ajuda • ESC Voltar")

	layout := NewLayoutBuilder(layoutMgr).
		SetSessionInfo(sessionInfo).
		SetHelpText(helpText).
		AddPanel(mainPanel.Render(), mainPanel.GetActualHeight())

	return layout.BuildSingleColumn(statusPanel.Render())
}

// ExampleScrollablePanel demonstra como implementar scroll
func ExampleScrollablePanel() {
	layoutMgr := NewLayoutManager()

	// Criar um painel com muito conteúdo (mais que 35 linhas)
	var largeContent []string
	for i := 1; i <= 50; i++ {
		largeContent = append(largeContent, fmt.Sprintf("Item %d com conteúdo extenso que pode ocupar múltiplas linhas", i))
	}

	panel := NewResponsivePanel("Painel com Scroll", largeContent, PrimaryColor, layoutMgr)

	// Simular scroll
	fmt.Println("=== Estado inicial ===")
	fmt.Println(panel.Render())

	// Scroll para baixo
	for i := 0; i < 10; i++ {
		panel.ScrollDown()
	}

	fmt.Println("\n=== Após scroll down ===")
	fmt.Println(panel.Render())

	// Ajustar scroll para manter item 25 visível
	panel.AdjustScrollToKeepItemVisible(25)

	fmt.Println("\n=== Após ajustar para item 25 ===")
	fmt.Println(panel.Render())
}

// ExampleSpacingCalculation demonstra o cálculo de espaçamento
func ExampleSpacingCalculation() {
	layoutMgr := NewLayoutManager()

	// Exemplo 1: Painéis com altura mínima (20 linhas cada)
	spacing1 := layoutMgr.CalculateSpacing(20, 20)
	fmt.Printf("Painéis 20x20: espaçamento = %d linhas\n", strings.Count(spacing1, "\n"))

	// Exemplo 2: Painel esquerdo cresceu para 25 linhas
	spacing2 := layoutMgr.CalculateSpacing(25, 20)
	fmt.Printf("Painéis 25x20: espaçamento = %d linhas\n", strings.Count(spacing2, "\n"))

	// Exemplo 3: Ambos painéis cresceram para 30 linhas
	spacing3 := layoutMgr.CalculateSpacing(30, 30)
	fmt.Printf("Painéis 30x30: espaçamento = %d linhas\n", strings.Count(spacing3, "\n"))

	// Exemplo 4: Painéis muito grandes (35+ linhas)
	spacing4 := layoutMgr.CalculateSpacing(40, 35)
	fmt.Printf("Painéis 40x35: espaçamento = %d linhas\n", strings.Count(spacing4, "\n"))
}

// ExampleIntegrationWithExistingCode mostra como integrar com código existente
func ExampleIntegrationWithExistingCode(selectedHPAs []string, clusters []string) string {
	layoutMgr := NewLayoutManager()

	// Converter dados existentes para formato do novo sistema
	var clusterContent []string
	clusterContent = append(clusterContent, "🌐 Clusters Disponíveis")
	clusterContent = append(clusterContent, "")
	for i, cluster := range clusters {
		status := "⏳"
		if i%3 == 0 {
			status = "✅"
		} else if i%3 == 1 {
			status = "❌"
		}
		clusterContent = append(clusterContent, fmt.Sprintf("%s %s", status, cluster))
	}

	var hpaContent []string
	hpaContent = append(hpaContent, "🎯 HPAs Selecionados")
	hpaContent = append(hpaContent, "")
	for _, hpa := range selectedHPAs {
		hpaContent = append(hpaContent, fmt.Sprintf("● %s", hpa))
	}

	// Usar sistema novo
	leftPanel := NewResponsivePanel("Clusters", clusterContent, PrimaryColor, layoutMgr)
	rightPanel := NewResponsivePanel("HPAs", hpaContent, SuccessColor, layoutMgr)
	statusPanel := NewFixedPanel("Status", "Sistema funcionando normalmente", SecondaryColor, layoutMgr)

	layout := NewLayoutBuilder(layoutMgr).
		AddPanel(leftPanel.Render(), leftPanel.GetActualHeight()).
		AddPanel(rightPanel.Render(), rightPanel.GetActualHeight())

	return layout.BuildTwoColumn(statusPanel.Render())
}