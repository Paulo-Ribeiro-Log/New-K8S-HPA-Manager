export interface JsonFormatResult {
  valid: boolean;
  formatted: string;
  error?: string;
  errorLine?: number;
  errorCol?: number;
  charCount: number;
  lineCount: number;
  wasExtracted?: boolean; // JSON foi extraído de um texto maior (ex: linha de log com prefixo)
}

// Tenta extrair linha/coluna da mensagem de erro do V8
function parseErrorPosition(error: string, input: string): { line?: number; col?: number } {
  // V8 moderno: "... (line N column M)"
  const modern = error.match(/\(line (\d+) column (\d+)\)/);
  if (modern) return { line: parseInt(modern[1]), col: parseInt(modern[2]) };

  // V8 antigo: "at position N"
  const posMatch = error.match(/at position (\d+)/);
  if (posMatch) {
    const pos = parseInt(posMatch[1]);
    const before = input.slice(0, pos);
    const lines = before.split("\n");
    return { line: lines.length, col: lines[lines.length - 1].length + 1 };
  }

  return {};
}

// Extrai o primeiro bloco JSON balanceado ({ } ou [ ]) do texto.
// Útil quando a seleção contém prefixo de log antes do JSON.
function extractJsonBlock(text: string): string | null {
  const firstObj = text.indexOf("{");
  const firstArr = text.indexOf("[");

  let start = -1;
  if (firstObj !== -1 && firstArr !== -1) start = Math.min(firstObj, firstArr);
  else if (firstObj !== -1) start = firstObj;
  else if (firstArr !== -1) start = firstArr;

  if (start === -1) return null;

  const openChar = text[start];
  const closeChar = openChar === "{" ? "}" : "]";
  let depth = 0;
  let inString = false;
  let escaped = false;

  for (let i = start; i < text.length; i++) {
    const ch = text[i];
    if (escaped) { escaped = false; continue; }
    if (ch === "\\" && inString) { escaped = true; continue; }
    if (ch === '"') { inString = !inString; continue; }
    if (inString) continue;
    if (ch === openChar) depth++;
    else if (ch === closeChar) {
      depth--;
      if (depth === 0) return text.slice(start, i + 1);
    }
  }

  return null;
}

export function tryFormatJson(input: string): JsonFormatResult {
  const trimmed = input.trim();
  const charCount = trimmed.length;

  // Tenta o texto completo primeiro
  try {
    const parsed = JSON.parse(trimmed);
    const formatted = JSON.stringify(parsed, null, 2);
    return { valid: true, formatted, charCount, lineCount: formatted.split("\n").length };
  } catch (fullErr) {
    const fullError = (fullErr as Error).message;

    // Tenta extrair bloco JSON embutido (ex: prefixo de timestamp ou level no log)
    const extracted = extractJsonBlock(trimmed);
    if (extracted && extracted !== trimmed) {
      try {
        const parsed = JSON.parse(extracted);
        const formatted = JSON.stringify(parsed, null, 2);
        return {
          valid: true,
          formatted,
          charCount: extracted.length,
          lineCount: formatted.split("\n").length,
          wasExtracted: true,
        };
      } catch {
        // extração também falhou — usa o erro original abaixo
      }
    }

    // Sem JSON válido — retorna com posição do erro para destacar a linha
    const { line: errorLine, col: errorCol } = parseErrorPosition(fullError, trimmed);
    return {
      valid: false,
      formatted: trimmed,
      error: fullError,
      errorLine,
      errorCol,
      charCount,
      lineCount: trimmed.split("\n").length,
    };
  }
}

export type JsonTokenType = "key" | "string" | "number" | "boolean" | "null" | "punctuation" | "space";

export interface JsonToken {
  type: JsonTokenType;
  value: string;
}

// Tokeniza JSON formatado para syntax highlighting.
// Espera JSON já passado por JSON.stringify com indent.
export function tokenizeJson(json: string): JsonToken[] {
  const tokens: JsonToken[] = [];
  const regex =
    /("(?:\\u[0-9a-fA-F]{4}|\\[^u]|[^\\"])*")(\s*:)?|true|false|null|-?\d+(?:\.\d*)?(?:[eE][+-]?\d+)?|[{}[\],]/g;

  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = regex.exec(json)) !== null) {
    if (match.index > lastIndex) {
      tokens.push({ type: "space", value: json.slice(lastIndex, match.index) });
    }

    const stringBody = match[1];
    const colonPart = match[2];
    const full = match[0];

    if (stringBody !== undefined) {
      if (colonPart) {
        tokens.push({ type: "key", value: stringBody });
        tokens.push({ type: "punctuation", value: colonPart });
      } else {
        tokens.push({ type: "string", value: stringBody });
      }
    } else if (full === "true" || full === "false") {
      tokens.push({ type: "boolean", value: full });
    } else if (full === "null") {
      tokens.push({ type: "null", value: full });
    } else if (/^-?\d/.test(full)) {
      tokens.push({ type: "number", value: full });
    } else {
      tokens.push({ type: "punctuation", value: full });
    }

    lastIndex = regex.lastIndex;
  }

  if (lastIndex < json.length) {
    tokens.push({ type: "space", value: json.slice(lastIndex) });
  }

  return tokens;
}
