// Utilitários de destaque de texto das mensagens do Teste Kafka (KafkaMessagesModal/KafkaTestTab).

function escapeRegExp(s: string) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/** Regex que casa qualquer um dos termos (sem diferenciar maiúsculas), ou null se não houver termo. */
export function termsRegex(terms: string[]): RegExp | null {
  const valid = terms.map((t) => t.trim()).filter(Boolean);
  if (valid.length === 0) return null;
  // Termos maiores primeiro: `"SKU":"teste"` ganha de `SKU` quando os dois casam no mesmo trecho.
  valid.sort((a, b) => b.length - a.length);
  return new RegExp(`(${valid.map(escapeRegExp).join("|")})`, "gi");
}

/** Trecho curto começando perto da primeira ocorrência — para ela aparecer mesmo em payload longo. */
export function snippetAround(text: string, terms: string[], radius = 60): string {
  const re = termsRegex(terms);
  const idx = re ? text.search(re) : -1;
  if (idx <= radius) return text;
  return "…" + text.slice(idx - radius);
}
