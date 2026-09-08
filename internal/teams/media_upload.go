package teams

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/go-rod/rod"
	"github.com/rs/zerolog"
)

// teamsMediaHost — host do gateway de mídia AMS (Async Messaging Service) do Teams, CONFIRMADO
// AO VIVO via captura real de tráfego (scripts/teams-spy-upload) contra esta conta/tenant durante
// um upload de imagem manual real no Teams Web. Pode variar por região/tenant — não há forma
// pública documentada de descobrir esse host dinamicamente; se um upload falhar com erro de
// rede/404 em outra conta, provavelmente é esse host que precisa mudar.
const teamsMediaHost = "us-prod.asyncgw.teams.microsoft.com"

// reDataURIImage casa uma tag "<img ... src="data:<mime>;base64,<dados>" ...>" já montada por
// resolveLocalImagePlaceholders (internal/web/handlers/teams_broadcast.go) — usada por
// uploadImagesInHTML pra encontrar cada imagem embutida e trocá-la por uma referência real ao
// serviço de mídia do Teams antes do envio.
var reDataURIImage = regexp.MustCompile(`<img src="data:([^;"]+);base64,([^"]+)" alt="([^"]*)">`)

// uploadImagesInHTML substitui cada "<img src="data:...">" no HTML pela referência real de uma
// imagem já hospedada no serviço de mídia AMS do Teams — CONFIRMADO ao vivo que o Teams REJEITA/
// remove silenciosamente uma imagem embutida como data URI (relatado e confirmado pelo usuário:
// a mensagem chega sem a imagem, mesmo com o HTTP 200 de sucesso no envio de texto). O protocolo
// real (2 chamadas, mesmo Bearer token IC3 já usado pra enviar texto) foi descoberto capturando o
// tráfego de um upload de imagem manual real no Teams Web (scripts/teams-spy-upload):
//
//  1. POST https://{host}/v1/objects/ — cria um objeto vazio, com "permissions" listando os MRIs
//     (thread IDs) de quem pode LER a imagem depois — sem isso o destinatário não teria acesso.
//     Retorna {"id": "<objectId>"}.
//  2. PUT https://{host}/v1/objects/{objectId}/content/imgpsh — envia os bytes crus da imagem.
//
// `permissions` deve conter TODOS os threadIDs de destino desta mensagem (upload único
// compartilhado entre os destinatários do broadcast, não um upload por destinatário).
//
// Best-effort por imagem: se o upload falhar, a tag "<img src="data:...">" original permanece
// intacta no HTML (mesmo comportamento de antes desta função existir) — nunca pior que o estado
// anterior, só potencialmente melhor.
//
// ATENÇÃO: o formato exato da tag "<img>" final ("itemid"+"itemscope"+itemtype
// "http://schema.skype.com/AMSImage", vista no HTML de uma mensagem RECEBIDA real, ver
// internal/teams/message_fetch.go) é uma reconstrução baseada nesse HTML observado — a
// requisição de ENVIO de uma mensagem com imagem (o "wire format" exato esperado pelo campo
// `content`) NÃO foi capturada ainda; validar contra um envio real antes de confiar cegamente.
func uploadImagesInHTML(page *rod.Page, html string, threadIDs []string, logger *zerolog.Logger) string {
	if !reDataURIImage.MatchString(html) {
		return html
	}
	return reDataURIImage.ReplaceAllStringFunc(html, func(match string) string {
		m := reDataURIImage.FindStringSubmatch(match)
		mimeType, base64Data, alt := m[1], m[2], m[3]
		objectID, uploadErr := uploadImageToAMS(page, threadIDs, alt, mimeType, base64Data, logger)
		if uploadErr != nil {
			logger.Warn().Err(uploadErr).Msg("[Sender] Upload de imagem pro AMS falhou — mantendo data URI original como fallback")
			return match
		}
		imageURL := fmt.Sprintf("https://%s/v1/objects/%s/views/imgpsh_fullsize", teamsMediaHost, objectID)
		logger.Info().Str("object_id", objectID).Str("url", imageURL).Msg("[Sender] Imagem enviada pro AMS com sucesso — referenciando na mensagem")
		return fmt.Sprintf(`<img itemid="%s" itemscope itemtype="http://schema.skype.com/AMSImage" src="%s" alt="%s">`, objectID, imageURL, alt)
	})
}

// uploadImageToAMS executa as 2 chamadas do protocolo de upload (ver comentário de
// uploadImagesInHTML) via CDP/JS dentro da página já autenticada do Teams — reaproveita
// teamsAuthExtractJS (mesma extração de Bearer token IC3 já usada por SendBatch/DeleteMessages),
// nenhuma autenticação nova necessária.
func uploadImageToAMS(page *rod.Page, threadIDs []string, filename, mimeType, base64Data string, logger *zerolog.Logger) (string, error) {
	if filename == "" {
		filename = "image"
	}
	threadIDsJSON, _ := json.Marshal(threadIDs)
	base64JSON, _ := json.Marshal(base64Data)
	filenameJSON, _ := json.Marshal(filename)

	js := "async () => {\n" + teamsAuthExtractJS + fmt.Sprintf(`
		const threadIDs = %s;
		const permissions = {};
		for (const id of threadIDs) permissions[id] = ['read'];

		try {
			const createResp = await fetch('https://%s/v1/objects/', {
				method: 'POST',
				headers: { authorization: 'Bearer ' + bearerToken, 'content-type': 'application/json' },
				body: JSON.stringify({ filename: %s, permissions, sharingMode: 'Attached', type: 'pish/image' }),
			});
			if (!createResp.ok) {
				return JSON.stringify({ error: 'create object failed: HTTP ' + createResp.status + ' ' + (await createResp.text()).slice(0, 200) });
			}
			const createBody = await createResp.json();
			const objectId = createBody.id;
			if (!objectId) return JSON.stringify({ error: 'create object: resposta sem id' });

			// Decodifica o base64 (já recebido do Go) pros bytes crus a enviar no PUT.
			const binary = atob(%s);
			const bytes = new Uint8Array(binary.length);
			for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);

			const putResp = await fetch('https://%s/v1/objects/' + objectId + '/content/imgpsh', {
				method: 'PUT',
				headers: { authorization: 'Bearer ' + bearerToken, 'content-type': 'application/octet-stream' },
				body: bytes,
			});
			if (!putResp.ok) {
				return JSON.stringify({ error: 'upload bytes failed: HTTP ' + putResp.status });
			}
			return JSON.stringify({ objectId });
		} catch (e) {
			return JSON.stringify({ error: String(e) });
		}
	}`, string(threadIDsJSON), teamsMediaHost, string(filenameJSON), string(base64JSON), teamsMediaHost)

	res, evalErr := page.Eval(js)
	if evalErr != nil {
		return "", fmt.Errorf("falha ao executar JS de upload: %w", evalErr)
	}

	var out struct {
		ObjectID string `json:"objectId"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal([]byte(res.Value.String()), &out); err != nil {
		return "", fmt.Errorf("falha ao parsear resultado do upload: %w (raw: %.200s)", err, res.Value.String())
	}
	if out.Error != "" {
		return "", fmt.Errorf("%s", out.Error)
	}
	if out.ObjectID == "" {
		return "", fmt.Errorf("upload não retornou objectId")
	}
	logger.Debug().Str("mime", mimeType).Str("filename", filename).Str("object_id", out.ObjectID).
		Msg("[Sender] uploadImageToAMS: upload concluído")
	return out.ObjectID, nil
}
