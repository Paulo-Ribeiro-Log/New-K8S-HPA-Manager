package handlers

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// http_client_curl.go — parser de comandos cURL colados na ferramenta Cliente HTTP (formato do
// "Copy as cURL" do Postman/Chrome e comandos escritos à mão). Cobre o subconjunto usado para
// testar APIs: método, URL, headers, corpo, redirects, TLS e auth básica. Flags não suportadas
// não quebram o parse — viram avisos na resposta.

// HTTPClientHeader é um par chave/valor de header (lista, não map: preserva ordem e repetição).
type HTTPClientHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ParsedCurl é o resultado do parse — mesmos campos do formulário da ferramenta.
type ParsedCurl struct {
	Method             string             `json:"method"`
	URL                string             `json:"url"`
	Headers            []HTTPClientHeader `json:"headers"`
	Body               string             `json:"body"`
	FollowRedirects    bool               `json:"follow_redirects"`
	InsecureSkipVerify bool               `json:"insecure_skip_verify"`
	TimeoutMs          int                `json:"timeout_ms,omitempty"`
	Warnings           []string           `json:"warnings,omitempty"`
}

// shellSplit quebra a linha de comando como um shell POSIX faria: aspas simples, aspas duplas
// (com escapes \" \\ \$ \`), `$'...'` (ANSI-C, usado pelo "Copy as cURL (bash)" do Chrome),
// barra invertida fora de aspas e continuação de linha (`\` + quebra de linha).
func shellSplit(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inToken := false
	r := []rune(s)
	for i := 0; i < len(r); i++ {
		c := r[i]
		switch {
		case c == '\\':
			if i+1 < len(r) && (r[i+1] == '\n' || r[i+1] == '\r') {
				i++
				if r[i] == '\r' && i+1 < len(r) && r[i+1] == '\n' {
					i++
				}
				continue
			}
			if i+1 < len(r) {
				i++
				cur.WriteRune(r[i])
				inToken = true
			}
		case c == '\'':
			inToken = true
			i++
			closed := false
			for ; i < len(r); i++ {
				if r[i] == '\'' {
					closed = true
					break
				}
				cur.WriteRune(r[i])
			}
			if !closed {
				return nil, fmt.Errorf("aspas simples sem fechamento")
			}
		case c == '$' && i+1 < len(r) && r[i+1] == '\'':
			inToken = true
			i += 2
			closed := false
			for ; i < len(r); i++ {
				if r[i] == '\'' {
					closed = true
					break
				}
				if r[i] == '\\' && i+1 < len(r) {
					i++
					switch r[i] {
					case 'n':
						cur.WriteRune('\n')
					case 't':
						cur.WriteRune('\t')
					case 'r':
						cur.WriteRune('\r')
					default:
						cur.WriteRune(r[i])
					}
					continue
				}
				cur.WriteRune(r[i])
			}
			if !closed {
				return nil, fmt.Errorf("aspas $'...' sem fechamento")
			}
		case c == '"':
			inToken = true
			i++
			closed := false
			for ; i < len(r); i++ {
				if r[i] == '"' {
					closed = true
					break
				}
				if r[i] == '\\' && i+1 < len(r) {
					switch r[i+1] {
					case '"', '\\', '$', '`':
						i++
						cur.WriteRune(r[i])
						continue
					case '\n':
						i++
						continue
					}
				}
				cur.WriteRune(r[i])
			}
			if !closed {
				return nil, fmt.Errorf("aspas duplas sem fechamento")
			}
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			if inToken {
				args = append(args, cur.String())
				cur.Reset()
				inToken = false
			}
		default:
			cur.WriteRune(c)
			inToken = true
		}
	}
	if inToken {
		args = append(args, cur.String())
	}
	return args, nil
}

// curlValueFlags são as flags que consomem o argumento seguinte; o valor é o nome canônico.
var curlValueFlags = map[string]string{
	"-X": "request", "--request": "request",
	"-H": "header", "--header": "header",
	"-d": "data", "--data": "data", "--data-raw": "data", "--data-binary": "data", "--data-ascii": "data",
	"--data-urlencode": "data-urlencode",
	"--json":           "json",
	"-u":               "user", "--user": "user",
	"--url": "url",
	"-A":    "user-agent", "--user-agent": "user-agent",
	"-b": "cookie", "--cookie": "cookie",
	"-e": "referer", "--referer": "referer",
	"-m": "max-time", "--max-time": "max-time",
	"-F": "form", "--form": "form",
	// Consomem valor, mas não têm efeito aqui (ignoradas com aviso quando relevante).
	"--connect-timeout": "ignore", "-o": "ignore", "--output": "ignore", "-w": "ignore", "--write-out": "ignore",
	"--retry": "ignore", "-c": "ignore", "--cookie-jar": "ignore",
	"--cacert": "unsupported", "--cert": "unsupported", "--key": "unsupported", "-E": "unsupported",
	"-x": "unsupported", "--proxy": "unsupported", "--resolve": "unsupported",
}

// curlBoolFlags são flags sem valor; o valor é o nome canônico ("" = ignorada sem aviso).
var curlBoolFlags = map[string]string{
	"-L": "location", "--location": "location",
	"-k": "insecure", "--insecure": "insecure",
	"-G": "get", "--get": "get",
	"-I": "head", "--head": "head",
	"-s": "", "--silent": "", "-S": "", "--show-error": "", "-v": "", "--verbose": "", "-i": "", "--include": "",
	"--compressed": "", "-f": "", "--fail": "", "--http1.1": "", "--http2": "", "-N": "", "--no-buffer": "",
}

