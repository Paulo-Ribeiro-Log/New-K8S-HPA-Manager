package vmssh

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// knownHostsFileName vive em ~/.k8s-hpa-manager/vm_known_hosts — arquivo próprio desta feature
// (nunca reaproveita/mistura com o known_hosts padrão do usuário em ~/.ssh/), formato OpenSSH via
// golang.org/x/crypto/ssh/knownhosts. Chaveado por host:porta — cada instância tem seu próprio
// host key, TOFU (trust-on-first-use) nunca reaproveitado entre instâncias diferentes.
const knownHostsFileName = "vm_known_hosts"

// KnownHostsPath resolve o caminho do arquivo, criando-o vazio (0600) se ainda não existir —
// golang.org/x/crypto/ssh/knownhosts.New exige que o arquivo já exista e seja abrível.
func KnownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("erro ao resolver home dir: %w", err)
	}
	dir := filepath.Join(home, ".k8s-hpa-manager")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("erro ao criar %s: %w", dir, err)
	}
	path := filepath.Join(dir, knownHostsFileName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte{}, 0600); err != nil {
			return "", fmt.Errorf("erro ao criar %s: %w", path, err)
		}
	}
	return path, nil
}

// HostKeyConfirmFunc é chamada quando o host:porta nunca foi visto antes (TOFU) — deve bloquear e
// devolver true só quando o usuário confirmar explicitamente a fingerprint apresentada. Nunca
// chamada quando a chave já é conhecida E bate (retorno direto nil) nem quando a chave MUDOU
// (rejeitado sempre, sem chance de "aceitar" — ver buildHostKeyCallback).
type HostKeyConfirmFunc func(fingerprint string) (bool, error)

// buildHostKeyCallback monta o HostKeyCallback real usado por ssh.ClientConfig — 3 desfechos
// possíveis por chave apresentada:
//  1. já conhecida e bate → nil (aceita, handshake segue normalmente)
//  2. já conhecida mas DIFERENTE (KeyError.Want não-vazio) → SEMPRE rejeita, nunca delega pra
//     confirmFunc — uma chave que mudou é sinal de possível MITM ou reinstalação do servidor, não
//     um caso de "primeira vez", e não deveria ser contornável só clicando "aceitar" de novo.
//  3. desconhecida (KeyError.Want vazio) → delega pra confirmFunc; se aceita, grava no arquivo e
//     retorna nil; se recusada, retorna erro (handshake falha, conexão nunca chega a completar).
func buildHostKeyCallback(path string, confirmFunc HostKeyConfirmFunc) (ssh.HostKeyCallback, error) {
	base, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir known_hosts (%s): %w", path, err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := base(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) > 0 {
			return fmt.Errorf(
				"a host key de %s MUDOU desde a última conexão confiada — possível MITM ou reinstalação do servidor; "+
					"se tiver certeza que é legítimo, remova a entrada antiga de %s manualmente antes de tentar de novo",
				hostname, path,
			)
		}

		// Desconhecida (Want vazio) — TOFU, exige confirmação explícita.
		fp := ssh.FingerprintSHA256(key)
		accepted, confirmErr := confirmFunc(fp)
		if confirmErr != nil {
			return confirmErr
		}
		if !accepted {
			return fmt.Errorf("host key de %s (%s) rejeitada pelo usuário", hostname, fp)
		}

		if err := appendKnownHost(path, hostname, key); err != nil {
			return fmt.Errorf("host key aceita, mas erro ao gravar em %s: %w", path, err)
		}
		return nil
	}, nil
}

func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	line := knownhosts.Line([]string{hostname}, key) + "\n"
	_, err = f.WriteString(line)
	return err
}
