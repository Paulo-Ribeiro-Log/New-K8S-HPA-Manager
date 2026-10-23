package handlers

import (
	"strings"
	"testing"
)

func TestMarkdownToTeamsHTML_Formatacao(t *testing.T) {
	cases := []struct {
		nome, md string
		contem   []string
		naoTem   []string
	}{
		{
			nome: "cabeçalho de tabela colorido",
			md:   "| A | B |\n|---|---|\n| 1 | 2 |",
			contem: []string{
				`<th style="background-color:#0078d4;color:#ffffff">A</th>`,
				"<td>1</td>",
			},
		},
		{
			nome:   "título com tamanho",
			md:     "# Grande\n## Médio\n### Pequeno",
			contem: []string{`font-size:22px">Grande`, `font-size:18px">Médio`, `font-size:15px">Pequeno`},
			naoTem: []string{"<h1>", "# "},
		},
		{
			nome:   "regra horizontal",
			md:     "antes\n\n---\n\ndepois",
			contem: []string{"<hr>"},
		},
		{
			nome:   "bloco de código preserva indentação e escapa HTML",
			md:     "```\nif a < b {\n    x()\n}\n```",
			contem: []string{"<pre>if a &lt; b {\n    x()\n}</pre>"},
		},
		{
			nome:   "citação em várias linhas vira um blockquote",
			md:     "> linha **um**\n> linha dois",
			contem: []string{"<blockquote>linha <b>um</b><br>linha dois</blockquote>"},
		},
		{
			nome:   "lista não vira regra horizontal",
			md:     "- item\n- outro",
			contem: []string{"<ul><li>item</li><li>outro</li></ul>"},
			naoTem: []string{"<hr>"},
		},
	}
	for _, c := range cases {
		t.Run(c.nome, func(t *testing.T) {
			got := markdownToTeamsHTML(c.md)
			for _, want := range c.contem {
				if !strings.Contains(got, want) {
					t.Errorf("faltou %q em %s", want, got)
				}
			}
			for _, bad := range c.naoTem {
				if strings.Contains(got, bad) {
					t.Errorf("não deveria conter %q em %s", bad, got)
				}
			}
		})
	}
}
