package servicenow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// manualSession guarda cookies colados manualmente pelo usuário.
// Os cookies ficam em memória e opcionalmente persistidos em disco (sem criptografia,
// pois o usuário escolhe explicitamente salvar).
type manualSession struct {
	mu      sync.RWMutex
	cookies map[string]string
	savedAt time.Time
}

var globalManualSession = &manualSession{}

// ParseCookieHeader parseia um header Cookie (ex: "JSESSIONID=abc; glide_user_auth=xyz")
// ou um par simples "name=value" e retorna map[string]string.
func ParseCookieHeader(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("cookie string vazia")
	}
	// Remover prefixo "Cookie:" se o usuário colou o header completo
	if idx := strings.Index(raw, ":"); idx != -1 {
		prefix := strings.ToLower(strings.TrimSpace(raw[:idx]))
		if prefix == "cookie" {
			raw = strings.TrimSpace(raw[idx+1:])
		}
	}
	result := make(map[string]string)
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.Index(part, "=")
		if idx <= 0 {
			continue
		}
		name := strings.TrimSpace(part[:idx])
		value := strings.TrimSpace(part[idx+1:])
		if name != "" {
			result[name] = value
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("nenhum cookie válido encontrado na string informada")
	}
	return result, nil
}

// SetManualCookies salva os cookies colados pelo usuário em memória.
// Se persist=true, salva também em disco (~/.k8s-hpa-manager/sn-session.json).
func SetManualCookies(cookies map[string]string, persist bool) error {
	globalManualSession.mu.Lock()
	globalManualSession.cookies = cookies
	globalManualSession.savedAt = time.Now()
	globalManualSession.mu.Unlock()

	if persist {
		return saveSessionToDisk(cookies)
	}
	return nil
}

// GetManualCookies retorna os cookies salvos manualmente, ou nil se não houver.
func GetManualCookies() map[string]string {
	globalManualSession.mu.RLock()
	defer globalManualSession.mu.RUnlock()
	if len(globalManualSession.cookies) == 0 {
		return nil
	}
	// Retornar cópia
	cp := make(map[string]string, len(globalManualSession.cookies))
	for k, v := range globalManualSession.cookies {
		cp[k] = v
	}
	return cp
}

// GetManualSessionInfo retorna info da sessão manual (existe, válida, quando foi salva).
func GetManualSessionInfo() (exists bool, savedAt *time.Time, cookieCount int) {
	globalManualSession.mu.RLock()
	defer globalManualSession.mu.RUnlock()
	if len(globalManualSession.cookies) == 0 {
		// Tentar carregar do disco
		globalManualSession.mu.RUnlock()
		loadSessionFromDisk()
		globalManualSession.mu.RLock()
	}
	if len(globalManualSession.cookies) == 0 {
		return false, nil, 0
	}
	t := globalManualSession.savedAt
	return true, &t, len(globalManualSession.cookies)
}

// ClearManualSession remove cookies da memória e do disco.
func ClearManualSession() {
	globalManualSession.mu.Lock()
	globalManualSession.cookies = nil
	globalManualSession.savedAt = time.Time{}
	globalManualSession.mu.Unlock()
	os.Remove(sessionDiskPath())
}

func sessionDiskPath() string {
	home := os.Getenv("HOME")
	if home == "" {
		home = os.Getenv("USERPROFILE")
	}
	return filepath.Join(home, ".k8s-hpa-manager", "sn-session.json")
}

type diskSession struct {
	Cookies map[string]string `json:"cookies"`
	SavedAt time.Time         `json:"saved_at"`
}

func saveSessionToDisk(cookies map[string]string) error {
	path := sessionDiskPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(diskSession{Cookies: cookies, SavedAt: time.Now()}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func loadSessionFromDisk() {
	data, err := os.ReadFile(sessionDiskPath())
	if err != nil {
		return
	}
	var s diskSession
	if err := json.Unmarshal(data, &s); err != nil {
		return
	}
	if len(s.Cookies) == 0 {
		return
	}
	globalManualSession.mu.Lock()
	globalManualSession.cookies = s.Cookies
	globalManualSession.savedAt = s.SavedAt
	globalManualSession.mu.Unlock()
}

// InitManualSession carrega sessão salva do disco na inicialização.
func InitManualSession() {
	loadSessionFromDisk()
}
