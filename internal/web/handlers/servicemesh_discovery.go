package handlers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KialiAuthConfig representa a configuração de autenticação do Kiali
type KialiAuthConfig struct {
	Strategy string `json:"strategy"`
}

// KialiConfig representa a resposta da API /api/config do Kiali
type KialiConfig struct {
	Auth KialiAuthConfig `json:"auth"`
}

// Cache de tokens por cluster
var (
	tokenCache      = make(map[string]*cachedToken)
	tokenCacheMutex sync.RWMutex
)

type cachedToken struct {
	Token     string
	ExpiresAt time.Time
}

// buildKialiURL constrói a URL externa do Kiali baseado no nome do cluster
// Pattern: https://kiali-<nome>-<ambiente>.grupocasasbahia.com.br/kiali/
// Exemplo: akspriv-abastecimento-hlg-admin → https://kiali-akspriv-abastecimento-hlg.grupocasasbahia.com.br/kiali/
func buildKialiURL(cluster string) string {
	// Remove sufixo "-admin"
	clean := strings.TrimSuffix(cluster, "-admin")

	// Pattern da URL do Kiali (Ingress externo com base path /kiali/)
	return fmt.Sprintf("https://kiali-%s.grupocasasbahia.com.br/kiali/", clean)
}

// validateKialiURL verifica se a URL do Kiali está acessível
// Retorna (isValid bool, needsAuth bool)
func validateKialiURL(url string) (bool, bool) {
	// Cliente HTTP com SSL auto-assinado permitido
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Certificado auto-assinado
			},
		},
	}

	// Testar endpoint /api/config (sempre requer autenticação se auth:token)
	configURL := url + "api/config"

	resp, err := client.Get(configURL)
	if err != nil {
		fmt.Printf("[ServiceMesh] Erro ao validar Kiali URL %s: %v\n", url, err)
		return false, false
	}
	defer resp.Body.Close()

	// 401 = URL válida mas precisa de autenticação (auth:token)
	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Printf("[ServiceMesh] 🔐 Kiali URL requer autenticação (401 em /api/config): %s\n", url)
		return true, true
	}

	// 403 = URL válida mas acesso negado sem token (auth:token)
	if resp.StatusCode == http.StatusForbidden {
		fmt.Printf("[ServiceMesh] 🔐 Kiali URL requer autenticação (403 em /api/config): %s\n", url)
		return true, true
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[ServiceMesh] Kiali URL retornou status %d em /api/config: %s\n", resp.StatusCode, url)
		return false, false
	}

	fmt.Printf("[ServiceMesh] ✅ Kiali URL validada (auth:anonymous): %s\n", url)
	return true, false
}

// getKialiURL retorna a URL do Kiali para o cluster (auto-discovery)
// Retorna (url string, needsAuth bool, error)
func getKialiURL(cluster string) (string, bool, error) {
	url := buildKialiURL(cluster)

	// Validar se URL está acessível
	isValid, needsAuth := validateKialiURL(url)
	if !isValid {
		return "", false, fmt.Errorf("kiali não acessível em: %s", url)
	}

	return url, needsAuth, nil
}

// getKialiAuthStrategy verifica a estratégia de autenticação do Kiali
// Retorna "anonymous", "token" ou erro
func getKialiAuthStrategy(kialiURL string, authToken string) (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// Endpoint de configuração do Kiali
	configURL := kialiURL + "api/config"
	
	req, err := http.NewRequest("GET", configURL, nil)
	if err != nil {
		return "", fmt.Errorf("erro ao criar requisição: %w", err)
	}

	// Se temos um token, adicionar ao header
	if authToken != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authToken))
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("erro ao consultar config do Kiali: %w", err)
	}
	defer resp.Body.Close()

	// Se retornar 401 sem token, significa que precisa de autenticação
	if resp.StatusCode == http.StatusUnauthorized && authToken == "" {
		fmt.Printf("[ServiceMesh] Kiali retornou 401 - autenticação necessária\n")
		return "token", nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("kiali config retornou status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("erro ao ler resposta: %w", err)
	}

	var config KialiConfig
	if err := json.Unmarshal(body, &config); err != nil {
		return "", fmt.Errorf("erro ao decodificar config: %w", err)
	}

	fmt.Printf("[ServiceMesh] Kiali auth strategy: %s\n", config.Auth.Strategy)
	return config.Auth.Strategy, nil
}

