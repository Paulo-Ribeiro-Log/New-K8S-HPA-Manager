package teams

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
)

func TestImageSizeAttrs(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 800, 51))); err != nil {
		t.Fatal(err)
	}
	got := imageSizeAttrs(base64.StdEncoding.EncodeToString(buf.Bytes()))
	want := ` width="800" height="51" style="vertical-align:bottom;width:800px;height:51px"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := imageSizeAttrs("não-é-imagem"); got != "" {
		t.Errorf("formato inválido deveria voltar vazio, veio %q", got)
	}
}
