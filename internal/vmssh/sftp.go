package vmssh

import (
	"fmt"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// OpenSFTP abre um client SFTP real (github.com/pkg/sftp) sobre uma sessão SSH já conectada — bem
// mais simples que internal/podsftp (que existe só porque um Pod K8s não tem sshd/SFTP nativo,
// exigindo montar um servidor SFTP em memória sobre kubectl exec/cp, ver
// internal/podsftp/handlers.go). Uma VM real já tem sshd de verdade, então sftp.NewClient(client)
// já basta — nenhum servidor/net.Pipe() precisa ser simulado aqui.
func OpenSFTP(client *ssh.Client) (*sftp.Client, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, fmt.Errorf("erro ao abrir sessão SFTP: %w", err)
	}
	return sftpClient, nil
}
