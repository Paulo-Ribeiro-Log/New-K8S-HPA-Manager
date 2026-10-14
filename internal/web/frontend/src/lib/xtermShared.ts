import type { Terminal } from "xterm";

// Peças comuns dos terminais xterm.js ligados a um shell remoto por WebSocket (terminal do Code
// Editor e shell/Ephemeral Debug de pods). Protocolo dos dois backends: mensagens JSON
// {type: "input"|"output", data: <base64 dos bytes crus>} e {type: "resize", cols, rows}.

// Entrada do usuário → base64 dos bytes UTF-8 (btoa() sozinho falha com chars > 255).
export function encodeTerminalInput(data: string): string {
  const bytes = new TextEncoder().encode(data);
  let binary = "";
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary);
}

// Saída do backend (base64) → bytes. Passar Uint8Array ao term.write() deixa o xterm decodificar
// o UTF-8 sozinho, inclusive sequências multibyte divididas entre duas mensagens; passar a string
// do atob() quebraria acentos/emoji (o xterm trataria cada byte como um codepoint).
export function decodeTerminalOutput(b64: string): Uint8Array {
  const binary = atob(b64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

// Copiar/colar de verdade, devolvendo a função de limpeza.
//  - Ctrl/Cmd+C com seleção ativa no xterm copia (sem seleção continua mandando \x03 = SIGINT);
//    Ctrl+Shift+C sempre copia.
//  - Ctrl/Cmd+V e Ctrl+Shift+V NÃO vão para o xterm: ele os traduziria em ^V (\x16), que no bash
//    é "inserir o próximo caractere literalmente" — o primeiro caractere colado (ou o ESC do
//    bracketed paste, que aparecia como "^[[200~") entrava sujo na linha. Devolver false sem
//    preventDefault deixa o browser disparar o "paste" nativo.
//  - O "paste" nativo (Ctrl+V, botão direito, menu Editar) é interceptado em capture phase, antes
//    do handler interno do xterm, e passa por term.paste(): quebras de linha viram CR (o \r\n
//    do Windows virava dois Enter) e, se o shell ligou o bracketed paste (DECSET 2004), o texto
//    vai entre os marcadores — tabs não disparam o autocompletar e um colar de várias linhas não
//    é executado linha a linha. ESC no texto colado é removido (fecharia o bracketed paste).
export function attachTerminalClipboard(term: Terminal, container: HTMLElement, send: (data: string) => void): () => void {
  term.attachCustomKeyEventHandler(event => {
    if (event.type !== "keydown") return true;
    const mod = event.ctrlKey || event.metaKey;
    if (!mod || event.altKey) return true;
    const key = event.key.toLowerCase();
    if (key === "c" && (event.shiftKey || term.hasSelection())) {
      event.preventDefault();
      if (term.hasSelection()) navigator.clipboard.writeText(term.getSelection()).catch(() => {});
      return false;
    }
    if (key === "v") return false;
    return true;
  });
  const handleNativePaste = (event: ClipboardEvent) => {
    event.preventDefault();
    event.stopPropagation();
    const text = event.clipboardData?.getData("text");
    if (text) pasteIntoTerminal(term, text, send);
  };
  container.addEventListener("paste", handleNativePaste, true);
  return () => container.removeEventListener("paste", handleNativePaste, true);
}

// Mesma preparação do colar do xterm (prepareTextForTerminal + bracketTextForPaste), mandando
// direto pelo `send`.
export function pasteIntoTerminal(term: Terminal, text: string, send: (data: string) => void) {
  const clean = text.split("\x1b").join("").replace(/\r?\n/g, "\r");
  send(term.modes.bracketedPasteMode ? `\x1b[200~${clean}\x1b[201~` : clean);
}
