import { RefObject, useEffect, useRef } from "react";

/**
 * Rola o elemento `[data-item-key="<focusKey>"]` dentro de `containerRef` para a área
 * visível sempre que `focusKey` mudar (ignora re-renders sem mudança de chave).
 *
 * Escopado a `containerRef` de propósito — várias abas de workload ficam montadas em
 * `display:none` em segundo plano, então uma busca via `document.querySelector` poderia
 * casar com uma instância oculta de outra aba.
 */
export function useRevealOnKeyChange(
  containerRef: RefObject<HTMLElement | null>,
  focusKey: string | null | undefined
): void {
  const prevKeyRef = useRef<string | null>(null);

  useEffect(() => {
    if (!focusKey || focusKey === prevKeyRef.current) return;
    prevKeyRef.current = focusKey;

    const el = containerRef.current?.querySelector<HTMLElement>(
      `[data-item-key="${CSS.escape(focusKey)}"]`
    );
    el?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  }, [focusKey, containerRef]);
}
