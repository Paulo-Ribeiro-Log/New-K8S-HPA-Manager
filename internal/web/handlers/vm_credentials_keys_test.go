package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateSSHKeyPair_RSA(t *testing.T) {
	privPEM, pubAuthorized, err := generateSSHKeyPair("rsa", 2048)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	signer, err := ssh.ParsePrivateKey(privPEM)
	if err != nil {
		t.Fatalf("PEM gerado não parseou como chave privada SSH: %v", err)
	}
	if signer.PublicKey().Type() != "ssh-rsa" {
		t.Errorf("tipo de chave = %q, esperado ssh-rsa", signer.PublicKey().Type())
	}
	if !strings.HasPrefix(pubAuthorized, "ssh-rsa ") {
		t.Errorf("authorized_keys = %q, esperado prefixo 'ssh-rsa '", pubAuthorized)
	}
}

func TestGenerateSSHKeyPair_Ed25519(t *testing.T) {
	privPEM, pubAuthorized, err := generateSSHKeyPair("ed25519", 0)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	signer, err := ssh.ParsePrivateKey(privPEM)
	if err != nil {
		t.Fatalf("PEM gerado não parseou como chave privada SSH: %v", err)
	}
	if signer.PublicKey().Type() != "ssh-ed25519" {
		t.Errorf("tipo de chave = %q, esperado ssh-ed25519", signer.PublicKey().Type())
	}
	if !strings.HasPrefix(pubAuthorized, "ssh-ed25519 ") {
		t.Errorf("authorized_keys = %q, esperado prefixo 'ssh-ed25519 '", pubAuthorized)
	}
}

func TestGenerateSSHKeyPair_RSABitsMuitoPequeno(t *testing.T) {
	if _, _, err := generateSSHKeyPair("rsa", 1024); err == nil {
		t.Fatal("esperado erro para chave RSA de 1024 bits, veio nil")
	}
}

func TestGenerateSSHKeyPair_TipoInvalido(t *testing.T) {
	if _, _, err := generateSSHKeyPair("dsa", 0); err == nil {
		t.Fatal("esperado erro para keyType desconhecido, veio nil")
	}
}

func TestLooksLikePrivateKeyPEM(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"rsa real", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow...\n-----END RSA PRIVATE KEY-----\n", true},
		{"openssh real", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaA...\n-----END OPENSSH PRIVATE KEY-----\n", true},
		{"chave publica", "ssh-rsa AAAAB3NzaC1yc2EA... comment\n", false},
		{"known_hosts", "github.com ssh-rsa AAAAB3Nz...\n", false},
		{"vazio", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikePrivateKeyPEM([]byte(tc.data)); got != tc.want {
				t.Errorf("looksLikePrivateKeyPEM(%q) = %v, esperado %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestListLocalSSHPrivateKeys(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	sshDir := filepath.Join(tmpHome, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(sshDir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	privKeyPEM := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaA...\n-----END OPENSSH PRIVATE KEY-----\n"
	write("id_ed25519", privKeyPEM)
	write("id_ed25519.pub", "ssh-ed25519 AAAAC3Nz... comment\n")
	write("id_rsa", privKeyPEM)
	write("known_hosts", "github.com ssh-rsa AAAA...\n")
	write("config", "Host *\n  User foo\n")

	entries, err := listLocalSSHPrivateKeys()
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("esperado 2 candidatos (id_ed25519, id_rsa), veio %d: %+v", len(entries), entries)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
		if filepath.Dir(e.Path) != sshDir {
			t.Errorf("path %q não está dentro de %q", e.Path, sshDir)
		}
	}
	if !names["id_ed25519"] || !names["id_rsa"] {
		t.Errorf("candidatos = %+v, esperado id_ed25519 e id_rsa", entries)
	}
}

func TestListLocalSSHPrivateKeys_DiretorioAusente(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // sem ~/.ssh criado

	entries, err := listLocalSSHPrivateKeys()
	if err != nil {
		t.Fatalf("diretório ausente não deveria ser erro, veio: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("esperado lista vazia, veio %+v", entries)
	}
}
