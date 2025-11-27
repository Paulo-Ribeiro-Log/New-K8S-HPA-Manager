package notifications

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// WindowsNotifier envia notificações nativas do Windows via PowerShell
type WindowsNotifier struct {
	enabled bool
}

// NewWindowsNotifier cria um novo notificador Windows
func NewWindowsNotifier() *WindowsNotifier {
	return &WindowsNotifier{
		enabled: runtime.GOOS == "linux", // WSL2
	}
}

// NotificationOptions representa opções para a notificação
type NotificationOptions struct {
	Title    string // Título da notificação
	Message  string // Mensagem principal
	Severity string // critical, warning, info
	AppLogo  string // Caminho para ícone (opcional)
	Sound    string // Som da notificação: Default, IM, Mail, Reminder, SMS
	Duration string // Short, Long
}

// SendNotification envia uma notificação Windows Toast
func (n *WindowsNotifier) SendNotification(opts NotificationOptions) error {
	if !n.enabled {
		return fmt.Errorf("notificações Windows só funcionam em WSL2")
	}

	// Determinar ícone baseado na severidade
	icon := n.getIconForSeverity(opts.Severity)

	// Construir script PowerShell
	// Usando Windows.UI.Notifications (nativo, não requer BurntToast)
	psScript := fmt.Sprintf(`
$ErrorActionPreference = 'SilentlyContinue'

# Criar notificação Toast nativa do Windows 10/11
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null

$APP_ID = 'K8sHPAManager'

$template = @"
<toast>
    <visual>
        <binding template='ToastGeneric'>
            <text>%s</text>
            <text>%s</text>
            <image placement='appLogoOverride' src='%s'/>
        </binding>
    </visual>
    <audio src='ms-winsoundevent:Notification.%s'/>
</toast>
"@

$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml($template)

$toast = New-Object Windows.UI.Notifications.ToastNotification $xml
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($APP_ID).Show($toast)
`,
		n.escapePowerShell(opts.Title),
		n.escapePowerShell(opts.Message),
		icon,
		n.getSoundForSeverity(opts.Severity),
	)

	// Executar PowerShell via WSL
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("falha ao enviar notificação: %w (output: %s)", err, string(output))
	}

	return nil
}

// SendAlertNotification envia notificação de alerta do Prometheus
func (n *WindowsNotifier) SendAlertNotification(alertName, severity, cluster, namespace, hpaName, message string) error {
	var title string
	switch severity {
	case "critical":
		title = fmt.Sprintf("🔴 CRÍTICO: %s", alertName)
	case "warning":
		title = fmt.Sprintf("🟡 AVISO: %s", alertName)
	default:
		title = fmt.Sprintf("ℹ️ INFO: %s", alertName)
	}

	fullMessage := fmt.Sprintf(
		"Cluster: %s\nNamespace: %s\nHPA: %s\n\n%s",
		cluster, namespace, hpaName, message,
	)

	return n.SendNotification(NotificationOptions{
		Title:    title,
		Message:  fullMessage,
		Severity: severity,
		Duration: "Long",
	})
}

// SendCordonDrainNotification envia notificação de Cordon/Drain
func (n *WindowsNotifier) SendCordonDrainNotification(phase, nodeName, message string) error {
	var title string
	var severity string

	switch phase {
	case "CORDON":
		title = "🔒 Node Cordoned"
		severity = "info"
	case "DRAIN":
		title = "⚠️ Node Draining"
		severity = "warning"
	case "COMPLETE":
		title = "✅ Cordon/Drain Completo"
		severity = "info"
	case "ERROR":
		title = "❌ Erro no Cordon/Drain"
		severity = "critical"
	default:
		title = fmt.Sprintf("🔧 %s", phase)
		severity = "info"
	}

	fullMessage := fmt.Sprintf("Node: %s\n\n%s", nodeName, message)

	return n.SendNotification(NotificationOptions{
		Title:    title,
		Message:  fullMessage,
		Severity: severity,
		Duration: "Short",
	})
}

// SendApplySuccessNotification envia notificação de aplicação bem-sucedida
func (n *WindowsNotifier) SendApplySuccessNotification(resourceType string, count int) error {
	return n.SendNotification(NotificationOptions{
		Title:    "✅ Aplicação Concluída",
		Message:  fmt.Sprintf("%d %s(s) aplicado(s) com sucesso!", count, resourceType),
		Severity: "info",
		Duration: "Short",
	})
}

// getIconForSeverity retorna o ícone adequado para a severidade
func (n *WindowsNotifier) getIconForSeverity(severity string) string {
	// Usar ícones do próprio Windows (ms-appx: ou file://)
	// Por padrão, vazio usa o ícone da aplicação
	switch severity {
	case "critical":
		return "ms-appx:///Images/error.png" // Ícone de erro do Windows
	case "warning":
		return "ms-appx:///Images/warning.png" // Ícone de aviso
	default:
		return "ms-appx:///Images/info.png" // Ícone de informação
	}
}

// getSoundForSeverity retorna o som adequado para a severidade
func (n *WindowsNotifier) getSoundForSeverity(severity string) string {
	switch severity {
	case "critical":
		return "Alarm" // Som de alarme
	case "warning":
		return "Reminder" // Som de lembrete
	default:
		return "Default" // Som padrão
	}
}

// escapePowerShell escapa strings para uso em PowerShell
func (n *WindowsNotifier) escapePowerShell(s string) string {
	// Escapar aspas simples e duplas
	s = strings.ReplaceAll(s, "'", "''")
	s = strings.ReplaceAll(s, "\"", "`\"")
	s = strings.ReplaceAll(s, "\n", "&#10;") // Quebra de linha em XML
	return s
}

// IsEnabled verifica se as notificações estão habilitadas
func (n *WindowsNotifier) IsEnabled() bool {
	return n.enabled
}

// TestNotification envia uma notificação de teste
func (n *WindowsNotifier) TestNotification() error {
	return n.SendNotification(NotificationOptions{
		Title:    "🧪 K8s HPA Manager",
		Message:  "Sistema de notificações configurado com sucesso!\n\nVocê receberá alertas em tempo real sobre seus clusters Kubernetes.",
		Severity: "info",
		Duration: "Long",
	})
}
