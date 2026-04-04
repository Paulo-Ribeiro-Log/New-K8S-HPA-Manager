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

/** Alça de redimensionamento — coloque dentro de um container `relative overflow-hidden pr-3` */
export function ResizeHandle({ onResize }: { onResize: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastX = useRef(0);
  const cb = useRef(onResize);
  const [isDragging, setIsDragging] = useState(false);

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
      setIsDragging(false);
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
      className="absolute right-0 top-0 h-full w-4 cursor-col-resize z-10 flex items-center justify-center group select-none"
      onMouseDown={(e) => {
        e.preventDefault();
        e.stopPropagation();
        dragging.current = true;
        setIsDragging(true);
        lastX.current = e.clientX;
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
      }}
    >
      {/* Linha separadora + grip */}
      {isDragging ? (
        /* Linha cheia durante o drag */
        <div className="w-0.5 h-full bg-primary rounded-full" />
      ) : (
        /* Grip de 3 pontos no estado normal/hover */
        <div className="flex flex-col items-center justify-center gap-0.5 h-full py-1">
          {/* Linha separadora sutil à esquerda */}
          <div className="absolute left-1 top-0 bottom-0 w-px bg-border/40 group-hover:bg-primary/30 transition-colors" />
          {/* Três pontos de grip */}
          <span className="w-0.5 h-0.5 rounded-full bg-muted-foreground/30 group-hover:bg-primary/70 transition-colors" />
          <span className="w-0.5 h-0.5 rounded-full bg-muted-foreground/30 group-hover:bg-primary/70 transition-colors" />
          <span className="w-0.5 h-0.5 rounded-full bg-muted-foreground/30 group-hover:bg-primary/70 transition-colors" />
        </div>
      )}
    </div>
  );
}
