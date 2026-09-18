package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ssm_command.go — executa comandos remotos via AWS SSM Run Command (documento
// "AWS-RunShellScript"), sem depender de SSH/sshd em nenhum momento — só do próprio agente SSM,
// o mesmo já exigido pelo terminal via SSM (vm_terminal.go) e pelo túnel de port-forwarding
// (vm_ssm_tunnel.go). Motivado por um caso real: instância gerida só via SSM, sem sshd
// instalado/rodando — nesse cenário nem o túnel SSM (que só encaminha TCP, ainda precisa de um
// sshd do outro lado) resolve, então SFTP nunca é uma opção. RunShellCommand é o mecanismo que
// destrava ler/gravar um arquivo (ex: certificado TLS) mesmo nesse caso.

// ssmCommandPollInterval/ssmCommandPollTimeout — AWS tem uma janela real de inconsistência
// eventual logo após send-command, onde get-command-invocation pode responder
// "InvocationDoesNotExist" por 1-2s (a invocação ainda não propagou) — tratado como "ainda não
// pronto", não como erro definitivo, até o timeout total. `var`, não `const` — permite teste
// determinístico encolhendo os dois valores (mesmo padrão já usado em internal/browser/
// idle_reap_test.go pro idleTimeout do Manager de browser persistente).
var (
	ssmCommandPollInterval = 1 * time.Second
	ssmCommandPollTimeout  = 30 * time.Second
)

// SSMCommandResult é o resultado terminal de uma invocação (Status final + saída capturada).
type SSMCommandResult struct {
	Status                string
	StandardOutputContent string
	StandardErrorContent  string
}

type ssmSendCommandResponse struct {
	Command struct {
		CommandID string `json:"CommandId"`
	} `json:"Command"`
}

type ssmGetInvocationResponse struct {
	Status                string `json:"Status"`
	StandardOutputContent string `json:"StandardOutputContent"`
	StandardErrorContent  string `json:"StandardErrorContent"`
}

// ssmTerminalStatus reconhece os status finais documentados da API (qualquer outro —
// Pending/InProgress/Delayed/Cancelling — significa "ainda rodando, continue esperando").
func ssmTerminalStatus(status string) bool {
	switch status {
	case "Success", "Failed", "Cancelled", "TimedOut":
		return true
	default:
		return false
	}
}

// ShellQuote escapa s pra uso seguro dentro de aspas simples POSIX (fecha a aspa, insere um `\'`
// literal, reabre) — usado pra montar cada linha de script remoto sem depender de o conteúdo
// (caminho de arquivo, texto base64) nunca conter um caractere problemático pro shell de destino.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// RunShellCommand roda `commands` (uma linha de shell por elemento, mesma ordem, mesmo estado de
// shell entre elas — equivalente a um script) via SSM Run Command contra instanceID, e espera a
// invocação terminar. Reaproveita o mesmo lock de profile de runAWSCLI (evita a mesma contenção de
// lock SQLite do cache de sessão SSO do AWS CLI v2 já documentada em awsCLIProfileLock).
//
// `--parameters` é passado como JSON literal (via encoding/json.Marshal, nunca concatenação de
// string) — o AWS CLI aceita JSON direto nesse parâmetro, o que evita por completo qualquer
// ambiguidade da sintaxe "shorthand" (vírgulas/chaves) que a CLI usaria pra um valor digitado à
// mão; como este processo nunca passa por um shell (exec.CommandContext recebe args já separados),
// não há escaping duplo a se preocupar no lado desta aplicação.
func RunShellCommand(ctx context.Context, profile, region, instanceID string, commands []string) (*SSMCommandResult, error) {
	if instanceID == "" {
		return nil, fmt.Errorf("instanceID é obrigatório")
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("commands não pode ser vazio")
	}

	params := struct {
		Commands []string `json:"commands"`
	}{Commands: commands}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("erro ao montar parâmetros do comando: %w", err)
	}

	sendArgs := buildAWSArgs(region, profile, "ssm", "send-command",
		"--instance-ids", instanceID,
		"--document-name", "AWS-RunShellScript",
		"--parameters", string(paramsJSON),
	)
	sendOut, err := runAWSCLI(ctx, profile, sendArgs)
	if err != nil {
		return nil, fmt.Errorf("erro ao enviar comando via SSM: %w", err)
	}
	var sendResp ssmSendCommandResponse
	if err := json.Unmarshal(sendOut, &sendResp); err != nil {
		return nil, fmt.Errorf("resposta inesperada do send-command: %w", err)
	}
	if sendResp.Command.CommandID == "" {
		return nil, fmt.Errorf("send-command não retornou CommandId (instância pode não ter o agente SSM ativo)")
	}

	getArgs := buildAWSArgs(region, profile, "ssm", "get-command-invocation",
		"--command-id", sendResp.Command.CommandID,
		"--instance-id", instanceID,
	)

	deadline := time.Now().Add(ssmCommandPollTimeout)
	for {
		out, err := runAWSCLI(ctx, profile, getArgs)
		if err == nil {
			var inv ssmGetInvocationResponse
			if jerr := json.Unmarshal(out, &inv); jerr != nil {
				return nil, fmt.Errorf("resposta inesperada do get-command-invocation: %w", jerr)
			}
			if ssmTerminalStatus(inv.Status) {
				return &SSMCommandResult{
					Status:                inv.Status,
					StandardOutputContent: inv.StandardOutputContent,
					StandardErrorContent:  inv.StandardErrorContent,
				}, nil
			}
		} else if time.Now().After(deadline) {
			// InvocationDoesNotExist persistindo até o deadline é erro de verdade — antes
			// disso, é só a propagação assíncrona ainda não ter chegado.
			return nil, fmt.Errorf("erro ao consultar status do comando: %w", err)
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout aguardando o comando SSM terminar")
		}
		select {
		case <-time.After(ssmCommandPollInterval):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
