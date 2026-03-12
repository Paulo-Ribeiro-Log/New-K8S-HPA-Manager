// Limite máximo de entradas no cache de histórico de edição por aba
const MAX_HISTORY_KEYS = 50;

/**
 * Insere uma entrada no historyCache com eviction FIFO quando o limite é atingido.
 * Evita acúmulo ilimitado de memória ao editar muitos recursos diferentes.
 */
export function setHistoryCacheEntry<T>(
  cache: Map<string, T>,
  key: string,
  value: T,
): void {
  if (!cache.has(key) && cache.size >= MAX_HISTORY_KEYS) {
    const oldest = cache.keys().next().value;
    if (oldest !== undefined) cache.delete(oldest);
  }
  cache.set(key, value);
}
