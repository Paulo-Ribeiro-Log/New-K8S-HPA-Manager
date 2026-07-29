package certificates

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// ChainValidationResult é o resultado de ValidateCertificateChain — usado tanto na validação
// ad-hoc (antes de instalar) quanto na validação automática pós-instalação/rollback.
type ChainValidationResult struct {
	Valid             bool     `json:"valid"`
	KeyMatchesCert    bool     `json:"key_matches_cert"`
	ChainOrderCorrect bool     `json:"chain_order_correct"`
	TrustedByPublicCA bool     `json:"trusted_by_public_ca"` // false é comum e OK pra CA interna/privada
	ExpiryOK          bool     `json:"expiry_ok"`
	Errors            []string `json:"errors,omitempty"`   // bloqueantes
	Warnings          []string `json:"warnings,omitempty"` // não-bloqueantes
	ChainSubjects     []string `json:"chain_subjects"`     // leaf → intermediário(s) → (raiz, se presente)
	OpenSSLNotes      []string `json:"openssl_notes,omitempty"`
}

// ValidateCertificateChain valida um par cert+key além do que ValidatePEM já cobre (existência de
// blocos PEM válidos) — verifica se a chave realmente bate com o certificado, se a cadeia está na
// ordem correta, e se ela é confiável/ainda válida no tempo. Mecanismo primário é Go nativo
// (crypto/x509, crypto/tls) — a imagem de produção (dockerfile, FROM scratch) não garante o
// binário openssl no PATH, então nenhuma checagem aqui pode depender dele. Um enriquecimento via
// openssl (enrichWithOpenSSL) é tentado só se o binário existir, e nunca é bloqueante.
func ValidateCertificateChain(certPEM, keyPEM []byte) (*ChainValidationResult, error) {
	certs, err := parsePEMChain(certPEM)
	if err != nil {
		return nil, fmt.Errorf("certificado PEM inválido: %w", err)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("nenhum certificado encontrado no PEM")
	}

	result := &ChainValidationResult{
		ChainSubjects: make([]string, 0, len(certs)),
	}
	for _, cert := range certs {
		result.ChainSubjects = append(result.ChainSubjects, cert.Subject.CommonName)
	}

	// Passo 1 — a chave bate com o certificado? tls.X509KeyPair já faz essa checagem
	// internamente (é a mesma validação que um servidor TLS real faria ao carregar o par), sem
	// precisar comparar manualmente RSA/ECDSA/Ed25519 campo a campo.
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		result.KeyMatchesCert = false
		result.Errors = append(result.Errors, fmt.Sprintf("chave não corresponde ao certificado: %s", err.Error()))
	} else {
		result.KeyMatchesCert = true
	}

	// Passo 2 — ordem da cadeia: cada cert deve ser assinado pelo próximo da lista (leaf ->
	// intermediário -> ... ). Detecta cadeia fora de ordem ou com elo faltando.
	result.ChainOrderCorrect = true
	for i := 0; i < len(certs)-1; i++ {
		if err := certs[i].CheckSignatureFrom(certs[i+1]); err != nil {
			result.ChainOrderCorrect = false
			result.Errors = append(result.Errors, fmt.Sprintf(
				"cadeia fora de ordem ou incompleta: %q não foi assinado por %q (%s)",
				certs[i].Subject.CommonName, certs[i+1].Subject.CommonName, err.Error(),
			))
			break
		}
	}

	// Passo 3 — expiração (leaf)
	leaf := certs[0]
	now := time.Now()
	result.ExpiryOK = now.After(leaf.NotBefore) && now.Before(leaf.NotAfter)
	if !result.ExpiryOK {
		if now.Before(leaf.NotBefore) {
			result.Errors = append(result.Errors, fmt.Sprintf("certificado ainda não é válido (NotBefore: %s)", leaf.NotBefore.Format(time.RFC3339)))
		} else {
			result.Errors = append(result.Errors, fmt.Sprintf("certificado expirado (NotAfter: %s)", leaf.NotAfter.Format(time.RFC3339)))
		}
	}

	// Passo 4 — confiança: verifica contra o root store do SISTEMA (não passamos Roots
	// explícito). UnknownAuthorityError é o caso comum e ESPERADO pra CA interna/privada — vira
	// warning, não erro. Qualquer outro erro do Verify (ex: uso de chave incompatível) é
	// bloqueante.
	pool := x509.NewCertPool()
	for _, c := range certs[1:] {
		pool.AddCert(c)
	}
	_, verifyErr := leaf.Verify(x509.VerifyOptions{
		Intermediates: pool,
		CurrentTime:   now,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	switch {
	case verifyErr == nil:
		result.TrustedByPublicCA = true
	case isUnknownAuthorityError(verifyErr):
		result.TrustedByPublicCA = false
		result.Warnings = append(result.Warnings, "cadeia não leva a uma CA pública confiável pelo sistema — normal para CA interna/privada; se esperava uma CA pública, confira se algum certificado intermediário está faltando")
	default:
		result.TrustedByPublicCA = false
		result.Errors = append(result.Errors, fmt.Sprintf("falha ao verificar a cadeia: %s", verifyErr.Error()))
	}

	// Enriquecimento opcional via openssl — nunca bloqueante, nunca obrigatório.
	result.OpenSSLNotes = enrichWithOpenSSL(certPEM)

	result.Valid = len(result.Errors) == 0
	return result, nil
}

func isUnknownAuthorityError(err error) bool {
	_, ok := err.(x509.UnknownAuthorityError)
	return ok
}

// enrichWithOpenSSL roda uma checagem extra via openssl SOMENTE se o binário estiver disponível
// no PATH do processo — a imagem de produção (FROM scratch) não garante isso, então esta função
// nunca deve ser tratada como fonte de verdade, só como diagnóstico complementar. Qualquer falha
// (binário ausente, comando retorna erro) é engolida silenciosamente.
func enrichWithOpenSSL(certPEM []byte) []string {
	if _, err := exec.LookPath("openssl"); err != nil {
		return nil
	}

	tmpFile, err := os.CreateTemp("", "cert-validate-*.pem")
	if err != nil {
		return nil
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write(certPEM); err != nil {
		tmpFile.Close()
		return nil
	}
	tmpFile.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var notes []string

	// -checkend 0: retorna erro (exit != 0) se já estiver expirado no momento da checagem —
	// diagnóstico complementar ao Passo 3 (que já usa Go nativo), não substitui.
	if out, err := exec.CommandContext(ctx, "openssl", "x509", "-noout", "-checkend", "0", "-in", tmpFile.Name()).CombinedOutput(); err != nil {
		notes = append(notes, fmt.Sprintf("openssl: certificado expirado ou inválido (%s)", string(out)))
	} else {
		notes = append(notes, "openssl: certificado válido no momento da checagem")
	}

	return notes
}
