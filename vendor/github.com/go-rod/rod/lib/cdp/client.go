// Package cdp for application layer communication with browser.
package cdp

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/go-rod/rod/lib/defaults"
	"github.com/go-rod/rod/lib/utils"
)

// Request to send to browser.
type Request struct {
	ID        int         `json:"id"`
	SessionID string      `json:"sessionId,omitempty"`
	Method    string      `json:"method"`
	Params    interface{} `json:"params,omitempty"`
}

// Response from browser.
type Response struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Event from browser.
type Event struct {
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
}

// WebSocketable enables you to choose the websocket lib you want to use.
// Such as you can easily wrap gorilla/websocket and use it as the transport layer.
type WebSocketable interface {
	// Send text message only
	Send(data []byte) error
	// Read returns text message only
	Read() ([]byte, error)
}

// Client is a devtools protocol connection instance.
type Client struct {
	count uint64

	ws WebSocketable

	pending sync.Map    // pending requests
	event   chan *Event // events from browser

	logger utils.Logger
}

// New creates a cdp connection, all messages from Client.Event must be received or they will block the client.
func New() *Client {
	return &Client{
		event:  make(chan *Event),
		logger: defaults.CDP,
	}
}

// Logger sets the logger to log all the requests, responses, and events transferred between Rod and the browser.
// The default format for each type is in file format.go.
func (cdp *Client) Logger(l utils.Logger) *Client {
	cdp.logger = l
	return cdp
}

// Start to browser.
func (cdp *Client) Start(ws WebSocketable) *Client {
	cdp.ws = ws

	go cdp.consumeMessages()

	return cdp
}

type result struct {
	msg json.RawMessage
	err error
}

// Call a method and wait for its response.
func (cdp *Client) Call(ctx context.Context, sessionID, method string, params interface{}) ([]byte, error) {
	req := &Request{
		ID:        int(atomic.AddUint64(&cdp.count, 1)),
		SessionID: sessionID,
		Method:    method,
		Params:    params,
	}

	cdp.logger.Println(req)

	data, err := json.Marshal(req)
	utils.E(err)

	done := make(chan result)
	once := sync.Once{}
	cdp.pending.Store(req.ID, func(res result) {
		once.Do(func() {
			select {
			case <-ctx.Done():
			case done <- res:
			}
		})
	})
	defer cdp.pending.Delete(req.ID)

	err = cdp.ws.Send(data)
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-done:
		return res.msg, res.err
	}
}

// Event returns a channel that will emit browser devtools protocol events. Must be consumed or will block producer.
func (cdp *Client) Event() <-chan *Event {
	return cdp.event
}

// PATCH MANUAL (k8s-hpa-manager, ver CLAUDE.md "Notas de Build" > patch vendor go-rod): os 3
// utils.E(err) originais deste loop PANICAVAM o processo inteiro sempre que um frame do
// websocket vinha com JSON malformado/truncado (ex: "unexpected end of JSON input") — como esta
// goroutine é lançada pelo próprio go-rod (cdp.Start, não pelo código da app), um panic aqui não
// tem nenhum recover() no meio do caminho e derruba o servidor Go inteiro, não só a extração em
// andamento. Reproduzido ao vivo usando o modo Docker (Chrome dentro de container
// selenium/standalone-chrome, CDP retransmitido pelo proxy WebSocket do Selenium Grid — ver
// internal/teams/docker_browser.go) — a camada extra de proxy expôs esse caso-limite que conexão
// direta a um Chrome local (embed/sistema) aparentemente nunca disparou. Trocado por log +
// `continue` (ignora a mensagem malformada e segue lendo a próxima) em vez de panic — mesmo
// tratamento que uma mensagem sem sessionId/id reconhecido já recebe logo abaixo (`if !ok {
// continue }`). Efeito colateral aceitável: uma resposta CDP pontual perdida vira timeout do
// `Call()` que a esperava (erro tratável) em vez de crashar tudo.
func (cdp *Client) consumeMessages() {
	defer close(cdp.event)

	for {
		data, err := cdp.ws.Read()
		if err != nil {
			cdp.pending.Range(func(_, val interface{}) bool {
				val.(func(result))(result{err: err}) //nolint: forcetypeassert
				return true
			})
			return
		}

		var id struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(data, &id); err != nil {
			cdp.logger.Println("cdp: mensagem malformada ignorada (id):", err)
			continue
		}

		if id.ID == 0 {
			var evt Event
			if err := json.Unmarshal(data, &evt); err != nil {
				cdp.logger.Println("cdp: mensagem malformada ignorada (event):", err)
				continue
			}
			cdp.logger.Println(&evt)
			cdp.event <- &evt
			continue
		}

		var res Response
		if err := json.Unmarshal(data, &res); err != nil {
			cdp.logger.Println("cdp: mensagem malformada ignorada (response):", err)
			continue
		}

		cdp.logger.Println(&res)

		val, ok := cdp.pending.Load(id.ID)
		if !ok {
			continue
		}
		if res.Error == nil {
			val.(func(result))(result{res.Result, nil}) //nolint: forcetypeassert
		} else {
			val.(func(result))(result{nil, res.Error}) //nolint: forcetypeassert
		}
	}
}
