package handlers

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"

	"k8s-hpa-manager/internal/storage"
)

// vm_credentials_keys.go — 2 caminhos alternativos de criação de perfil de credencial SSH, além
// de colar uma chave/senha crua já existente (SaveProfile em vm_credentials.go): gerar um par de
// chaves novo (RSA/Ed25519) direto no app, ou importar uma chave já existente no diretório
// ~/.ssh do HOST onde o servidor roda. Pedido explícito do usuário depois de relatar que o modal
// de SFTP não tinha nenhum perfil pra escolher — colar uma chave PEM manualmente era o único
// caminho disponível, pouco conveniente pra quem já tem uma chave pronta ou só quer gerar uma
// nova rapidamente.
//
// Nota de segurança sobre "importar de ~/.ssh": este é o diretório home do PRÓPRIO usuário que
// roda o servidor (app self-hosted, de uso pessoal/pequeno time — não multi-tenant), então expor
// essa listagem/importação não vaza segredo de terceiro nenhum, só dá acesso à própria chave que o
// usuário já tem no disco. Mesmo assim, o path de importação é sempre validado como estando
// LITERALMENTE dentro de ~/.ssh (nunca um caminho arbitrário do filesystem) antes de ler o
// conteúdo.

// defaultRSAKeyBits — tamanho padrão quando o usuário não escolhe explicitamente; 4096 é o
// recomendado atual (2048 ainda aceito, nunca menos — chave RSA menor que isso é considerada
// fraca pelos padrões de segurança correntes).
const defaultRSAKeyBits = 4096

// generateSSHKeyPair gera um par de chaves SSH novo — devolve a chave privada em PEM (RSA:
// PKCS#1 "RSA PRIVATE KEY", mesma convenção já usada pelo resto desta app pra chaves RSA — ver
// ExtractPFX em internal/certificates/pfx_extract.go; Ed25519 não tem equivalente PKCS#1, usa
// PKCS#8 genérico) e a chave pública já no formato "authorized_keys" (pronta pra colar no destino
// via `ssh-copy-id`/edição manual).
func generateSSHKeyPair(keyType string, bits int) (privPEM []byte, publicKeyAuthorized string, err error) {
	switch strings.ToLower(strings.TrimSpace(keyType)) {
	case "rsa":
		if bits <= 0 {
			bits = defaultRSAKeyBits
		}
		if bits < 2048 {
			return nil, "", fmt.Errorf("tamanho de chave RSA muito pequeno (mínimo 2048 bits, recomendado %d)", defaultRSAKeyBits)
		}
		key, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			return nil, "", fmt.Errorf("erro ao gerar chave RSA: %w", err)
		}
		pemBlock := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		sshPub, err := ssh.NewPublicKey(&key.PublicKey)
		if err != nil {
			return nil, "", fmt.Errorf("erro ao derivar chave pública: %w", err)
		}
		return pemBlock, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), nil

	case "ed25519":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, "", fmt.Errorf("erro ao gerar chave Ed25519: %w", err)
		}
		pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			return nil, "", fmt.Errorf("erro ao serializar chave Ed25519: %w", err)
		}
		pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
		sshPub, err := ssh.NewPublicKey(pub)
		if err != nil {
			return nil, "", fmt.Errorf("erro ao derivar chave pública: %w", err)
		}
		return pemBlock, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))), nil

	default:
		return nil, "", fmt.Errorf("keyType inválido: %q (use 'rsa' ou 'ed25519')", keyType)
	}
}

type generateVMSSHKeyRequest struct {
	Name     string `json:"name" binding:"required"`
	Username string `json:"username" binding:"required"`
	KeyType  string `json:"keyType" binding:"required"` // "rsa" | "ed25519"
	Bits     int    `json:"bits"`                       // só relevante pra rsa; 0 = usa o default
}

// GenerateKey — POST /api/v1/vms/credentials/generate-key. Gera o par, já salva o perfil (mesmo
// VMCredentialStore.SaveProfile usado pelo fluxo de colar chave) e devolve a chave PÚBLICA pro
// frontend mostrar — é a única vez que essa informação existe fora do banco criptografado, então o
// frontend precisa exibi-la de forma copiável logo em seguida (nunca mais recuperável depois,
// mesmo princípio de qualquer gerador de chave — a privada nunca é reexibida uma vez salva).
func (h *VMCredentialsHandler) GenerateKey(c *gin.Context) {
	if h.credentialStoreUnavailable(c) {
		return
	}
	var req generateVMSSHKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": fmt.Sprintf("requisição inválida: %v", err)},
		})
		return
	}

	privPEM, pubAuthorized, err := generateSSHKeyPair(req.KeyType, req.Bits)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "KEY_GENERATION_ERROR", "message": err.Error()},
		})
		return
	}

	userInfo := GetUserInfoForHistory(c)
	id, err := h.store.SaveProfile(userInfo.Email, storage.SaveSSHCredentialProfileInput{
		Name:          req.Name,
		Username:      req.Username,
		AuthMethod:    "key",
		PrivateKeyPEM: string(privPEM),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "SAVE_CREDENTIAL_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"id": id, "publicKey": pubAuthorized}})
}

// localSSHKeyEntry é a projeção JSON de um arquivo candidato a chave privada em ~/.ssh — nunca
// inclui o conteúdo, só nome/caminho (o conteúdo só é lido no momento da importação real).
type localSSHKeyEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// sshDirPath resolve ~/.ssh do usuário que roda o processo do servidor.
func sshDirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("erro ao resolver home dir: %w", err)
	}
	return filepath.Join(home, ".ssh"), nil
}