// createKialiToken cria um token de service account para o Kiali
func createKialiToken(clientset kubernetes.Interface, namespace string, clusterName string) (string, error) {
	ctx := context.Background()

	fmt.Printf("[ServiceMesh] 🎫 Criando token para cluster '%s', service account 'kiali' no namespace '%s'...\n", clusterName, namespace)

	// Verificar se o service account existe
	sa, err := clientset.CoreV1().ServiceAccounts(namespace).Get(ctx, "kiali", metav1.GetOptions{})
	if err != nil {
		fmt.Printf("[ServiceMesh] ❌ Service account 'kiali' não encontrado no namespace '%s': %v\n", namespace, err)
		return "", fmt.Errorf("service account 'kiali' não encontrado: %w", err)
	}
	fmt.Printf("[ServiceMesh] ✅ Service account 'kiali' encontrado no namespace '%s'\n", namespace)

	// Criar TokenRequest para o service account kiali
	tokenRequest := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			// Token com duração de 1 hora
			ExpirationSeconds: func(i int64) *int64 { return &i }(3600),
		},
	}

	result, err := clientset.CoreV1().ServiceAccounts(namespace).CreateToken(
		ctx,
		sa.Name,
		tokenRequest,
		metav1.CreateOptions{},
	)

	if err != nil {
		fmt.Printf("[ServiceMesh] ❌ Erro ao criar token: %v\n", err)
		return "", fmt.Errorf("erro ao criar token: %w", err)
	}

	tokenPreview := result.Status.Token
	if len(tokenPreview) > 20 {
		tokenPreview = tokenPreview[:20] + "..."
	}
	fmt.Printf("[ServiceMesh] ✅ Token criado com sucesso para cluster '%s' (início: %s, comprimento: %d, expira em 1h)\n", 
		clusterName, tokenPreview, len(result.Status.Token))
	return result.Status.Token, nil
}

// getOrCreateKialiToken obtém um token do cache ou cria um novo
func getOrCreateKialiToken(clientset kubernetes.Interface, cluster, namespace string) (string, error) {
	cacheKey := fmt.Sprintf("%s:%s", cluster, namespace)

	// Verificar cache
	tokenCacheMutex.RLock()
	cached, exists := tokenCache[cacheKey]
	tokenCacheMutex.RUnlock()

	if exists && time.Now().Before(cached.ExpiresAt) {
		fmt.Printf("[ServiceMesh] Usando token em cache para %s\n", cacheKey)
		return cached.Token, nil
	}

	// Token expirado ou não existe, criar novo
	token, err := createKialiToken(clientset, namespace, cluster)
	if err != nil {
		return "", err
	}

	// Guardar no cache (expira 5 minutos antes do real para segurança)
	tokenCacheMutex.Lock()
	tokenCache[cacheKey] = &cachedToken{
		Token:     token,
		ExpiresAt: time.Now().Add(55 * time.Minute),
	}
	tokenCacheMutex.Unlock()

	return token, nil
}

// authenticateKiali faz login no Kiali usando token e retorna HTTP client com sessão
func authenticateKiali(kialiURL, token string) (*http.Client, error) {
	// Criar cookiejar para gerenciar sessão
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar cookiejar: %w", err)
	}
	
	// Criar HTTP client com suporte a cookies e TLS
	client := &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	
	// Endpoint de autenticação do Kiali
	authURL := kialiURL + "api/authenticate"

	fmt.Printf("[ServiceMesh] 🔐 Autenticando no Kiali: %s\n", authURL)

	// Payload: Kiali espera form-data (application/x-www-form-urlencoded), NÃO JSON
	formData := fmt.Sprintf("token=%s", token)

	// Fazer POST para autenticar
	req, err := http.NewRequest("POST", authURL, strings.NewReader(formData))
	if err != nil {
		return nil, fmt.Errorf("erro ao criar requisição: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro na autenticação: %w", err)
	}
	defer resp.Body.Close()
	
	fmt.Printf("[ServiceMesh] Status da autenticação: %d\n", resp.StatusCode)
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("[ServiceMesh] Erro de autenticação: %s\n", string(body))
		return nil, fmt.Errorf("autenticação falhou com status %d: %s", resp.StatusCode, string(body))
	}
	
	// Ler resposta (contém informações do usuário)
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("[ServiceMesh] ✅ Autenticação bem-sucedida, sessão estabelecida\n")
	fmt.Printf("[ServiceMesh] Resposta: %s\n", string(body))
	
	// Retornar client com cookies de sessão configurados
	return client, nil
}
