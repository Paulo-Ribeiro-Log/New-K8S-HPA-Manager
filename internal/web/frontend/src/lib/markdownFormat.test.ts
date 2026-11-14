import { describe, expect, it } from "vitest";
import { applyEdit, applyFormat, type FormatAction } from "./markdownFormat";

const run = (text: string, s: number, e: number, a: FormatAction) => applyEdit(text, applyFormat(text, s, e, a));

describe("markdownFormat", () => {
  it("negrito embrulha a seleção e desembrulha ao repetir", () => {
    expect(run("uma palavra aqui", 4, 11, "bold")).toBe("uma **palavra** aqui");
    expect(run("uma **palavra** aqui", 6, 13, "bold")).toBe("uma palavra aqui");
  });
  it("sem seleção insere marcador com texto selecionado", () => {
    const ed = applyFormat("", 0, 0, "italic");
    expect(applyEdit("", ed)).toBe("*texto*");
    expect([ed.selStart, ed.selEnd]).toEqual([1, 6]);
  });
  it("itálico, riscado e código", () => {
    expect(run("a", 0, 1, "italic")).toBe("*a*");
    expect(run("a", 0, 1, "strike")).toBe("~~a~~");
    expect(run("a", 0, 1, "code")).toBe("`a`");
  });
  it("título troca o nível e alterna", () => {
    expect(run("Título", 0, 0, "h2")).toBe("## Título");
    expect(run("# Título", 0, 0, "h3")).toBe("### Título");
    expect(run("## Título", 0, 0, "h2")).toBe("Título");
  });
  it("listas em várias linhas, alternando", () => {
    expect(run("a\nb", 0, 3, "ul")).toBe("- a\n- b");
    expect(run("- a\n- b", 0, 7, "ul")).toBe("a\nb");
    expect(run("a\nb", 0, 3, "ol")).toBe("1. a\n2. b");
    expect(run("1. a\n2. b", 0, 9, "ol")).toBe("a\nb");
    expect(run("- a", 0, 0, "task")).toBe("- [ ] a");
    expect(run("a", 0, 0, "quote")).toBe("> a");
  });
  it("lista numerada troca o marcador de outra lista", () => {
    expect(run("- a\n- b", 0, 7, "ol")).toBe("1. a\n2. b");
  });
  it("cor embrulha com span e recusa cor insegura", () => {
    expect(run("x", 0, 1, "color:red")).toBe('<span style="color:red">x</span>');
    expect(run("x", 0, 1, "color:url(js)")).toBe("x");
  });
  it("link deixa a URL selecionada", () => {
    const ed = applyFormat("site", 0, 4, "link");
    expect(applyEdit("site", ed)).toBe("[site](https://)");
    expect(applyEdit("site", ed).slice(ed.selStart, ed.selEnd)).toBe("https://");
  });
  it("bloco de código, tabela, hr, br e caixinha", () => {
    expect(run("x", 0, 1, "codeblock")).toBe("```\nx\n```");
    expect(run("abc", 3, 3, "table")).toContain("abc\n\n| Coluna 1 |");
    expect(run("abc", 3, 3, "hr")).toBe("abc\n\n---\n");
    expect(run("a", 1, 1, "br")).toBe("a<br>");
    expect(run("a ", 2, 2, "check")).toBe("a ☐ ");
    expect(run("☐", 0, 1, "check")).toBe("☑");
  });
});

describe("quebra de linha", () => {
  it("insere dois espaços e Enter", () => {
    expect(run("a b", 1, 1, "break")).toBe("a  \n b");
  });
});
