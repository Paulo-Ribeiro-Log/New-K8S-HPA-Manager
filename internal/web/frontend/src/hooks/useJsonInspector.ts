import { useState, useCallback, useEffect } from "react";

export interface JsonInspectorState {
  open: boolean;
  text: string;
  floatingPos: { x: number; y: number } | null;
  handleMouseUp: () => void;
  openInspector: () => void;
  setOpen: (v: boolean) => void;
}

/**
 * Hook para inspecionar JSON a partir de seleção de texto em áreas de log.
 *
 * Uso:
 *   const json = useJsonInspector();
 *   // 1. Adicione onMouseUp={json.handleMouseUp} ao container de log
 *   // 2. Renderize o botão flutuante quando json.floatingPos !== null
 *   // 3. Renderize <JsonInspectorModal open={json.open} ... text={json.text} />
 *
 * Não funciona em <textarea> — para esses casos, use o overload com textareaRef.
 */
export function useJsonInspector(): JsonInspectorState {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const [floatingPos, setFloatingPos] = useState<{ x: number; y: number } | null>(null);

  // Esconde o botão flutuante quando a seleção é desmarcada
  useEffect(() => {
    const onSelectionChange = () => {
      const sel = window.getSelection();
      if (!sel || sel.isCollapsed || !sel.toString().trim()) {
        setFloatingPos(null);
      }
    };
    document.addEventListener("selectionchange", onSelectionChange);
    return () => document.removeEventListener("selectionchange", onSelectionChange);
  }, []);

  const handleMouseUp = useCallback(() => {
    const sel = window.getSelection();
    if (!sel || sel.isCollapsed) return;
    const selected = sel.toString().trim();
    if (!selected) return;
    try {
      const rect = sel.getRangeAt(0).getBoundingClientRect();
      setFloatingPos({
        x: Math.min(rect.right, window.innerWidth - 180),
        y: Math.min(rect.bottom + 6, window.innerHeight - 40),
      });
    } catch {
      // getBoundingClientRect pode falhar em edge cases
    }
  }, []);

  const openInspector = useCallback(() => {
    const sel = window.getSelection()?.toString().trim() ?? "";
    if (sel) {
      setText(sel);
      setOpen(true);
    }
    setFloatingPos(null);
  }, []);

  return { open, text, floatingPos, handleMouseUp, openInspector, setOpen };
}
