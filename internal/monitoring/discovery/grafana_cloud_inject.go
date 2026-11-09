package discovery

import "strings"

// injectLabelMatcher devolve a expressão PromQL com o matcher `label="value"` acrescentado a TODO
// seletor de série (`metric`, `metric{...}`, `{...}`). É o que isola um cluster dentro de um tenant
// compartilhado do Grafana Cloud, onde vários clusters gravam as mesmas métricas.
//
// Não há parser PromQL vendorizado, então é um tokenizador mínimo: pula strings, comentários,
// durações (`[5m:1m]`, `offset 1h`), listas de rótulos (`by (...)`, `on (...)`) e nomes de função;
// qualquer outro identificador é nome de métrica. Um matcher repetido é válido em PromQL (os
// matchers são combinados com E), então não tenta deduplicar.
func injectLabelMatcher(expr, label, value string) string {
	matcher := label + `="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
	var b strings.Builder
	b.Grow(len(expr) + 32)
	n := len(expr)
	i := 0
	for i < n {
		ch := expr[i]
		switch {
		case ch == '"' || ch == '\'' || ch == '`':
			j := skipString(expr, i)
			b.WriteString(expr[i:j])
			i = j
		case ch == '#':
			j := strings.IndexByte(expr[i:], '\n')
			if j < 0 {
				j = n - i
			}
			b.WriteString(expr[i : i+j])
			i += j
		case ch == '[':
			j := strings.IndexByte(expr[i:], ']')
			if j < 0 {
				j = n - i - 1
			}
			b.WriteString(expr[i : i+j+1])
			i += j + 1
		case ch == '{':
			i = writeSelectorBraces(&b, expr, i, matcher)
		case isDigit(ch) || (ch == '.' && i+1 < n && isDigit(expr[i+1])):
			j := skipNumber(expr, i)
			b.WriteString(expr[i:j])
			i = j
		case isIdentStart(ch):
			j := i + 1
			for j < n && isIdentPart(expr[j]) {
				j++
			}
			word := expr[i:j]
			b.WriteString(word)
			i = j
			k := skipSpaces(expr, i)
			switch {
			case isGroupingKeyword(word):
				// `by (a, b)`, `on (x)`: lista de rótulos, nunca de séries.
				if k < n && expr[k] == '(' {
					end := strings.IndexByte(expr[k:], ')')
					if end < 0 {
						end = n - k - 1
					}
					b.WriteString(expr[i : k+end+1])
					i = k + end + 1
				}
			case isOperatorKeyword(word):
				// `and`, `or`, `offset`, `bool`... nada a fazer.
			case k < n && expr[k] == '(':
				// função ou agregação (rate(, sum(, label_replace(...).
			case nextWordIsGrouping(expr, k):
				// agregação com modificador antes dos argumentos: `sum by (x) (...)`.
			case k < n && expr[k] == '{':
				b.WriteString(expr[i:k])
				i = writeSelectorBraces(&b, expr, k, matcher)
			default:
				b.WriteString("{" + matcher + "}")
			}
		default:
			b.WriteByte(ch)
			i++
		}
	}
	return b.String()
}

// writeSelectorBraces copia `{...}` (a partir de expr[start]=='{') injetando o matcher no começo e
// devolve o índice logo depois do `}`.
func writeSelectorBraces(b *strings.Builder, expr string, start int, matcher string) int {
	n := len(expr)
	b.WriteByte('{')
	b.WriteString(matcher)
	i := start + 1
	if k := skipSpaces(expr, i); k < n && expr[k] != '}' {
		b.WriteByte(',')
	}
	for i < n {
		ch := expr[i]
		switch ch {
		case '"', '\'', '`':
			j := skipString(expr, i)
			b.WriteString(expr[i:j])
			i = j
		case '}':
			b.WriteByte('}')
			return i + 1
		default:
			b.WriteByte(ch)
			i++
		}
	}
	return i
}

// skipString devolve o índice logo depois da string que começa em expr[start].
func skipString(expr string, start int) int {
	quote := expr[start]
	i := start + 1
	for i < len(expr) {
		switch {
		case expr[i] == '\\' && quote != '`':
			i += 2
			continue
		case expr[i] == quote:
			return i + 1
		}
		i++
	}
	return len(expr)
}

// skipNumber consome número ou duração (`5m`, `1h30m`, `1e-3`, `0x1f`, `.5`).
func skipNumber(expr string, start int) int {
	i := start
	for i < len(expr) {
		c := expr[i]
		if isIdentPart(c) || c == '.' {
			i++
			continue
		}
		if (c == '+' || c == '-') && i > start && (expr[i-1] == 'e' || expr[i-1] == 'E') &&
			i+1 < len(expr) && isDigit(expr[i+1]) && !strings.HasPrefix(expr[start:], "0x") {
			i++
			continue
		}
		break
	}
	return i
}

// nextWordIsGrouping diz se em expr[k:] começa `by` ou `without` (modificador de agregação).
func nextWordIsGrouping(expr string, k int) bool {
	j := k
	for j < len(expr) && isIdentPart(expr[j]) {
		j++
	}
	w := strings.ToLower(expr[k:j])
	return w == "by" || w == "without"
}

func skipSpaces(expr string, i int) int {
	for i < len(expr) && (expr[i] == ' ' || expr[i] == '\t' || expr[i] == '\n' || expr[i] == '\r') {
		i++
	}
	return i
}

func isDigit(c byte) bool      { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool { return c == '_' || c == ':' || (c|0x20 >= 'a' && c|0x20 <= 'z') }
func isIdentPart(c byte) bool  { return isIdentStart(c) || isDigit(c) }

func isGroupingKeyword(w string) bool {
	switch strings.ToLower(w) {
	case "by", "without", "on", "ignoring", "group_left", "group_right":
		return true
	}
	return false
}

func isOperatorKeyword(w string) bool {
	switch strings.ToLower(w) {
	case "and", "or", "unless", "bool", "offset", "inf", "nan", "start", "end":
		return true
	}
	return false
}
