import { useState, useCallback, useEffect, useRef } from "react";

/** Hook para controlar larguras de colunas redimensionáveis */
export function useResizableColumns(initial: number[]) {
  const [widths, setWidths] = useState(initial);

  const resize = useCallback((idx: number, delta: number) => {
    setWidths((prev) => {
      const next = [...prev];
      next[idx] = Math.max(40, next[idx] + delta);
      return next;
    });
  }, []);

  const gridTemplate = widths.map((w) => `${w}px`).join(" ");
  return { widths, resize, gridTemplate };
}

/** Alça de redimensionamento — coloque dentro de um container `relative` */
export function ResizeHandle({ onResize }: { onResize: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastX = useRef(0);
  const cb = useRef(onResize);
  useEffect(() => { cb.current = onResize; }, [onResize]);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      cb.current(e.clientX - lastX.current);
      lastX.current = e.clientX;
    };
    const onUp = () => {
      if (!dragging.current) return;
      dragging.current = false;
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
    };
  }, []);

  return (
    <div
      className="absolute right-0 top-0 h-full w-2.5 cursor-col-resize z-10 flex items-center justify-end"
      onMouseDown={(e) => {
        e.preventDefault();
        e.stopPropagation();
        dragging.current = true;
        lastX.current = e.clientX;
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
      }}
    >
      <div className="w-px h-3/4 bg-border/60 hover:bg-primary/60 transition-colors" />
    </div>
  );
}
