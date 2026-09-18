package aws

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
)

// awsCLIProfileLocks — serializa chamadas concorrentes de `aws` CLI que compartilham o MESMO
// profile (um semáforo binário por nome de profile, criado sob demanda).
//
// Bug real corrigido — relatado pelo usuário como "a aplicação parou de comunicar com a aws" logo
// após um ajuste que era 100% frontend (sem nenhuma linha de Go tocada). Investigado com
// reprodução real, fora da aplicação: capturado o ambiente exato do processo do servidor
// (`/proc/<pid>/environ`) e disparadas N chamadas `aws ec2 describe-instances --profile asaplog`
// concorrentes (via goroutines/threads) — confirmado que a maioria das chamadas concorrentes sob o
// MESMO profile *trava indefinidamente* (nunca retorna, nem erro nem dado, até o timeout de
// contexto matar o subprocesso — daí o "signal: killed" nos logs), enquanto uma chamada ISOLADA
// sempre responde em ~1s. Causa raiz: o AWS CLI v2 (a partir de uma versão recente, confirmado
// nesta máquina com `aws-cli/2.34.18`) passou a cachear a sessão SSO do IAM Identity Center num
// banco SQLite (`~/.aws/cli/cache/session.db`, confirmado via `file`), substituindo os arquivos
// JSON avulsos de antes — SQLite usa lock exclusivo de ESCRITA no arquivo, e chamadas concorrentes
// da CLI compartilhando o mesmo profile competem por esse lock sem nenhum backoff/retry
// configurado, travando ("database is locked") em vez de enfileirar e seguir.
//
// Isso é uma limitação de AMBIENTE (comportamento do próprio AWS CLI v2), não um bug introduzido
// por nenhuma mudança de código desta sessão — só ficou mais visível quando a aba VMs/EC2
// (describe-instances) passou a ser MAIS UM consumidor concorrente do mesmo profile já usado pela
// aba Node Pools (describe-nodegroup, disparado uma vez por node group em paralelo) e por
// autenticação (sts get-caller-identity) — antes disso, a chance de duas chamadas coincidirem no
// mesmo instante já existia, só era menor.
//
// Bug real corrigido na 1ª versão desta correção (achado validando ao vivo, antes de considerar
// pronto): usar um `*sync.Mutex` cru faz uma chamada cujo CONTEXTO já expirou continuar bloqueada
// esperando o lock mesmo assim (sync.Mutex não tem noção de cancelamento) — só falha DEPOIS de
// finalmente conseguir o lock, desperdiçando toda a fila de espera com um trabalho que já nasceu
// morto, e sem nunca chegar a tentar `aws` de verdade pra essas chamadas. Corrigido com um
// semáforo baseado em canal (`chan struct{}` de capacidade 1) — `Lock(ctx)` usa `select` entre
// receber do canal (conseguiu o lock) e `ctx.Done()` (desiste, devolvendo erro claro), então uma
// chamada some da fila assim que o PRÓPRIO prazo dela expira, sem nunca bloquear além disso.
type profileSemaphore chan struct{}

func newProfileSemaphore() profileSemaphore {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	return ch
}

func (s profileSemaphore) Lock(ctx context.Context) error {
	select {
	case <-s:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s profileSemaphore) Unlock() {
	s <- struct{}{}
}

var (
	awsCLIProfileLocksMu sync.Mutex
	awsCLIProfileLocks   = map[string]profileSemaphore{}
)

func awsCLIProfileLock(profile string) profileSemaphore {
	awsCLIProfileLocksMu.Lock()
	defer awsCLIProfileLocksMu.Unlock()
	s, ok := awsCLIProfileLocks[profile]
	if !ok {
		s = newProfileSemaphore()
		awsCLIProfileLocks[profile] = s
	}
	return s
}

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
// processo sai com falha). `profile` serializa chamadas concorrentes que compartilham o mesmo
// profile (ver comentário de awsCLIProfileLock acima) — a espera pelo lock respeita `ctx`, então
// uma chamada cujo prazo já expirou enquanto esperava a vez nunca chega a segurar o lock à toa; o
// lock em si só é retido pela duração real do subprocesso, e `exec.CommandContext` já mata o
// processo (liberando o lock) se o contexto expirar DEPOIS de já estar rodando.
func runAWSCLI(ctx context.Context, profile string, args []string) ([]byte, error) {
	lock := awsCLIProfileLock(profile)
	if err := lock.Lock(ctx); err != nil {
		return nil, fmt.Errorf("aguardando outra chamada AWS CLI no mesmo profile (%q): %w", profile, err)
	}
	defer lock.Unlock()

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
