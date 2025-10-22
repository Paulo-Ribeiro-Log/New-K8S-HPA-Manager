package azure

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// AuthManager gerencia a autenticação com Azure AD
type AuthManager struct {
	credential azcore.TokenCredential
	tenantID   string
	clientID   string
}

// AuthResult resultado da autenticação
type AuthResult struct {
	Success    bool
	Token      string
	ExpiresAt  time.Time
	Error      string
}

// NewAuthManager cria um novo gerenciador de autenticação
func NewAuthManager() *AuthManager {
	return &AuthManager{
		// Usar Application ID padrão do Azure CLI para compatibilidade
		clientID: "04b07795-8ddb-461a-bbee-02f9e1bf7b46",
		tenantID: getEnvOrDefault("AZURE_TENANT_ID", "common"),
	}
}

// Authenticate realiza a autenticação usando Interactive Browser Flow com fallback para Device Code
func (a *AuthManager) Authenticate(ctx context.Context) (*AuthResult, error) {
	// Tentar Interactive Browser Flow primeiro
	result, err := a.authenticateInteractive(ctx)
	if err == nil {
		return result, nil
	}

	// Se falhar, tentar Device Code Flow
	fmt.Printf("🔄 Browser authentication failed, trying device code flow...\n")
	return a.authenticateDeviceCode(ctx)
}

// authenticateInteractive usa Interactive Browser Flow
func (a *AuthManager) authenticateInteractive(ctx context.Context) (*AuthResult, error) {
	fmt.Printf("🌐 Starting Azure authentication via browser...\n")
	
	options := &azidentity.InteractiveBrowserCredentialOptions{
		ClientID: a.clientID,
		TenantID: a.tenantID,
	}
	
	cred, err := azidentity.NewInteractiveBrowserCredential(options)
	if err != nil {
		return nil, fmt.Errorf("failed to create interactive browser credential: %w", err)
	}
	
	// Testar a autenticação obtendo um token
	token, err := a.getToken(ctx, cred)
	if err != nil {
		return nil, err
	}
	
	a.credential = cred
	fmt.Printf("✅ Successfully authenticated via browser\n")
	
	return &AuthResult{
		Success:   true,
		Token:     token.Token,
		ExpiresAt: token.ExpiresOn,
	}, nil
}

// authenticateDeviceCode usa Device Code Flow
func (a *AuthManager) authenticateDeviceCode(ctx context.Context) (*AuthResult, error) {
	fmt.Printf("📱 Starting Azure authentication via device code...\n")
	
	options := &azidentity.DeviceCodeCredentialOptions{
		ClientID: a.clientID,
		TenantID: a.tenantID,
		UserPrompt: func(ctx context.Context, message azidentity.DeviceCodeMessage) error {
			fmt.Printf("\n🔐 Azure Authentication Required\n")
			fmt.Printf("📋 Instructions:\n")
			fmt.Printf("   1. Open: %s\n", message.VerificationURL)
			fmt.Printf("   2. Enter code: %s\n", message.UserCode)
			fmt.Printf("   3. Sign in with your Azure credentials\n")
			fmt.Printf("\n⏳ Waiting for authentication...\n\n")
			return nil
		},
	}
	
	cred, err := azidentity.NewDeviceCodeCredential(options)
	if err != nil {
		return nil, fmt.Errorf("failed to create device code credential: %w", err)
	}
	
	// Testar a autenticação obtendo um token
	token, err := a.getToken(ctx, cred)
	if err != nil {
		return nil, err
	}
	
	a.credential = cred
	fmt.Printf("✅ Successfully authenticated via device code\n")
	
	return &AuthResult{
		Success:   true,
		Token:     token.Token,
		ExpiresAt: token.ExpiresOn,
	}, nil
}

// getToken obtém um token para validar a autenticação
func (a *AuthManager) getToken(ctx context.Context, cred azcore.TokenCredential) (azcore.AccessToken, error) {
	// Solicitar token para Azure Resource Manager
	return cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://management.azure.com/.default"},
	})
}

// GetCredential retorna a credencial autenticada
func (a *AuthManager) GetCredential() azcore.TokenCredential {
	return a.credential
}

// IsAuthenticated verifica se já está autenticado
func (a *AuthManager) IsAuthenticated() bool {
	return a.credential != nil
}

// EnsureAzureLogin verifica se estamos logados no Azure e faz login se necessário
func EnsureAzureLogin() error {
	// Verificar se Azure CLI está autenticado
	cmd := exec.Command("az", "account", "show")
	err := cmd.Run()
	if err == nil {
		return nil // Já está logado
	}

	// Se não está logado, tentar fazer login
	fmt.Printf("🔄 Azure CLI não está autenticado. Tentando fazer login...\n")
	loginCmd := exec.Command("az", "login")
	return loginCmd.Run()
}

// SetAzureCliContext verifica se o Azure CLI está autenticado
func (a *AuthManager) SetAzureCliContext(ctx context.Context) error {
	if !a.IsAuthenticated() {
		return fmt.Errorf("not authenticated")
	}
	
	// Verificar se Azure CLI está autenticado
	fmt.Printf("🔧 Checking Azure CLI authentication...\n")
	
	cmd := exec.Command("az", "account", "show")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Azure CLI não está autenticado. Execute: az login --tenant %s", a.tenantID)
	}
	
	// Obter token atual para mostrar informações
	token, err := a.getToken(ctx, a.credential)
	if err != nil {
		return fmt.Errorf("failed to get current token: %w", err)
	}
	
	fmt.Printf("✅ Azure CLI authentication verified\n")
	fmt.Printf("📅 Token expires at: %s\n", token.ExpiresOn.Format("2006-01-02 15:04:05"))
	fmt.Printf("Account info: %s\n", string(output))
	
	return nil
}

// getEnvOrDefault obtém variável de ambiente ou retorna valor padrão
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}