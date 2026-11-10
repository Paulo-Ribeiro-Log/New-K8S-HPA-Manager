import { describe, expect, it } from "vitest";
import { packRecipients, unpackRecipients, type BroadcastChat } from "./broadcastRecipients";

const chats: BroadcastChat[] = [
  { id: "19:a@thread.v2", display_name: "Time --> Ops", source: "chat" },
  { id: "19:b", display_name: "Ana", source: "person" },
];
const md = "# Aviso\n\nTexto";

describe("packRecipients / unpackRecipients", () => {
  it("ida e volta preserva o texto e os destinatários", () => {
    const r = unpackRecipients(packRecipients(md, chats));
    expect(r.content).toBe(md);
    expect(r.recipients).toEqual(chats);
  });

  it("o bloco vai num comentário HTML no topo e escapa '>' para um nome não fechá-lo antes da hora", () => {
    const packed = packRecipients(md, chats);
    expect(packed.startsWith("<!--broadcast-recipients\n")).toBe(true);
    const header = packed.slice(0, packed.indexOf("\n-->"));
    expect(header).not.toContain("-->"); // o "-->" do nome virou >
    expect(header).toContain("\\u003e");
  });

  it("sem destinatários o texto não muda", () => {
    expect(packRecipients(md, [])).toBe(md);
  });

  it("arquivo sem bloco (modelos antigos) abre como antes e não mexe na seleção", () => {
    expect(unpackRecipients(md)).toEqual({ content: md, recipients: null });
  });

  it("bloco ilegível é descartado e o texto é mantido", () => {
    expect(unpackRecipients("<!--broadcast-recipients\n{ruim\n-->\nOi")).toEqual({ content: "Oi", recipients: null });
    expect(unpackRecipients('<!--broadcast-recipients\n{"a":1}\n-->\nOi')).toEqual({ content: "Oi", recipients: null });
  });

  it("ignora entradas sem id e completa campos ausentes", () => {
    const raw = '<!--broadcast-recipients\n[{"id":"19:x"},{"display_name":"sem id"},null,{"id":"19:y","display_name":"Bia","source":"chat"}]\n-->\nTexto';
    const r = unpackRecipients(raw);
    expect(r.content).toBe("Texto");
    expect(r.recipients).toEqual([
      { id: "19:x", display_name: "19:x", source: "" },
      { id: "19:y", display_name: "Bia", source: "chat" },
    ]);
  });

  it("aceita quebras de linha do Windows", () => {
    const r = unpackRecipients('<!--broadcast-recipients\r\n[{"id":"19:x","display_name":"A","source":"c"}]\r\n-->\r\nTexto');
    expect(r.content).toBe("Texto");
    expect(r.recipients).toHaveLength(1);
  });
});
