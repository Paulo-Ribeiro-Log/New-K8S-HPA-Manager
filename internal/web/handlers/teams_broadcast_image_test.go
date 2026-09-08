package handlers

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s-hpa-manager/internal/teams"
)

// TestApplyInlineMarkdown_Image confirma o suporte real de imagem no conversor de envio — antes
// desta mudança "![alt](url)" era interpretado por reLink como um LINK comum (a subsequência
// "[alt](url)" batia sozinha, sobrando um "!" solto na frente), nunca virava uma imagem de
// verdade no HTML enviado ao Teams.
func TestApplyInlineMarkdown_Image(t *testing.T) {
	got := applyInlineMarkdown("![uma foto](https://example.com/foto.png)")
	want := `<img src="https://example.com/foto.png" alt="uma foto">`
	if got != want {
		t.Errorf("applyInlineMarkdown(imagem) = %q, want %q", got, want)
	}
}

// TestApplyInlineMarkdown_ImageDoesNotLeakIntoLink confirma que reImage roda antes de reLink —
// sem essa ordem, "![alt](url)" viraria "!<a href=\"url\">alt</a>" (um "!" solto antes de um link
// comum), nunca uma tag <img>.
func TestApplyInlineMarkdown_ImageDoesNotLeakIntoLink(t *testing.T) {
	got := applyInlineMarkdown("veja: ![captura](http://x/y.png) e [este link](http://x/z)")
	if strings.Contains(got, "!<a") {
		t.Fatalf("imagem vazou pro caminho de link comum: %q", got)
	}
	if !strings.Contains(got, `<img src="http://x/y.png" alt="captura">`) {
		t.Errorf("imagem não convertida corretamente: %q", got)
	}
	if !strings.Contains(got, `<a href="http://x/z">este link</a>`) {
		t.Errorf("link comum não convertido corretamente: %q", got)
	}
}

// TestResolveLocalImagePlaceholders_EmbedsRealBytesAsDataURI cobre o caminho principal: uma
// imagem de conteúdo baixada ao carregar uma mensagem via link (ver
// internal/teams/message_fetch.go) precisa virar um data URI de verdade na hora do envio, já que
// esta aplicação não tem nenhum endpoint de upload de mídia pro Teams.
func TestResolveLocalImagePlaceholders_EmbedsRealBytesAsDataURI(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	dir, err := teams.ResetTeamsBroadcastImagesDir(homeDir)
	if err != nil {
		t.Fatalf("ResetTeamsBroadcastImagesDir falhou: %v", err)
	}
	filename, err := teams.SaveTeamsBroadcastImage(dir, 0, "image/png", []byte("fake-png-bytes"))
	if err != nil {
		t.Fatalf("SaveTeamsBroadcastImage falhou: %v", err)
	}

	markdown := "Confira: ![imagem](teams-temp:" + filename + ") obrigado"
	got := resolveLocalImagePlaceholders(markdown, nil)

	wantB64 := base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))
	if !strings.Contains(got, "data:image/png;base64,"+wantB64) {
		t.Errorf("resultado não contém o data URI esperado: %s", got)
	}
	if strings.Contains(got, "teams-temp:") {
		t.Errorf("placeholder teams-temp: não deveria sobreviver à resolução: %s", got)
	}
}

// TestResolveLocalImagePlaceholders_MissingFileBecomesWarning cobre o caso best-effort: a pasta
// temporária já foi destruída por uma coleta mais recente (ResetTeamsBroadcastImagesDir) antes do
// usuário enviar — a referência devia virar um aviso textual, nunca quebrar o envio inteiro.
func TestResolveLocalImagePlaceholders_MissingFileBecomesWarning(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if _, err := teams.ResetTeamsBroadcastImagesDir(homeDir); err != nil {
		t.Fatalf("ResetTeamsBroadcastImagesDir falhou: %v", err)
	}

	got := resolveLocalImagePlaceholders("![imagem](teams-temp:img-0.png)", nil)
	if strings.Contains(got, "data:") {
		t.Errorf("não deveria produzir data URI pra arquivo inexistente: %s", got)
	}
	if !strings.Contains(got, "imagem indisponível") {
		t.Errorf("esperava aviso de imagem indisponível, got: %s", got)
	}
}

// TestResolveLocalImagePlaceholders_RejectsFilenameOutsideGeneratedFormat garante que um filename
// fora do formato exato gerado por SaveTeamsBroadcastImage (ex: editado à mão no editor Markdown)
// nunca é lido, mesmo existindo de fato dentro da pasta temporária — defesa em profundidade
// compartilhada com o handler HTTP (ServeBroadcastImage), via IsValidTeamsBroadcastImageFilename.
// Path traversal de verdade (".."/"/") já é estruturalmente impossível antes disso: reTeamsTempImage
// só captura "[a-zA-Z0-9_.-]+", que nunca inclui "/".
func TestResolveLocalImagePlaceholders_RejectsFilenameOutsideGeneratedFormat(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	dir, err := teams.ResetTeamsBroadcastImagesDir(homeDir)
	if err != nil {
		t.Fatalf("ResetTeamsBroadcastImagesDir falhou: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-generated.png"), []byte("dado-sensivel"), 0600); err != nil {
		t.Fatalf("falha ao preparar arquivo de teste: %v", err)
	}

	got := resolveLocalImagePlaceholders("![imagem](teams-temp:not-generated.png)", nil)
	if strings.Contains(got, "dado-sensivel") {
		t.Fatalf("arquivo fora do formato gerado por SaveTeamsBroadcastImage não deveria ter sido lido: %s", got)
	}
	if !strings.Contains(got, "imagem indisponível") {
		t.Errorf("esperava aviso de imagem indisponível, got: %s", got)
	}
}
