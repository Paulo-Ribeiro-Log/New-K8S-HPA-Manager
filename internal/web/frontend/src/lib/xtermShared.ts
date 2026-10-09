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
//  - Ctrl/Cmd+C com seleção ativa no xterm copia (sem seleção continua mandando \x03 = SIGINT).
//  - Colar (Ctrl+V, botão direito, menu Editar — todos disparam o evento nativo "paste") é
//    interceptado em capture phase no container, ANTES do handler interno do xterm, e o texto vai
//    cru para o shell por `send`. Sem isso o xterm embrulha o texto nos marcadores de bracketed
//    paste (ESC[200~…ESC[201~) quando o shell liga o modo (DECSET 2004), e eles podiam aparecer
//    como texto na linha ("^[[200~…~") e bagunçar a edição.
export function attachTerminalClipboard(term: Terminal, container: HTMLElement, send: (data: string) => void): () => void {
  term.attachCustomKeyEventHandler(event => {
    if (event.type !== "keydown") return true;
    const mod = event.ctrlKey || event.metaKey;
    if (!mod || event.altKey) return true;
    if (event.key.toLowerCase() === "c" && term.hasSelection()) {
      event.preventDefault();
      navigator.clipboard.writeText(term.getSelection()).catch(() => {});
      return false;
    }
    return true;
  });
  const handleNativePaste = (event: ClipboardEvent) => {
    event.preventDefault();
    event.stopPropagation();
    const text = event.clipboardData?.getData("text");
    if (text) send(text);
  };
  container.addEventListener("paste", handleNativePaste, true);
  return () => container.removeEventListener("paste", handleNativePaste, true);
}
