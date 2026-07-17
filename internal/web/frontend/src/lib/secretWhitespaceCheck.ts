import * as yaml from "js-yaml";

export type WhitespaceIssue = "leading" | "trailing";

export interface SecretDataLine {
  key: string;
  rawValue: string;
  lineNumber: number; // 1-based, para decorations do Monaco
}

export interface SecretKeyWhitespaceResult {
  key: string;
  lineNumber: number;
  issues: WhitespaceIssue[];
  decodedPreview: string;
}

// Mesmo padrão de linha `chave: valor` já usado por handleToggleDecode em SecretsTab.tsx —
// mantido em sincronia de propósito (não vale a pena abstrair mais que isso).
const DATA_LINE_REGEX = /^(\s+)([^:]+):\s+(.+)$/;

const CERT_KEY_PATTERNS = [".crt", ".cert", ".pem", ".key", "tls.crt", "tls.key", "ca.crt", "ca.key", "certificate", "private", "public"];
const CERT_MARKERS = [
  "-----BEGIN CERTIFICATE-----",
  "-----BEGIN PRIVATE KEY-----",
  "-----BEGIN RSA PRIVATE KEY-----",
  "-----BEGIN PUBLIC KEY-----",
  "-----BEGIN EC PRIVATE KEY-----",
];

// Certificados/chaves quase sempre têm \n no fim do PEM por convenção — não é bug, então
// ficam fora do escopo desta checagem (mesma exclusão que handleToggleDecode já aplica).
function isCertificateOrKey(key: string, decoded: string): boolean {
  const keyLower = key.toLowerCase();
  if (CERT_KEY_PATTERNS.some((pattern) => keyLower.includes(pattern))) return true;
  return CERT_MARKERS.some((marker) => decoded.includes(marker));
}

function isValidBase64(str: string): boolean {
  if (!str || str.trim() === "") return false;
  const base64Regex = /^[A-Za-z0-9+/]*={0,2}$/;
  if (!base64Regex.test(str)) return false;
  try {
    const decoded = atob(str);
    return btoa(decoded) === str;
  } catch {
    return false;
  }
}

function base64DecodeUtf8(str: string): string {
  return decodeURIComponent(
    Array.prototype.map
      .call(atob(str), (c: string) => "%" + ("00" + c.charCodeAt(0).toString(16)).slice(-2))
      .join("")
  );
}

export function parseSecretDataLines(yamlText: string): SecretDataLine[] {
  let yamlObj: any;
  try {
    yamlObj = yaml.load(yamlText);
  } catch {
    return [];
  }
  if (!yamlObj || typeof yamlObj !== "object" || !yamlObj.data) return [];

  const lines = yamlText.split("\n");
  const result: SecretDataLine[] = [];
  for (let i = 0; i < lines.length; i++) {
    const match = lines[i].match(DATA_LINE_REGEX);
    if (!match) continue;
    const [, , key, value] = match;
    if (key in yamlObj.data) {
      result.push({ key, rawValue: value.trim(), lineNumber: i + 1 });
    }
  }
  return result;
}

export function detectWhitespaceIssues(decoded: string): WhitespaceIssue[] {
  const issues: WhitespaceIssue[] = [];
  if (/^\s/.test(decoded)) issues.push("leading");
  if (/\s$/.test(decoded)) issues.push("trailing");
  return issues;
}

function visibleWhitespace(s: string): string {
  const truncated = s.length > 80 ? s.slice(0, 80) + "…" : s;
  return truncated.replace(/\n/g, "\\n").replace(/\t/g, "\\t").replace(/\r/g, "\\r");
}

// Só sinaliza espaço/tab/newline/CR na BORDA (início ou fim) do valor decodificado — o caso
// clássico do `echo` sem `-n`. Não checa espaço interno, pra não gerar falso-positivo em valores
// que legitimamente têm espaços (connection strings, JSON etc.) — decisão de escopo do plano.
export function scanSecretForWhitespaceIssues(yamlText: string): SecretKeyWhitespaceResult[] {
  const results: SecretKeyWhitespaceResult[] = [];

  for (const { key, rawValue, lineNumber } of parseSecretDataLines(yamlText)) {
    if (!isValidBase64(rawValue)) continue; // já decodificado (modo "Decoded") ou não é base64

    let decoded: string;
    try {
      decoded = base64DecodeUtf8(rawValue);
    } catch {
      continue;
    }

    if (isCertificateOrKey(key, decoded)) continue;

    const issues = detectWhitespaceIssues(decoded);
    if (issues.length === 0) continue;

    results.push({ key, lineNumber, issues, decodedPreview: visibleWhitespace(decoded) });
  }

  return results;
}
