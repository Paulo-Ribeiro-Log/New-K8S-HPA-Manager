package aws

import (
	"context"
	"fmt"
	"os/exec"
)

// buildAWSArgs monta os argumentos comuns pra qualquer chamada `aws <serviço> ...` — região,
// perfil e --output json. Extraído de AWSNodeGroupProvider.baseArgs (nodegroup.go) pra ser
// compartilhado com AWSEC2Provider (ec2.go) sem duplicar a lógica. Comportamento idêntico ao
// método original, que agora só delega pra esta função.
func buildAWSArgs(region, profile string, subcmd ...string) []string {
	args := append([]string{}, subcmd...)
	if region != "" {
		args = append(args, "--region", region)
	}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	args = append(args, "--output", "json")
	return args
}

// runAWSCLI executa `aws <args...>` e devolve o stdout — extraído de
// AWSNodeGroupProvider.run (nodegroup.go), mesmo comportamento (stderr embutido no erro quando o
// processo sai com falha).
func runAWSCLI(ctx context.Context, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "aws", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%w: %s", err, string(exitErr.Stderr))
		}
		return nil, err
	}
	return out, nil
}
