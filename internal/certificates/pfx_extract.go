package certificates

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// ExtractPFX decodifica um arquivo .pfx/.p12 (PKCS#12) e devolve tlsCrt (leaf + chain já
// concatenados em PEM, ordem leaf-primeiro — mesma convenção que um Secret kubernetes.io/tls
// espera) e tlsKey (chave privada em PEM, sempre SEM senha), além do leaf já parseado (pra
// popular metadata sem precisar reparsear tlsCrt em seguida).
//
// Usa software.sslmate.com/src/go-pkcs12 (fork ativamente mantido) em vez da alternativa já
// indiretamente vendorizada golang.org/x/crypto/pkcs12 — essa última é legada e só suporta
// RC2/3DES, falhando em .pfx modernos com criptografia AES (mesma classe de problema já vista
// nesta investigação com chaves "ENCRYPTED PRIVATE KEY": formato mais novo, ferramenta antiga
// não entende). Ver PFX-CERT-EXTRACTION-PLAN.md.
func ExtractPFX(pfxBytes []byte, password string) (tlsCrt, tlsKey []byte, leaf *x509.Certificate, err error) {
	privateKey, cert, caCerts, err := pkcs12.DecodeChain(pfxBytes, password)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("erro ao decodificar .pfx: %w", err)
	}
	if cert == nil {
		return nil, nil, nil, fmt.Errorf(".pfx não contém certificado")
	}

	// BUG REAL CORRIGIDO — pkcs12.DecodeChain (vendor) decide qual certificado é o "leaf" só pela
	// ORDEM dos bags no arquivo (o primeiro certBag encontrado vira `cert`, o resto vira
	// `caCerts`), sem nenhuma validação de que esse certificado de fato corresponde à chave
	// privada. Reproduzido isoladamente: um .pfx cujos bags não vêm na ordem leaf-primeiro (comum
	// em exports de outras ferramentas — Windows/IIS, keytool, algumas CAs) faz `cert` virar uma
	// intermediária/raiz, e o leaf real acaba dentro de `caCerts` — visualmente o tls.crt resultante
	// mostra primeiro um certificado de CA, não o do domínio, mesmo com todos os certificados
	// fisicamente presentes ("a chain não foi extraída junto" na prática, mesmo a chain estando lá).
	// Corrigido comparando a chave pública de cada certificado candidato (cert + caCerts) contra a
	// chave privada decodificada — o que bate de verdade é promovido a leaf, independente da ordem
	// dos bags no arquivo original.
	candidates := append([]*x509.Certificate{cert}, caCerts...)
	leafIdx := 0
	for i, c := range candidates {
		if certMatchesPrivateKey(c, privateKey) {
			leafIdx = i
			break
		}
	}
	cert = candidates[leafIdx]
	caCerts = make([]*x509.Certificate, 0, len(candidates)-1)
	for i, c := range candidates {
		if i != leafIdx {
			caCerts = append(caCerts, c)
		}
	}

	// tlsCrt: leaf primeiro, seguido das intermediárias/raiz (se houver) — a chain inteira num
	// único tls.crt, exatamente o formato que um Secret kubernetes.io/tls espera (ver decisão em
	// PFX-CERT-EXTRACTION-PLAN.md: "no uso real desta empresa a chain já fica dentro do tls.crt").
	var crtBuf bytes.Buffer
	if err := pem.Encode(&crtBuf, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}); err != nil {
		return nil, nil, nil, fmt.Errorf("erro ao codificar leaf em PEM: %w", err)
	}
	for _, ca := range caCerts {
		if err := pem.Encode(&crtBuf, &pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}); err != nil {
			return nil, nil, nil, fmt.Errorf("erro ao codificar certificado da chain em PEM: %w", err)
		}
	}

	// tlsKey: PKCS#8 SEM senha (-----BEGIN PRIVATE KEY-----) — nunca criptografado. Resolve de
	// saída o mesmo problema já visto com chaves "ENCRYPTED PRIVATE KEY": um Secret K8s não tem
	// como fornecer senha no momento do handshake TLS, então a chave extraída aqui já sai pronta
	// pra uso, sem precisar de um passo de descriptografia manual depois.
	keyBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("erro ao serializar chave privada: %w", err)
	}
	var keyBuf bytes.Buffer
	if err := pem.Encode(&keyBuf, &pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return nil, nil, nil, fmt.Errorf("erro ao codificar chave privada em PEM: %w", err)
	}

	return crtBuf.Bytes(), keyBuf.Bytes(), cert, nil
}

// certMatchesPrivateKey confere se a chave pública do certificado corresponde à chave privada
// decodificada — usado por ExtractPFX pra achar o leaf de verdade, independente da ordem dos bags
// no .pfx original. Cobre os 3 tipos de chave que pkcs12.DecodeChain pode devolver (RSA/ECDSA/
// Ed25519, via x509.ParsePKCS8PrivateKey); qualquer outra combinação retorna false (nunca panica).
func certMatchesPrivateKey(cert *x509.Certificate, privateKey interface{}) bool {
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		priv, ok := privateKey.(*rsa.PrivateKey)
		if !ok {
			return false
		}
		return pub.N.Cmp(priv.N) == 0 && pub.E == priv.E
	case *ecdsa.PublicKey:
		priv, ok := privateKey.(*ecdsa.PrivateKey)
		if !ok {
			return false
		}
		return pub.Curve == priv.Curve && pub.X.Cmp(priv.X) == 0 && pub.Y.Cmp(priv.Y) == 0
	case ed25519.PublicKey:
		priv, ok := privateKey.(ed25519.PrivateKey)
		if !ok {
			return false
		}
		return bytes.Equal(pub, priv.Public().(ed25519.PublicKey))
	default:
		return false
	}
}
