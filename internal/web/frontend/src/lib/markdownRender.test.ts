// Teste de formatação: renderiza Markdown no mesmo pipeline do preview (react-markdown + remark-gfm + remarkMdExtras)
// e confere o HTML gerado. Os textos imitam o que o export do Confluence produz (ST Integrado).
import { createElement as h } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { describe, expect, it } from "vitest";
import { fixMarkdown, remarkMdExtras, remarkSoftBreaks } from "./markdownPreview";

const render = (md: string) =>
  renderToStaticMarkup(h(ReactMarkdown, { remarkPlugins: [remarkGfm, remarkMdExtras] }, md));

const NBSP = " ";
const TABELA =
  "| App | Total | CPU |\n| --- | --- | --- |\n" +
  "| `frete-hub` <br> *(ns: calculo-prd)* | **1170869** | **9.0%** <br> *(Min: 1.9%, Avg: 6.7%)* |\n";

describe("formatação do preview", () => {
  it("tabela: <br>, negrito, itálico e crase dentro das células", () => {
    const html = render(TABELA);
    expect(html).toContain("<td><code>frete-hub</code> <br/>");
    expect(html).toContain("<em>(ns: calculo-prd)</em>");
    expect(html).toContain("<strong>1170869</strong>");
    expect(html).toContain("<strong>9.0%</strong> <br/>");
    expect(html).not.toContain("**");
    expect(html).not.toContain("&lt;br");
  });

  it("cores: span style e font color, em célula e em parágrafo", () => {
    const html = render('| a |\n| --- |\n| <span style="color:red">crítico</span> |\n\n<font color="#0a0">ok</font> fim\n');
    expect(html).toContain('<span style="color:red">crítico</span>');
    expect(html).toContain('<span style="color:#0a0">ok</span>');
  });

  it("cor insegura não vira estilo", () => {
    expect(render('x <span style="color:url(javascript:1)">y</span>')).not.toContain("style=");
  });

  it("☐ em tabela vira marca clicável com linha e coluna", () => {
    const html = render("| item | ok |\n| --- | --- |\n| HPA | ☐ |\n");
    expect(html).toMatch(/<span data-check-line="3" data-check-col="\d+" class="md-check">☐<\/span>/);
  });

  it("negrito com espaço dentro (Confluence) só renderiza depois de Corrigir Markdown", () => {
    const quebrado = `carga de até** 1745 usuários${NBSP}**para simular\n\n**Black Friday 2026 **\n`;
    expect(render(quebrado)).toContain("**"); // o problema
    const ok = render(fixMarkdown(quebrado));
    expect(ok).not.toContain("**");
    expect(ok).toContain("<strong>1745 usuários</strong>");
    expect(ok).toContain("<strong>Black Friday 2026</strong>");
  });

  it("barras de escape do Confluence só renderizam depois de Corrigir Markdown", () => {
    const esc = "| \\*\\*1170869\\*\\* | \\<br\\> |\n| --- | --- |\n| `a` \\<br\\> \\*x\\* | \\*\\*2\\*\\* |\n";
    const antes = render(esc); // o Markdown consome a barra e deixa ** e <br> como texto
    expect(antes).toContain("**1170869**");
    expect(antes).toContain("&lt;br&gt;");
    const ok = render(fixMarkdown(esc));
    expect(ok).not.toContain("**");
    expect(ok).not.toContain("&lt;br&gt;");
    expect(ok).toContain("<strong>2</strong>");
    expect(ok).toContain("<br/>");
  });

  it("fixMarkdown não mexe em texto já certo nem em bloco de código", () => {
    expect(fixMarkdown(TABELA)).toBe(TABELA);
    const code = "```\n** x **\n```\n";
    expect(fixMarkdown(code)).toBe(code);
  });
});

describe("quebra de linha", () => {
  const renderSoft = (md: string) =>
    renderToStaticMarkup(h(ReactMarkdown, { remarkPlugins: [remarkGfm, remarkMdExtras, remarkSoftBreaks] }, md));
  it("Enter simples junta as linhas no Markdown padrão e quebra com o plugin", () => {
    expect(render("linha 1\nlinha 2")).not.toContain("<br");
    expect(renderSoft("linha 1\nlinha 2")).toContain("linha 1<br/>");
  });
  it("dois espaços no fim da linha quebram nos dois modos", () => {
    expect(render("linha 1  \nlinha 2")).toContain("<br/>");
  });
  it("☐ depois de uma quebra continua com a coluna certa", () => {
    const html = render("texto\n☐ item");
    expect(html).toMatch(/data-check-line="2" data-check-col="1"/);
  });
  it("não mexe em bloco de código", () => {
    expect(renderSoft("```\na\nb\n```")).not.toContain("<br");
  });
});
