import { describe, expect, it } from "vitest";
import { parsePatch } from "./diffLines";

describe("parsePatch", () => {
  it("vazio devolve lista vazia", () => {
    expect(parsePatch("")).toEqual([]);
  });

  it("numera contexto, remoção e adição a partir do cabeçalho do hunk", () => {
    const patch = ["@@ -10,3 +20,4 @@ func x()", " a", "-b", "+B", "+C", " d"].join("\n");
    expect(parsePatch(patch)).toEqual([
      { text: "@@ -10,3 +20,4 @@ func x()", kind: "hunk" },
      { text: " a", kind: "ctx", oldNo: 10, newNo: 20 },
      { text: "-b", kind: "del", oldNo: 11 },
      { text: "+B", kind: "add", newNo: 21 },
      { text: "+C", kind: "add", newNo: 22 },
      { text: " d", kind: "ctx", oldNo: 12, newNo: 23 },
    ]);
  });

  it("recomeça a contagem a cada hunk e aceita cabeçalho sem o tamanho (-5 +5)", () => {
    const l = parsePatch(["@@ -1,2 +1,2 @@", " x", "-y", "+z", "@@ -50 +60 @@", "-q", "+r"].join("\n"));
    expect(l[4]).toEqual({ text: "@@ -50 +60 @@", kind: "hunk" });
    expect(l[5]).toEqual({ text: "-q", kind: "del", oldNo: 50 });
    expect(l[6]).toEqual({ text: "+r", kind: "add", newNo: 60 });
  });

  it("'\\ No newline at end of file' não consome número", () => {
    const l = parsePatch(["@@ -1 +1 @@", "-a", "\\ No newline at end of file", "+b"].join("\n"));
    expect(l[2]).toEqual({ text: "\\ No newline at end of file", kind: "meta" });
    expect(l[3]).toEqual({ text: "+b", kind: "add", newNo: 1 });
  });

  it("linha que começa com +++ ou --- dentro do hunk é adição/remoção, não cabeçalho", () => {
    const l = parsePatch(["@@ -1,2 +1,2 @@", "--- 2", "+++ 3"].join("\n"));
    expect(l[1]).toMatchObject({ kind: "del", oldNo: 1 });
    expect(l[2]).toMatchObject({ kind: "add", newNo: 1 });
  });
});
