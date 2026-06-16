package gcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// EnsureGKEAuthPlugin garante que o gke-gcloud-auth-plugin está instalado e que
// USE_GKE_GCLOUD_AUTH_PLUGIN=True está definido no processo.
//
// Chamado automaticamente na inicialização do servidor quando há clusters GKE
// configurados, e antes de cada ValidateAuth no GCPNodeGroupProvider.
//
// Instalação automática via gcloud components install. Se o gcloud foi instalado
// via apt (Debian/Ubuntu), o component manager fica desabilitado — nesse caso
// retorna erro com instrução manual clara.
func EnsureGKEAuthPlugin(logFunc func(string)) error {
	// Sempre definir — client-go 1.25+ exige para usar o exec credential provider
	os.Setenv("USE_GKE_GCLOUD_AUTH_PLUGIN", "True") //nolint:errcheck

	if _, err := exec.LookPath("gke-gcloud-auth-plugin"); err == nil {
		return nil // já instalado
	}

	if logFunc != nil {
		logFunc("[GKE] gke-gcloud-auth-plugin ausente — tentando instalar via gcloud...")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	out, err := exec.CommandContext(ctx,
		"gcloud", "components", "install", "gke-gcloud-auth-plugin", "--quiet",
	).CombinedOutput()

	if err != nil {
		outStr := strings.TrimSpace(string(out))

		// gcloud via apt desabilita o component manager
		if strings.Contains(outStr, "component manager is disabled") ||
			strings.Contains(outStr, "managed by your system's package manager") {
			return fmt.Errorf(
				"gke-gcloud-auth-plugin não instalado e o gcloud component manager está desabilitado.\n"+
					"Instale manualmente:\n"+
					"  sudo apt-get install google-cloud-sdk-gke-gcloud-auth-plugin\n"+
					"ou:\n"+
					"  sudo apt-get install google-cloud-cli-gke-gcloud-auth-plugin",
			)
		}

		return fmt.Errorf("falha ao instalar gke-gcloud-auth-plugin: %w\n%s", err, outStr)
	}

	// Verificar se ficou disponível no PATH após instalação
	if _, err := exec.LookPath("gke-gcloud-auth-plugin"); err != nil {
		return fmt.Errorf(
			"gke-gcloud-auth-plugin instalado mas não encontrado no PATH.\n" +
				"Reinicie o servidor ou adicione o diretório do gcloud ao PATH.",
		)
	}

	if logFunc != nil {
		logFunc("[GKE] gke-gcloud-auth-plugin instalado com sucesso")
	}
	return nil
}