// ParseCurl converte um comando cURL no formulário da ferramenta. Método: -X explícito; senão
// HEAD com -I; senão POST quando há corpo (e não -G); senão GET — mesma regra do curl.
func ParseCurl(cmd string) (*ParsedCurl, error) {
	args, err := shellSplit(strings.TrimSpace(cmd))
	if err != nil {
		return nil, err
	}
	if len(args) > 0 && (args[0] == "curl" || strings.HasSuffix(args[0], "/curl") || args[0] == "curl.exe") {
		args = args[1:]
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("comando vazio")
	}

	p := &ParsedCurl{Headers: []HTTPClientHeader{}}
	var method string
	var dataParts []string
	getMode, headMode, jsonMode := false, false, false
	warn := func(format string, a ...interface{}) { p.Warnings = append(p.Warnings, fmt.Sprintf(format, a...)) }

	apply := func(name, val string) {
		switch name {
		case "request":
			method = strings.ToUpper(val)
		case "header":
			k, v, ok := strings.Cut(val, ":")
			if !ok {
				warn("header ignorado (sem ':'): %s", val)
				return
			}
			p.Headers = append(p.Headers, HTTPClientHeader{Key: strings.TrimSpace(k), Value: strings.TrimSpace(v)})
		case "data":
			if strings.HasPrefix(val, "@") {
				warn("corpo lido de arquivo (%s) não é suportado — cole o conteúdo no corpo", val)
			}
			dataParts = append(dataParts, val)
		case "data-urlencode":
			if k, v, ok := strings.Cut(val, "="); ok {
				dataParts = append(dataParts, k+"="+url.QueryEscape(v))
			} else {
				dataParts = append(dataParts, url.QueryEscape(val))
			}
		case "json":
			jsonMode = true
			dataParts = append(dataParts, val)
		case "user":
			p.Headers = append(p.Headers, HTTPClientHeader{Key: "Authorization", Value: "Basic " + base64.StdEncoding.EncodeToString([]byte(val))})
		case "url":
			p.URL = val
		case "user-agent":
			p.Headers = append(p.Headers, HTTPClientHeader{Key: "User-Agent", Value: val})
		case "cookie":
			p.Headers = append(p.Headers, HTTPClientHeader{Key: "Cookie", Value: val})
		case "referer":
			p.Headers = append(p.Headers, HTTPClientHeader{Key: "Referer", Value: val})
		case "max-time":
			if secs, err := strconv.ParseFloat(val, 64); err == nil && secs > 0 {
				p.TimeoutMs = int(secs * 1000)
			}
		case "form":
			warn("multipart (-F/--form) não é suportado: %s", val)
		case "unsupported":
			warn("opção ignorada (não suportada pela ferramenta)")
		case "location":
			p.FollowRedirects = true
		case "insecure":
			p.InsecureSkipVerify = true
		case "get":
			getMode = true
		case "head":
			headMode = true
		}
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case strings.HasPrefix(a, "--") && strings.Contains(a, "="):
			flag, val, _ := strings.Cut(a, "=")
			if name, ok := curlValueFlags[flag]; ok {
				apply(name, val)
			} else {
				warn("opção desconhecida ignorada: %s", flag)
			}
		case a == "--":
			continue
		case strings.HasPrefix(a, "-") && len(a) > 1:
			if name, ok := curlValueFlags[a]; ok {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("a opção %s precisa de um valor", a)
				}
				i++
				apply(name, args[i])
				continue
			}
			if name, ok := curlBoolFlags[a]; ok {
				apply(name, "")
				continue
			}
			if strings.HasPrefix(a, "--") {
				warn("opção desconhecida ignorada: %s", a)
				continue
			}
			// Flags curtas agrupadas (-sSL) ou com valor colado (-XPOST, -H'...').
			short := a[1:]
			for j := 0; j < len(short); j++ {
				f := "-" + string(short[j])
				if name, ok := curlValueFlags[f]; ok {
					val := short[j+1:]
					if val == "" {
						if i+1 >= len(args) {
							return nil, fmt.Errorf("a opção %s precisa de um valor", f)
						}
						i++
						val = args[i]
					}
					apply(name, val)
					break
				}
				if name, ok := curlBoolFlags[f]; ok {
					apply(name, "")
					continue
				}
				warn("opção desconhecida ignorada: %s", f)
			}
		default:
			if p.URL == "" {
				p.URL = a
			} else {
				warn("argumento extra ignorado: %s", a)
			}
		}
	}

	if p.URL == "" {
		return nil, fmt.Errorf("URL não encontrada no comando")
	}
	if !strings.Contains(p.URL, "://") {
		p.URL = "http://" + p.URL // mesmo default do curl
	}

	body := strings.Join(dataParts, "&")
	if getMode && body != "" {
		sep := "?"
		if strings.Contains(p.URL, "?") {
			sep = "&"
		}
		p.URL += sep + body
		body = ""
	}
	p.Body = body

	if jsonMode {
		if !hasHeader(p.Headers, "Content-Type") {
			p.Headers = append(p.Headers, HTTPClientHeader{Key: "Content-Type", Value: "application/json"})
		}
		if !hasHeader(p.Headers, "Accept") {
			p.Headers = append(p.Headers, HTTPClientHeader{Key: "Accept", Value: "application/json"})
		}
	}

	switch {
	case method != "":
		p.Method = method
	case headMode:
		p.Method = "HEAD"
	case p.Body != "":
		p.Method = "POST"
	default:
		p.Method = "GET"
	}
	return p, nil
}

func hasHeader(headers []HTTPClientHeader, key string) bool {
	for _, h := range headers {
		if strings.EqualFold(h.Key, key) {
			return true
		}
	}
	return false
}
