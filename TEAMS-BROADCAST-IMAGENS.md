# Teams Broadcast — imagens: importar e reenviar

Problema: a imagem de uma mensagem do Teams aparecia no preview da aba Teams Broadcast, mas
chegava **sem a imagem** no chat de destino, embora o envio reportasse `sent: 1, failed: 0`.

Código envolvido: `internal/teams/message_fetch.go` (importação), `internal/teams/media_upload.go`
(upload), `internal/teams/sender.go` (envio), `internal/web/handlers/teams_broadcast.go`
(`resolveLocalImagePlaceholders`).

## Como o fluxo funciona

1. **Importar** (carregar a mensagem pelo link): o navegador do Teams lê o DOM, acha as tags `<img>`
   e salva os bytes em `~/.k8s-hpa-manager/teams-broadcast-images/img-N.<ext>`. No texto a imagem
   vira `![imagem](teams-temp:img-N.jpg)`.
2. **Enviar**: o backend troca a referência por `<img src="data:…;base64,…">`
   (`resolveLocalImagePlaceholders`) e o sender sobe cada imagem ao serviço de mídia do Teams
   (AMS) antes de mandar a mensagem (`uploadImagesInHTML`):
   - `POST https://us-prod.asyncgw.teams.microsoft.com/v1/objects/` cria o objeto e devolve o `id`;
   - `PUT …/v1/objects/{id}/content/imgpsh` envia os bytes;
   - a tag final é `<img itemid=… itemtype="http://schema.skype.com/AMSImage" src=…/views/imgpsh_fullsize>`.
3. Se o upload falha, o código mantém o `data:` como fallback. **O Teams descarta imagem em
   `data:` sem avisar**, então a mensagem chega sem imagem e o envio ainda conta como sucesso.

## Causa 1 (corrigida): upload recusado pelo AMS

Sintoma no log (`[Sender] Upload de imagem pro AMS falhou — mantendo data URI original`):

```
create object failed: HTTP 400 {"title":"The 'User-Agent' header is missing or invalid.",
"detail":"The platform id could not be created from the 'Mozilla/5.0 (Macintosh; …' value of 'User-Agent' header."}
```

O erro culpa o User-Agent, mas **o User-Agent não era o problema**: forçar um User-Agent de
Windows (testado) devolveu o mesmo 400 com a string nova. A captura do upload real do Teams Web
(`scripts/teams-spy-upload`) mostrou que o Teams usa exatamente o mesmo User-Agent do Chrome do
app. A diferença estava nos outros cabeçalhos e no corpo:

| | Teams Web (real) | Código antigo |
|---|---|---|
| `x-ms-client-version` | `1415/26100212017` | ausente |
| `x-ams-post-sharing-mode` | `Inline` | ausente |
| `x-ms-migration` / `x-ms-test-user` | `True` / `False` | ausentes |
| `sharingMode` no corpo | `Inline` | `Attached` |

O AMS monta o "platform id" a partir do User-Agent **junto com** `x-ms-client-version`; sem esse
cabeçalho a mensagem de erro cita o User-Agent.

**Solução** (`media_upload.go`): `POST /v1/objects/` e `PUT …/content/imgpsh` agora mandam
`authorization: Bearer <token IC3>`, `x-ms-client-version` (constante `teamsMediaClientVersion`),
`x-ms-migration` e `x-ms-test-user`; a criação manda também `x-ams-post-sharing-mode: Inline` e
`sharingMode: "Inline"`. O erro do upload passou a ser logado com até 800 caracteres (antes 200).
Validado ao vivo: a imagem chega ao chat de destino.

**Manutenção:** se o AMS voltar a responder 400 "platform id", o primeiro candidato é
`teamsMediaClientVersion` ter envelhecido. Recapture com `scripts/teams-spy-upload` e atualize o
valor.

## Causa 2 (limitação, não corrigida): cartões adaptativos

Mensagens postadas por bot/fluxo (ex.: anúncio com título, texto e botão) são **Adaptive Cards**
(`data-tid="adaptive-card"`, "This is a card message"). No DOM não há `<img>`, só
`image-placeholder-container`; por isso o log mostra `images_detected: 0, img_tags_seen: 0` e o
dump de diagnóstico (`~/.k8s-hpa-manager/teams-debug/message-image-debug-*.html`) não tem URL de
imagem. Além disso o envio do app é HTML (`RichText/Html`) e um cartão é outro tipo de mensagem
(anexo de cartão), então ele **não é reenviável** pelo Broadcast. A importação de imagem só vale
para mensagens comuns (imagem colada ou anexada por uma pessoa).

Possível evolução, não feita: ler o JSON do cartão pelo serviço de mensagens (traz a URL das
imagens e o link do botão) e reenviar só texto + link como mensagem comum.

## Como diagnosticar de novo

Log do servidor (`/tmp/k8s-hpa-manager-web-*.log`):

| Linha | Significa |
|---|---|
| `[MessageFetch] Mensagem carregada … images_detected: 0, img_tags_seen: 0` | Mensagem sem `<img>` (cartão, ou imagem ainda não renderizada) |
| `markdown_has_teams_temp_ref: false` | O frontend não mandou a referência da imagem |
| `html_has_img_tag: false` | O backend não montou a tag `<img>` |
| `Upload de imagem pro AMS falhou` | Erro HTTP do AMS, completo na mesma linha |
| `Imagem enviada pro AMS com sucesso` e a imagem não aparece | Upload ok; suspeitar do formato da tag `<img>` |

```bash
grep -E "MessageFetch\] Mensagem carregada|AMS|Upload de imagem" /tmp/k8s-hpa-manager-web-*.log | cut -c1-900 | tail
```

Captura do protocolo real (rodar no terminal do usuário; fecha o Chrome do app e precisa de
Ctrl+C no fim). A saída traz tokens: oculte `authorization`, `x-skypetoken`, `authentication` e
os `Cookie`/`Set-Cookie` antes de salvar ou compartilhar.

```bash
go run ./scripts/teams-spy-upload/
```

Depois da captura, reinicie o servidor (`./rebuild-web.sh -n`) no terminal do usuário.

## Observações da captura

- Antes de subir a imagem, o Teams Web autentica o AMS por cookie: `POST /v1/{userId}/aadtokenauth`
  e `POST /v1/skypetokenauth` (corpo `skypetoken=…`, `x-ms-client-type: web`). O nosso upload
  funciona só com o `Bearer` IC3; esses dois passos são o próximo suspeito se o AMS passar a exigir
  cookie.
- O `PUT` da captura mostrou 400 "Invalid content length": é efeito da ferramenta de espionagem,
  que reencaminha a requisição, e não vale como referência.
