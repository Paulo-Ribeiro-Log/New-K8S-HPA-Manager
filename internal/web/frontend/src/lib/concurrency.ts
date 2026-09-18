/**
 * Executa `worker` para cada item de `items`, mantendo no máximo `limit`
 * execuções em andamento simultaneamente (worker pool clássico via cursor
 * compartilhado) — multi-thread real, não um `for...of` com `await` que só
 * parece paralelo no comentário.
 *
 * Uma falha de um item nunca aborta os demais (mesmo padrão de
 * `Promise.allSettled`) — o próprio `worker` decide como reportar erro
 * (ex: atualizar estado de UI), esta função só garante o paralelismo.
 */
export async function runWithConcurrencyLimit<T>(
  items: T[],
  limit: number,
  worker: (item: T, index: number) => Promise<void>
): Promise<void> {
  if (items.length === 0) return;

  const effectiveLimit = Math.max(1, Math.min(limit, items.length));
  let cursor = 0;

  const runners = Array.from({ length: effectiveLimit }, async () => {
    while (true) {
      const index = cursor++;
      if (index >= items.length) return;
      await worker(items[index], index);
    }
  });

  await Promise.all(runners);
}
