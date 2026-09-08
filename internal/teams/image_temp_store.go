package teams

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// teamsBroadcastImagesDirName — pasta temporária onde as imagens de conteúdo (fotos/prints/GIFs,
// nunca emoji) extraídas de uma mensagem via "Carregar mensagem por link do Teams" são salvas,
// pra serem reaproveitadas na composição/reenvio dessa mesma mensagem (ver FetchMessageByLink em
// message_fetch.go). Destruída por completo a cada NOVA coleta (ResetTeamsBroadcastImagesDir) —
// pedido explícito do usuário: "copie a imagem para uma pasta temporária... e destruída na
// próxima coleta". Nunca acumula imagens de coletas anteriores.
const teamsBroadcastImagesDirName = "teams-broadcast-images"

// TeamsBroadcastImagesDir retorna o caminho absoluto da pasta temporária de imagens (não garante
// que ela exista — ver ResetTeamsBroadcastImagesDir pra isso).
func TeamsBroadcastImagesDir(homeDir string) string {
	return filepath.Join(homeDir, ".k8s-hpa-manager", teamsBroadcastImagesDirName)
}

// ResetTeamsBroadcastImagesDir apaga qualquer imagem de uma coleta anterior e recria a pasta
// vazia. Chamada no INÍCIO de toda nova extração via link — mesmo que a coleta atual não tenha
// nenhuma imagem de conteúdo, o estado da coleta anterior não deve sobreviver a uma nova.
func ResetTeamsBroadcastImagesDir(homeDir string) (string, error) {
	dir := TeamsBroadcastImagesDir(homeDir)
	if err := os.RemoveAll(dir); err != nil {
		return dir, fmt.Errorf("falha ao limpar pasta temporária de imagens: %w", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return dir, fmt.Errorf("falha ao criar pasta temporária de imagens: %w", err)
	}
	return dir, nil
}

// extensionForMimeType mapeia os tipos de imagem mais comuns servidos pelo Teams pra uma extensão
// de arquivo previsível — não usa mime.ExtensionsByType porque ela pode devolver várias opções em
// ordem não-determinística dependendo do SO/registro local, e aqui o nome do arquivo precisa ser
// estável (é reconstruído a partir do índice pela função que resolve os placeholders no envio).
func extensionForMimeType(mimeType string) string {
	base := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch base {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".bin"
	}
}

// teamsBroadcastImageFilenameRe é o único formato de nome que SaveTeamsBroadcastImage gera —
// usada também pelo handler HTTP que serve o arquivo e pelo resolvedor de placeholders no envio,
// como defesa contra path traversal (nunca aceitar um filename fora deste formato exato, mesmo
// vindo de um valor digitado/editado à mão pelo usuário no editor Markdown).
var teamsBroadcastImageFilenameRe = regexp.MustCompile(`^img-[0-9]+\.[a-z0-9]+$`)

// IsValidTeamsBroadcastImageFilename confirma que filename bate exatamente com o padrão gerado
// por SaveTeamsBroadcastImage.
func IsValidTeamsBroadcastImageFilename(filename string) bool {
	return teamsBroadcastImageFilenameRe.MatchString(filename)
}

// SaveTeamsBroadcastImage grava os bytes de uma imagem já baixada na pasta temporária, com um
// nome de arquivo previsível (img-<index>.<ext>) — o índice corresponde à ordem de aparição da
// imagem dentro da mensagem original, usado para casar de volta com o marcador @@TEAMS_IMG_i@@
// deixado no texto (ver message_fetch.go).
func SaveTeamsBroadcastImage(dir string, index int, mimeType string, data []byte) (filename string, err error) {
	filename = fmt.Sprintf("img-%d%s", index, extensionForMimeType(mimeType))
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", fmt.Errorf("falha ao salvar imagem %q: %w", filename, err)
	}
	return filename, nil
}
