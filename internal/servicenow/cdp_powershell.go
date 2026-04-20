package servicenow

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// waitCDPReadyPS aguarda o Chrome CDP responder via PowerShell (lado Windows, 127.0.0.1).
// Um único processo powershell.exe faz o loop internamente.
func waitCDPReadyPS(cdpPort int, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	if secs < 1 {
		secs = 1
	}
	psCmd := fmt.Sprintf(`
$t = %d; $e = 0
while ($e -lt $t) {
    try { $null = Invoke-RestMethod 'http://127.0.0.1:%d/json/version'; exit 0 } catch {}
    Start-Sleep -Milliseconds 500; $e += 0.5
}
exit 1`, secs, cdpPort)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Chrome CDP não respondeu na porta %d após %v", cdpPort, timeout)
	}
	return nil
}

// waitServiceNowLoginPS aguarda um tab do Chrome em viavarejo.service-now.com fora do login/SAML.
func waitServiceNowLoginPS(cdpPort int, timeout time.Duration) error {
	secs := int(timeout.Seconds())
	if secs < 1 {
		secs = 1
	}
	psCmd := fmt.Sprintf(`
$t = %d; $e = 0
while ($e -lt $t) {
    try {
        $tabs = Invoke-RestMethod 'http://127.0.0.1:%d/json'
        foreach ($tab in @($tabs)) {
            if ($tab.url -like '*service-now.com*' -and
                $tab.url -notlike '*login*' -and
                $tab.url -notlike '*saml*' -and
                $tab.url -notlike '*microsoftonline*' -and
                $tab.url -notlike '*live.com*') { exit 0 }
        }
    } catch {}
    Start-Sleep -Seconds 2; $e += 2
}
exit 1`, secs, cdpPort)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	cmd.WaitDelay = timeout + 10*time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("timeout aguardando login no ServiceNow")
	}
	return nil
}

// cdpCookieResponse é a estrutura do retorno CDP de Network.getAllCookies
type cdpCookieResponse struct {
	Result struct {
		Cookies []struct {
			Name   string `json:"name"`
			Value  string `json:"value"`
			Domain string `json:"domain"`
		} `json:"cookies"`
	} `json:"result"`
}

// getCookiesViaPS obtém cookies do Chrome via CDP WebSocket usando PowerShell (lado Windows).
// Usa page target (/json) que é mais compatível com Network.getAllCookies do que o browser target.
// Fallback para browser target (/json/version) se não houver pages abertas.
func getCookiesViaPS(cdpPort int, domain string) (map[string]string, error) {
	psScript := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
try {
    # Page target e mais compativel com Network.getAllCookies em todas as versoes do Chrome
    $wsUrl = $null
    try {
        $pages = Invoke-RestMethod 'http://127.0.0.1:%d/json'
        foreach ($p in @($pages)) {
            if ($p.webSocketDebuggerUrl -and $p.type -ne 'browser') {
                $wsUrl = $p.webSocketDebuggerUrl; break
            }
        }
    } catch {}
    if (-not $wsUrl) {
        $ver = Invoke-RestMethod 'http://127.0.0.1:%d/json/version'
        $wsUrl = $ver.webSocketDebuggerUrl
    }
    if (-not $wsUrl) { throw "Nenhum target CDP disponivel" }
    $ws = New-Object System.Net.WebSockets.ClientWebSocket
    $ct = [System.Threading.CancellationToken]::None
    if (-not $ws.ConnectAsync([Uri]$wsUrl, $ct).Wait(5000)) { throw "WebSocket connect timeout" }
    $cmd     = [Text.Encoding]::UTF8.GetBytes('{"id":1,"method":"Network.getAllCookies"}')
    $sendSeg = [System.ArraySegment[byte]]::new($cmd)
    $ws.SendAsync($sendSeg, [System.Net.WebSockets.WebSocketMessageType]::Text, $true, $ct).Wait(5000) | Out-Null
    $buf     = New-Object byte[] 524288
    $recvSeg = [System.ArraySegment[byte]]::new($buf)
    $r       = $ws.ReceiveAsync($recvSeg, $ct).GetAwaiter().GetResult()
    try { $ws.CloseAsync('NormalClosure', '', $ct).Wait(2000) | Out-Null } catch {}
    [Console]::Write([Text.Encoding]::UTF8.GetString($buf, 0, $r.Count))
} catch {
    Write-Error $_.Exception.Message
    exit 1
}`, cdpPort, cdpPort)

	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psScript).Output()
	if err != nil {
		return nil, fmt.Errorf("PowerShell CDP falhou: %v", err)
	}

	var resp cdpCookieResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parse resposta CDP: %v (raw: %.200s)", err, string(out))
	}

	cookies := make(map[string]string)
	for _, c := range resp.Result.Cookies {
		if strings.Contains(c.Domain, domain) {
			cookies[c.Name] = c.Value
		}
	}
	return cookies, nil
}

// checkCDPRunningPS verifica rapidamente se Chrome está rodando com debug port (via PowerShell).
func checkCDPRunningPS(cdpPort int) bool {
	psCmd := fmt.Sprintf(
		`try { $null = Invoke-RestMethod 'http://127.0.0.1:%d/json/version'; exit 0 } catch { exit 1 }`,
		cdpPort,
	)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psCmd)
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run() == nil
}
