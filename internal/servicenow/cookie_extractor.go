package servicenow

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// chromeCookieExtractor extrai cookies do Chrome Windows a partir do WSL2.
// Não usa CDP. Lê o banco SQLite de cookies diretamente e descriptografa
// com a chave AES do Chrome (obtida via DPAPI + PowerShell).
type chromeCookieExtractor struct{}

// ExtractChromeCookiesWSL extrai cookies do Chrome para o domínio informado.
// Requer: WSL2 + sqlite3 instalado + PowerShell disponível no Windows.
// Retorna map de nome→valor (cookies descriptografados).
func ExtractChromeCookiesWSL(domain string) (map[string]string, error) {
	c := &chromeCookieExtractor{}

	username, err := c.windowsUsername()
	if err != nil {
		return nil, fmt.Errorf("não foi possível obter username Windows: %v", err)
	}

	aesKey, err := c.chromeAESKey(username)
	if err != nil {
		return nil, fmt.Errorf("falha ao obter chave AES do Chrome: %v", err)
	}

	rows, err := c.readCookieRows(username, domain)
	if err != nil {
		return nil, fmt.Errorf("falha ao ler cookies: %v", err)
	}

	result := make(map[string]string)
	for name, hexVal := range rows {
		encBytes, decErr := hex.DecodeString(hexVal)
		if decErr != nil {
			continue
		}
		value, decErr := c.decryptValue(aesKey, encBytes)
		if decErr == nil && value != "" {
			result[name] = value
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("nenhum cookie encontrado para %s — faça login no ServiceNow no Chrome e tente novamente", domain)
	}
	return result, nil
}

// windowsUsername retorna o username do Windows via cmd.exe
func (c *chromeCookieExtractor) windowsUsername() (string, error) {
	out, err := exec.Command("cmd.exe", "/c", "echo", "%USERNAME%").Output()
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(string(out))
	if name == "" || name == "%USERNAME%" {
		return "", fmt.Errorf("variável %%USERNAME%% não expandida")
	}
	return name, nil
}

// chromeLocalStatePath retorna o path WSL para o Local State do Chrome
func (c *chromeCookieExtractor) chromeLocalStatePath(username string) string {
	return fmt.Sprintf("/mnt/c/Users/%s/AppData/Local/Google/Chrome/User Data/Local State", username)
}

// chromeCookieDBPath retorna o path WSL para o banco de cookies do Chrome.
// Prioriza o perfil HPA-Manager (usado pelo LaunchWindowsBrowserForCDP), depois Default.
func (c *chromeCookieExtractor) chromeCookieDBPath(username string) string {
	base := fmt.Sprintf("/mnt/c/Users/%s/AppData/Local/Google/Chrome/User Data", username)
	profiles := []string{"HPA-Manager", "Default"}
	suffixes := []string{"Network/Cookies", "Cookies"}
	for _, profile := range profiles {
		for _, suffix := range suffixes {
			p := fmt.Sprintf("%s/%s/%s", base, profile, suffix)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// edgeCookieDBPath retorna o path WSL para o banco de cookies do Edge.
// Prioriza o perfil HPA-Manager, depois Default.
func (c *chromeCookieExtractor) edgeCookieDBPath(username string) string {
	base := fmt.Sprintf("/mnt/c/Users/%s/AppData/Local/Microsoft/Edge/User Data", username)
	profiles := []string{"HPA-Manager", "Default"}
	suffixes := []string{"Network/Cookies", "Cookies"}
	for _, profile := range profiles {
		for _, suffix := range suffixes {
			p := fmt.Sprintf("%s/%s/%s", base, profile, suffix)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// chromeAESKey obtém a chave AES-256 usada para criptografar os cookies do Chrome.
// Lê o Local State, extrai a chave encriptada com DPAPI e decripta via PowerShell.
func (c *chromeCookieExtractor) chromeAESKey(username string) ([]byte, error) {
	localStatePath := c.chromeLocalStatePath(username)
	data, err := os.ReadFile(localStatePath)
	if err != nil {
		// Tentar Edge
		edgeStatePath := fmt.Sprintf("/mnt/c/Users/%s/AppData/Local/Microsoft/Edge/User Data/Local State", username)
		data, err = os.ReadFile(edgeStatePath)
		if err != nil {
			return nil, fmt.Errorf("Chrome/Edge Local State não encontrado em /mnt/c/Users/%s/AppData/Local/", username)
		}
	}

	var localState struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(data, &localState); err != nil {
		return nil, fmt.Errorf("erro ao parsear Local State: %v", err)
	}

	encKey, err := base64.StdEncoding.DecodeString(localState.OSCrypt.EncryptedKey)
	if err != nil {
		return nil, fmt.Errorf("erro ao decodificar chave: %v", err)
	}

	// Primeiros 5 bytes são o prefixo "DPAPI"
	if len(encKey) < 5 || string(encKey[:5]) != "DPAPI" {
		return nil, fmt.Errorf("prefixo DPAPI não encontrado na chave do Chrome")
	}
	encKeyNoPrefix := encKey[5:]
	encKeyB64 := base64.StdEncoding.EncodeToString(encKeyNoPrefix)

	return c.dpapiDecrypt(encKeyB64)
}

// dpapiDecrypt decripta bytes via DPAPI usando PowerShell 5.1 (nativo no Windows).
// Recebe base64 dos bytes encriptados, retorna os bytes decriptados.
func (c *chromeCookieExtractor) dpapiDecrypt(encB64 string) ([]byte, error) {
	// PowerShell 5.1: Add-Type -AssemblyName System.Security já tem ProtectedData
	psScript := fmt.Sprintf(
		`Add-Type -AssemblyName System.Security; `+
			`$b=[System.Convert]::FromBase64String('%s'); `+
			`$k=[System.Security.Cryptography.ProtectedData]::Unprotect($b,$null,[System.Security.Cryptography.DataProtectionScope]::CurrentUser); `+
			`[System.Console]::Write([System.Convert]::ToBase64String($k))`,
		encB64,
	)

	// Tentar pwsh.exe (PS7) primeiro, depois powershell.exe (PS5.1)
	for _, psExe := range []string{"pwsh.exe", "powershell.exe"} {
		out, err := exec.Command(psExe, "-NoProfile", "-NonInteractive", "-Command", psScript).Output()
		if err != nil {
			continue
		}
		key, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
		if decErr != nil {
			continue
		}
		if len(key) != 32 {
			continue
		}
		return key, nil
	}

	return nil, fmt.Errorf("DPAPI: powershell.exe e pwsh.exe falharam ou não encontrados")
}

// readCookieRows usa sqlite3 (WSL) para ler os cookies do Chrome/Edge para o domínio.
// Retorna map de nome→hex(encrypted_value).
func (c *chromeCookieExtractor) readCookieRows(username, domain string) (map[string]string, error) {
	dbPath := c.chromeCookieDBPath(username)
	if dbPath == "" {
		dbPath = c.edgeCookieDBPath(username)
	}
	if dbPath == "" {
		return nil, fmt.Errorf("banco de cookies do Chrome/Edge não encontrado em /mnt/c/Users/%s/AppData/Local/", username)
	}

	// Tentar com immutable=1 (leitura sem lock) e, se falhar, copiar o arquivo primeiro
	rows, err := c.querySQLite(dbPath, domain)
	if err != nil {
		// Chrome provavelmente está com o DB aberto — copiar para /tmp
		tmpPath := filepath.Join("/tmp", fmt.Sprintf("sn_cookies_%d.db", os.Getpid()))
		if copyErr := copyFile(dbPath, tmpPath); copyErr != nil {
			return nil, fmt.Errorf("não foi possível copiar cookies DB: %v (original: %v)", copyErr, err)
		}
		defer os.Remove(tmpPath)
		rows, err = c.querySQLite(tmpPath, domain)
		if err != nil {
			return nil, fmt.Errorf("sqlite3 falhou: %v — instale com: sudo apt install sqlite3", err)
		}
	}
	return rows, nil
}

// querySQLite executa sqlite3 no arquivo informado e retorna name→hex(encrypted_value)
func (c *chromeCookieExtractor) querySQLite(dbPath, domain string) (map[string]string, error) {
	// Usar immutable=1 para evitar travamento (leitura apenas)
	sqURL := fmt.Sprintf("file:%s?immutable=1", dbPath)
	query := fmt.Sprintf(
		"SELECT name, hex(encrypted_value) FROM cookies WHERE host_key LIKE '%%%s%%' AND hex(encrypted_value) != '';",
		domain,
	)

	out, err := exec.Command("sqlite3", sqURL, query).Output()
	if err != nil {
		// Tentar sem URI (arquivo copiado não precisa de immutable)
		out, err = exec.Command("sqlite3", dbPath, query).Output()
		if err != nil {
			return nil, err
		}
	}

	rows := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			rows[parts[0]] = parts[1]
		}
	}
	return rows, nil
}

// decryptValue descriptografa um valor de cookie Chrome v10/v11 (AES-256-GCM).
// Formato: 3 bytes versão ("v10"/"v11") + 12 bytes nonce + ciphertext + 16 bytes tag.
func (c *chromeCookieExtractor) decryptValue(key, encValue []byte) (string, error) {
	if len(encValue) < 31 {
		// Cookie sem criptografia (Chrome < v80 ou plaintext)
		val := strings.TrimRight(string(encValue), "\x00")
		return val, nil
	}

	prefix := string(encValue[:3])
	if prefix != "v10" && prefix != "v11" {
		return strings.TrimRight(string(encValue), "\x00"), nil
	}

	nonce := encValue[3:15]     // 12 bytes
	payload := encValue[15:]    // ciphertext + 16-byte tag

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// gcm.Open recebe: nonce, ciphertext||tag, aad
	plain, err := gcm.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("AES-GCM falhou: %v", err)
	}
	return string(plain), nil
}

// copyFile copia um arquivo de src para dst
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}
