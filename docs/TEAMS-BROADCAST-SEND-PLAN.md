# Checklist: Teams Broadcast — Envio em Lote

## Contexto

O envio de mensagens não pode ser feito abrindo/fechando o Teams por destinatário.
A estratégia é: **abrir o Teams uma única vez, capturar o token de autenticação e
executar `Promise.all()` com `fetch()` diretamente da página do Teams** (same-origin
→ contorna o bloqueio do MCAS).

---

## Fase 1 — Espionagem do protocolo de envio

- [ ] Criar `scripts/teams-spy-send/main.go`
  - Abre o Teams via go-rod com a sessão existente (`~/.k8s-hpa-manager/teams-session/`)
  - Ativa listener CDP de Network (`Network.requestWillBeSent`)
  - Usuário abre manualmente um chat e envia uma mensagem qualquer
  - Script imprime o primeiro request que bater em `messages` ou `sendMessage`
  - Capturar: URL exata, método HTTP, headers (especialmente `Authorization`, `ClientInfo`), corpo JSON
- [ ] Documentar o resultado aqui (URL, campos obrigatórios do body)

---

## Fase 2 — `internal/teams/sender.go` (novo arquivo)

```go
type SendResult struct {
    ThreadID string
    OK       bool
    Status   int
    Error    string
}

func SendBatch(sessionDir string, threadIDs []string, htmlContent string, logger *zerolog.Logger) ([]SendResult, error)
```

- [ ] Reusar `killExistingChrome` + `findSystemChrome` + launcher config de `scanner.go`
- [ ] Aguardar Teams carregar (mesma lógica de URL-check de `scanner.go`)
- [ ] Capturar token de auth:
  - Tentar `localStorage` / `sessionStorage` pelas chaves conhecidas (`skypetoken`, `ts.xxx.auth.skypetoken`)
  - Fallback: captura via CDP Network listener (igual à Fase 1)
- [ ] Montar JS com `Promise.all()`:
  ```js
  const results = await Promise.all(threadIDs.map(async (id) => {
      try {
          const r = await fetch(url.replace('{id}', id), { method, headers, body });
          return { id, ok: r.ok, status: r.status };
      } catch(e) {
          return { id, ok: false, status: 0, error: e.message };
      }
  }));
  ```
- [ ] Batching: dividir em grupos de 10, `sleep(500ms)` entre grupos
- [ ] Retornar `[]SendResult`

---

## Fase 3 — Backend: atualizar `Send()` em `teams_broadcast.go`

- [ ] Substituir `501 Not Implemented` por chamada a `teams.SendBatch()`
- [ ] Validar `req.Markdown` → converter para HTML simples se necessário (Teams aceita `<p>`, `<b>`, `<br>`)
  - Biblioteca: `github.com/yuin/goldmark` (já no vendor?) ou regex simples
- [ ] Retornar `[]SendResult` serializado:
  ```json
  {
    "sent": 8,
    "failed": 2,
    "results": [
      { "thread_id": "19:...", "ok": true, "status": 200 },
      { "thread_id": "19:...", "ok": false, "status": 0, "error": "fetch failed" }
    ]
  }
  ```

---

## Fase 4 — Frontend: `TeamsBroadcastTab.tsx`

- [ ] Após clicar "Enviar", mostrar progress bar (total enviados / total selecionados)
- [ ] Exibir resultado por destinatário: badge verde "OK" ou vermelho "Falhou" com motivo
- [ ] Desabilitar botão "Enviar" enquanto envio estiver em andamento
- [ ] (Opcional) Streaming via SSE: `POST /api/v1/teams/broadcast/send` publica eventos por destinatário
  - Cada evento: `{ thread_id, ok, status, name }`
  - Frontend atualiza a lista em tempo real

---

## Fase 5 — SSE (opcional, após Fases 1-4 funcionando)

- [ ] Adicionar rota SSE `GET /api/v1/teams/broadcast/send/stream/:sessionId`
- [ ] `Send()` publica evento por destinatário via `sse.Broker`
- [ ] Frontend consome stream e atualiza badge por linha

---

## Notas

- **Token captura**: o campo exato varia por tenant. Testar com a Fase 1 antes de assumir o nome da chave.
- **Markdown → HTML**: Teams renderiza `<p>texto</p>`, `<b>`, `<i>`, `<br>`, listas `<ul><li>`. Não usar `**bold**` — mandar HTML.
- **Paralelismo**: 10 simultâneos é conservador. Se Teams não rate-limitar, pode subir para 20.
- **Sessão separada**: usar SEMPRE `~/.k8s-hpa-manager/teams-session/` — nunca misturar com `rod-session/` do ServiceNow.
- **Self-chat**: thread ID com prefixo `28:` — incluído no regex `(19|28|48):`.
