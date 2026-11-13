import { describe, expect, it } from "vitest";
import { cleanConfluenceEscapes, colorFromTag, remarkMdExtras, toggleCheckAt, toggleTaskAt } from "./markdownPreview";

describe("colorFromTag", () => {
  it("lê style e atributo, recusa o que não é cor", () => {
    expect(colorFromTag(' style="color: red;"')).toBe("red");
    expect(colorFromTag(' color="#c00"')).toBe("#c00");
    expect(colorFromTag(' style="color:url(x)"')).toBe("");
    expect(colorFromTag(' style="background:red"')).toBe("");
  });
});

describe("cleanConfluenceEscapes", () => {
  it("tira as barras, mantém \\| e blocos de código", () => {
    expect(cleanConfluenceEscapes("\\*\\*1\\*\\* \\<br\\> \\`x\\` \\> \\[!NOTE\\]")).toBe("**1** <br> `x` > [!NOTE]");
    expect(cleanConfluenceEscapes("a \\| b")).toBe("a \\| b");
    expect(cleanConfluenceEscapes("```\n\\*x\\*\n```\n\\*y\\*")).toBe("```\n\\*x\\*\n```\n*y*");
  });
});

describe("toggle", () => {
  it("alterna ☐/☑ só se a posição tiver o caractere", () => {
    expect(toggleCheckAt("| a | ☐ |", 1, 7)).toBe("| a | ☑ |");
    expect(toggleCheckAt("| a | ☑ |", 1, 7)).toBe("| a | ☐ |");
    expect(toggleCheckAt("| a | ☐ |", 1, 3)).toBe("| a | ☐ |");
    expect(toggleCheckAt("x", 5, 1)).toBe("x");
  });
  it("alterna - [ ]", () => {
    expect(toggleTaskAt("- [ ] a\n- [x] b", 1)).toBe("- [x] a\n- [x] b");
    expect(toggleTaskAt("- [ ] a\n- [x] b", 2)).toBe("- [ ] a\n- [ ] b");
    expect(toggleTaskAt("texto", 1)).toBe("texto");
  });
});

describe("remarkMdExtras", () => {
  const run = (kids: any[]) => { const t: any = { type: "root", children: [{ type: "paragraph", children: kids }] }; remarkMdExtras()(t); return t.children[0].children; };
  it("br vira quebra; span com cor vira nó colorido; tag sem fechar mantém o texto", () => {
    const r = run([{ type: "text", value: "a" }, { type: "html", value: "<br>" }, { type: "html", value: '<span style="color:red">' }, { type: "text", value: "x" }, { type: "html", value: "</span>" }]);
    expect(r.map((n: any) => n.type)).toEqual(["text", "break", "mdColor"]);
    expect(r[2].data.hProperties.style).toBe("color:red");
    const open = run([{ type: "html", value: '<font color="red">' }, { type: "text", value: "x" }]);
    expect(open.map((n: any) => n.type)).toEqual(["text"]);
  });
  it("☐ vira nó clicável com linha e coluna", () => {
    const r = run([{ type: "text", value: "a ☐", position: { start: { line: 3, column: 5 } } }]);
    expect(r.map((n: any) => n.type)).toEqual(["text", "mdCheck"]);
    expect(r[1].data.hProperties["data-check-line"]).toBe(3);
    expect(r[1].data.hProperties["data-check-col"]).toBe(7);
  });
});
