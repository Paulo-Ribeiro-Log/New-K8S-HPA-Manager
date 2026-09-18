// Package vmssh implementa conexão SSH direta a VMs/instâncias fora de qualquer cluster K8s —
// terminal interativo (terminal.go) e SFTP (sftp.go, Fase 4). Reaproveita golang.org/x/crypto/ssh
// como client de verdade (diferente de internal/podsftp, que existe só porque um Pod K8s não tem
// SFTP nativo). Ver plano em /home/paulo/.claude/plans/scalable-greeting-kazoo.md.
package vmssh

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// dialTimeout — timeout da conexão TCP em si (antes do handshake SSH). O handshake/auth em si
// não tem timeout próprio aqui (ssh.NewClientConn não aceita context) — a confirmação de host key
// desconhecida (TOFU) pode legitimamente levar minutos (usuário lendo/decidindo), então não faz
// sentido um timeout curto cobrindo esse trecho.
const dialTimeout = 15 * time.Second

// AuthConfig descreve como autenticar — chave privada (com passphrase opcional) OU senha, nunca
// os dois ao mesmo tempo (authMethods escolhe chave se presente, senão senha).
type AuthConfig struct {
	Username      string
	PrivateKeyPEM []byte // opcional
	Passphrase    string // só usada se PrivateKeyPEM setado e a chave estiver cifrada
	Password      string // usada só se PrivateKeyPEM vazio
}

func (a AuthConfig) authMethods() ([]ssh.AuthMethod, error) {
	if len(a.PrivateKeyPEM) > 0 {
		var signer ssh.Signer
		var err error
		if a.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(a.PrivateKeyPEM, []byte(a.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(a.PrivateKeyPEM)
		}
		if err != nil {
			return nil, fmt.Errorf("chave privada SSH inválida (ou passphrase incorreta): %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	}
	if a.Password != "" {
		return []ssh.AuthMethod{ssh.Password(a.Password)}, nil
	}
	return nil, fmt.Errorf("credencial SSH incompleta: informe chave privada ou senha")
}

// Dial abre uma conexão SSH — dialAddr ("host:porta") é o endereço TCP de verdade discado;
// hostKeyIdentity é a chave usada no known_hosts (TOFU) pra identificar o destino. Nos casos
// normais (SSH direto) os dois são o mesmo valor — dial() abaixo cobre isso. Precisam ser
// SEPARADOS quando o endereço de discagem não é estável entre sessões (ex: túnel SSM, onde a
// porta local é escolhida aleatoriamente a cada abertura — ver vm_ssm_tunnel.go/vm_sftp.go):
// chavear o known_hosts pela porta local aleatória faria o TOFU nunca bater duas vezes seguidas,
// sempre caindo no caminho de host-key-desconhecida (que numa rota REST sem confirmação
// interativa sempre rejeita) mesmo contra o mesmo sshd real por trás do túnel.
//
// A confirmação de host key desconhecida delega pra confirmFunc, que deve bloquear até o usuário
// decidir (ver internal/web/handlers/vm_terminal.go, onde confirmFunc manda a fingerprint pelo
// WebSocket e espera a resposta). Nunca aceita uma host key silenciosamente.
func Dial(ctx context.Context, dialAddr, hostKeyIdentity string, auth AuthConfig, confirmFunc HostKeyConfirmFunc) (*ssh.Client, error) {
	methods, err := auth.authMethods()
	if err != nil {
		return nil, err
	}

	knownHostsPath, err := KnownHostsPath()
	if err != nil {
		return nil, err
	}
	hostKeyCallback, err := buildHostKeyCallback(knownHostsPath, confirmFunc)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            auth.Username,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         dialTimeout,
	}

	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", dialAddr)
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar em %s: %w", dialAddr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, hostKeyIdentity, cfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("erro no handshake SSH com %s: %w", hostKeyIdentity, err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}
