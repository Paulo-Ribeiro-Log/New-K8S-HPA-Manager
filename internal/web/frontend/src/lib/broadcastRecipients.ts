// Destinatários dentro do arquivo salvo do Teams Broadcast. Ao salvar com a opção "salvar os
// destinatários", o arquivo ganha no topo um comentário HTML com a lista em JSON (invisível em
// qualquer visualizador de Markdown). Ao abrir, o bloco é removido do texto e a seleção é restaurada.
// ">" vai escapado (\\u003e) para um nome nunca fechar o comentário antes da hora.
// Lógica pura, testada em broadcastRecipients.test.ts.

export interface BroadcastChat {
  id: string;
  display_name: string;
  source: string;
}

const RECIPIENTS_BLOCK = /^<!--broadcast-recipients\r?\n([\s\S]*?)\r?\n-->\r?\n?/;

export function packRecipients(content: string, chats: BroadcastChat[]): string {
  if (chats.length === 0) return content;
  const json = JSON.stringify(chats.map((c) => ({ id: c.id, display_name: c.display_name, source: c.source })))
    .replace(/>/g, "\\u003e");
  return `<!--broadcast-recipients\n${json}\n-->\n${content}`;
}

export function unpackRecipients(raw: string): { content: string; recipients: BroadcastChat[] | null } {
  const m = raw.match(RECIPIENTS_BLOCK);
  if (!m) return { content: raw, recipients: null };
  try {
    const arr = JSON.parse(m[1]);
    if (!Array.isArray(arr)) throw new Error("formato");
    const recipients = arr
      .filter((c) => c && typeof c.id === "string" && c.id)
      .map((c) => ({ id: c.id, display_name: String(c.display_name ?? c.id), source: String(c.source ?? "") }));
    return { content: raw.slice(m[0].length), recipients };
  } catch {
    return { content: raw.slice(m[0].length), recipients: null }; // bloco ilegível: ignora, mantém o texto
  }
}
