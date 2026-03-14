import { ReactNode, useEffect, useRef, useState, useCallback } from "react";
import { Card } from "@/components/ui/card";

interface SplitViewProps {
  leftPanel: {
    title: string;
    titleAction?: ReactNode;
    content: ReactNode;
  };
  rightPanel: {
    title: string;
    titleAction?: ReactNode;
    content: ReactNode;
  };
}

function ResizeDivider({ onDrag }: { onDrag: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastX = useRef(0);
  const onDragRef = useRef(onDrag);
  useEffect(() => { onDragRef.current = onDrag; }, [onDrag]);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      onDragRef.current(e.clientX - lastX.current);
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
      className="w-1 flex-shrink-0 bg-border/40 hover:bg-primary/60 active:bg-primary cursor-col-resize transition-colors self-stretch"
      onMouseDown={(e) => {
        dragging.current = true;
        lastX.current = e.clientX;
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
        e.preventDefault();
      }}
    />
  );
}

export const SplitView = ({ leftPanel, rightPanel }: SplitViewProps) => {
  const containerRef = useRef<HTMLDivElement>(null);
  // 25% esquerda por padrão (equivalente ao 1:3 anterior)
  const [leftPct, setLeftPct] = useState(25);

  const handleDrag = useCallback((delta: number) => {
    const containerW = containerRef.current?.offsetWidth ?? 800;
    setLeftPct((prev) => {
      const newPct = prev + (delta / containerW) * 100;
      return Math.max(15, Math.min(70, newPct));
    });
  }, []);

  return (
    <div ref={containerRef} className="flex flex-row gap-0 p-4 h-full" style={{ gap: 0 }}>
      {/* Painel esquerdo */}
      <Card
        className="p-4 bg-gradient-card border-border/50 flex flex-col min-h-0 min-w-0"
        style={{ width: `${leftPct}%`, flexShrink: 0 }}
      >
        <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 mb-3 pb-2 border-b-2 border-primary flex-shrink-0">
          <h3 className="text-sm font-semibold text-primary shrink-0 whitespace-nowrap">
            {leftPanel.title}
          </h3>
          {leftPanel.titleAction && (
            <div className="flex items-center gap-1 flex-wrap min-w-0">
              {leftPanel.titleAction}
            </div>
          )}
        </div>
        <div className="flex-1 overflow-auto min-h-0">
          {leftPanel.content}
        </div>
      </Card>

      <ResizeDivider onDrag={handleDrag} />

      {/* Painel direito */}
      <Card
        className="p-4 bg-gradient-card border-border/50 flex flex-col min-h-0 min-w-0 ml-4"
        style={{ flex: 1 }}
      >
        <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 mb-3 pb-2 border-b-2 border-primary flex-shrink-0">
          <h3 className="text-sm font-semibold text-primary shrink-0 whitespace-nowrap">
            {rightPanel.title}
          </h3>
          {rightPanel.titleAction && (
            <div className="flex items-center gap-1 flex-wrap min-w-0">
              {rightPanel.titleAction}
            </div>
          )}
        </div>
        <div className="flex-1 overflow-auto min-h-0">
          {rightPanel.content}
        </div>
      </Card>
    </div>
  );
};