// looksLikePrivateKeyPEM — heurística simples e barata: um arquivo de chave privada sempre começa
// com um bloco PEM "-----BEGIN ... PRIVATE KEY-----" (RSA/Ed25519/EC/OPENSSH). Evita listar
// known_hosts/config/*.pub como se fossem candidatos.
func looksLikePrivateKeyPEM(data []byte) bool {
	s := strings.TrimSpace(string(data))
	return strings.HasPrefix(s, "-----BEGIN ") && strings.Contains(s, "PRIVATE KEY-----")
}

// listLocalSSHPrivateKeys varre ~/.ssh (não-recursivo — convenção do OpenSSH é chaves soltas
// nesse diretório, sem subpastas) e devolve só os arquivos cujo conteúdo bate com
// looksLikePrivateKeyPEM. Diretório ausente não é erro — devolve lista vazia (usuário nunca gerou
// nenhuma chave local ainda, situação normal).
func listLocalSSHPrivateKeys() ([]localSSHKeyEntry, error) {
	dir, err := sshDirPath()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []localSSHKeyEntry{}, nil
		}
		return nil, fmt.Errorf("erro ao ler %s: %w", dir, err)
	}

	result := []localSSHKeyEntry{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".pub") {
			continue
		}
		if name == "known_hosts" || name == "known_hosts.old" || name == "config" || name == "authorized_keys" {
			continue
		}
		fullPath := filepath.Join(dir, name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue // sem permissão de leitura, etc. — só pula, não aborta a listagem inteira
		}
		if !looksLikePrivateKeyPEM(data) {
			continue
		}
		result = append(result, localSSHKeyEntry{Name: name, Path: fullPath})
	}
	return result, nil
}

// ListLocalSSHKeys — GET /api/v1/vms/credentials/local-ssh-keys
func (h *VMCredentialsHandler) ListLocalSSHKeys(c *gin.Context) {
	entries, err := listLocalSSHPrivateKeys()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "LIST_LOCAL_KEYS_ERROR", "message": err.Error()},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": entries})
}

type importLocalVMSSHKeyRequest struct {
	Path       string `json:"path" binding:"required"`
	Name       string `json:"name" binding:"required"`
	Username   string `json:"username" binding:"required"`
	Passphrase string `json:"passphrase"`
}

// ImportLocalKey — POST /api/v1/vms/credentials/import-local-key. `path` precisa vir exatamente
// como devolvido por ListLocalSSHKeys (validado a seguir como estando dentro de ~/.ssh, nunca um
// caminho arbitrário do filesystem do servidor).
func (h *VMCredentialsHandler) ImportLocalKey(c *gin.Context) {
	if h.credentialStoreUnavailable(c) {
		return
	}
	var req importLocalVMSSHKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_REQUEST", "message": fmt.Sprintf("requisição inválida: %v", err)},
		})
		return
	}

	dir, err := sshDirPath()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "SSH_DIR_ERROR", "message": err.Error()},
		})
		return
	}
	resolved, err := filepath.Abs(req.Path)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "INVALID_PATH", "message": err.Error()},
		})
		return
	}
	if resolved != dir && !strings.HasPrefix(resolved, dir+string(os.PathSeparator)) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "PATH_OUTSIDE_SSH_DIR", "message": "o caminho precisa estar dentro de ~/.ssh"},
		})
		return
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   gin.H{"code": "READ_KEY_ERROR", "message": err.Error()},
		})
		return
	}

	// Valida que é uma chave privada parseável (com a passphrase informada, se houver) ANTES de
	// salvar — pega chave corrompida/passphrase errada na hora, não só quando o usuário tentar
	// conectar de verdade depois.
	if req.Passphrase != "" {
		if _, err := ssh.ParsePrivateKeyWithPassphrase(data, []byte(req.Passphrase)); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   gin.H{"code": "INVALID_PRIVATE_KEY", "message": "chave privada inválida ou passphrase incorreta: " + err.Error()},
			})
			return
		}
	} else {
		if _, err := ssh.ParsePrivateKey(data); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   gin.H{"code": "INVALID_PRIVATE_KEY", "message": "chave privada inválida (ou exige passphrase): " + err.Error()},
			})
			return
		}
	}

	userInfo := GetUserInfoForHistory(c)
	id, err := h.store.SaveProfile(userInfo.Email, storage.SaveSSHCredentialProfileInput{
		Name:          req.Name,
		Username:      req.Username,
		AuthMethod:    "key",
		PrivateKeyPEM: string(data),
		Passphrase:    req.Passphrase,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   gin.H{"code": "SAVE_CREDENTIAL_ERROR", "message": err.Error()},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"id": id}})
}
