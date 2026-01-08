package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose-ca [url]",
	Short: "Diagnosticar configuração de certificados CA customizados",
	Long: `Verifica se os certificados CA customizados estão configurados corretamente
para validar conexões HTTPS com serviços internos da empresa.

Exemplo:
  new-k8s-hpa diagnose-ca https://login-entrega.viavarejo.com.br/`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		testURL := "https://login-entrega.viavarejo.com.br/"
		if len(args) > 0 {
			testURL = args[0]
		}

		fmt.Println("=== Diagnóstico de Certificados CA Customizados ===")
		fmt.Println()

		// 1. Verificar variável de ambiente
		fmt.Println("1. Verificando variável de ambiente CUSTOM_CA_BUNDLE...")
		customCAPath := os.Getenv("CUSTOM_CA_BUNDLE")
		if customCAPath != "" {
			fmt.Printf("   ✓ CUSTOM_CA_BUNDLE definida: %s\n", customCAPath)
			if _, err := os.Stat(customCAPath); err == nil {
				fmt.Println("   ✓ Arquivo existe")
			} else {
				fmt.Printf("   ✗ Arquivo não encontrado: %s\n", customCAPath)
			}
		} else {
			fmt.Println("   ⚠ CUSTOM_CA_BUNDLE não definida (usando locais padrão)")
		}
		fmt.Println()

		// 2. Verificar locais padrão
		fmt.Println("2. Verificando locais padrão de certificados...")
		possiblePaths := []string{
			"/etc/ssl/certs/ca-certificates.crt",
			"/etc/pki/tls/certs/ca-bundle.crt",
			"/etc/ssl/ca-bundle.pem",
			"/usr/local/share/ca-certificates/ca-bundle.crt",
			"./ca-certificates.crt",
		}

		foundCA := ""
		for _, path := range possiblePaths {
			if _, err := os.Stat(path); err == nil {
				fmt.Printf("   ✓ Encontrado: %s\n", path)
				if foundCA == "" {
					foundCA = path
				}
			} else {
				fmt.Printf("   - Não encontrado: %s\n", path)
			}
		}
		fmt.Println()

		// 3. Validar certificado
		certFile := customCAPath
		if certFile == "" {
			certFile = foundCA
		}

		if certFile != "" {
			fmt.Printf("3. Validando certificado: %s\n", certFile)
			caCert, err := os.ReadFile(certFile)
			if err != nil {
				fmt.Printf("   ✗ Erro ao ler arquivo: %v\n", err)
			} else {
				// Tentar criar cert pool
				pool := x509.NewCertPool()
				if ok := pool.AppendCertsFromPEM(caCert); ok {
					fmt.Printf("   ✓ Formato PEM válido (certificados carregados)\n")
				} else {
					fmt.Println("   ✗ Formato PEM inválido ou certificados corrompidos")
					fmt.Println("   Dica: Certificado deve começar com '-----BEGIN CERTIFICATE-----'")
				}
			}
		} else {
			fmt.Println("3. ✗ Nenhum certificado CA encontrado")
			fmt.Println("   Recomendação: Instalar certificado CA da empresa")
		}
		fmt.Println()

		// 4. Testar conectividade TLS
		fmt.Printf("4. Testando conectividade TLS com: %s\n", testURL)

		// Teste sem custom CA
		fmt.Print("   Sistema (sem custom CA): ")
		systemPool, err := x509.SystemCertPool()
		if err != nil {
			fmt.Printf("✗ Erro ao carregar pool do sistema: %v\n", err)
		} else {
			client := &http.Client{
				Timeout: 10 * time.Second,
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs: systemPool,
					},
				},
			}

			resp, err := client.Get(testURL)
			if err != nil {
				fmt.Printf("✗ Falhou (%v)\n", err)
			} else {
				resp.Body.Close()
				fmt.Printf("✓ Sucesso (HTTP %d)\n", resp.StatusCode)
			}
		}

		// Teste com custom CA
		if certFile != "" {
			fmt.Print("   Sistema + Custom CA: ")

			rootCAs, err := x509.SystemCertPool()
			if err != nil {
				rootCAs = x509.NewCertPool()
			}

			caCert, err := os.ReadFile(certFile)
			if err != nil {
				fmt.Printf("✗ Erro ao ler CA: %v\n", err)
			} else {
				rootCAs.AppendCertsFromPEM(caCert)

				client := &http.Client{
					Timeout: 10 * time.Second,
					Transport: &http.Transport{
						TLSClientConfig: &tls.Config{
							RootCAs: rootCAs,
						},
					},
				}

				resp, err := client.Get(testURL)
				if err != nil {
					fmt.Printf("✗ Falhou (%v)\n", err)
				} else {
					resp.Body.Close()
					fmt.Printf("✓ Sucesso (HTTP %d)\n", resp.StatusCode)
				}
			}
		}
		fmt.Println()

		// 5. Recomendações
		fmt.Println("=== Recomendações ===")
		if certFile == "" {
			fmt.Println("⚠ Nenhum certificado CA customizado encontrado")
			fmt.Println()
			fmt.Println("Passos recomendados:")
			fmt.Println("1. Obter o certificado CA raiz da empresa (exportar do navegador ou solicitar ao time de infra)")
			fmt.Println()
			fmt.Println("2. Instalar o certificado (escolha um método):")
			fmt.Println()
			fmt.Println("   Opção A - Variável de ambiente:")
			fmt.Println("   export CUSTOM_CA_BUNDLE=/caminho/para/empresa-ca.crt")
			fmt.Println()
			fmt.Println("   Opção B - Sistema (Debian/Ubuntu):")
			fmt.Println("   sudo cp empresa-ca.crt /usr/local/share/ca-certificates/")
			fmt.Println("   sudo update-ca-certificates")
			fmt.Println()
			fmt.Println("   Opção C - Local (desenvolvimento):")
			fmt.Println("   cp empresa-ca.crt ./ca-certificates.crt")
			fmt.Println()
			fmt.Println("3. Reiniciar o servidor web:")
			fmt.Println("   ./build/new-k8s-hpa web")
		} else {
			fmt.Printf("✓ Certificado CA encontrado: %s\n\n", certFile)
			fmt.Println("Se ainda houver erros de TLS:")
			fmt.Println("1. Verifique se o certificado é da CA raiz correta")
			fmt.Println("2. Verifique o formato PEM (deve conter 'BEGIN CERTIFICATE')")
			fmt.Println("3. Verifique logs do backend ao executar health check")
		}

		fmt.Println()
		fmt.Println("📖 Documentação completa: docs/guides/CUSTOM_CA_CERTIFICATES.md")
	},
}

func init() {
	rootCmd.AddCommand(diagnoseCmd)
}
