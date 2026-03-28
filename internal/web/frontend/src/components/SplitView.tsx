import { ReactNode, useEffect, useRef, useState } from "react";
import { Card } from "@/components/ui/card";

interface SplitViewProps {
  leftPanel: {
    title: string;
    titlePrefix?: ReactNode;
    titleAction?: ReactNode;
    content: ReactNode;
  };
  rightPanel: {
    title: string;
    titlePrefix?: ReactNode;
    titleAction?: ReactNode;
    content: ReactNode;
  };
}

function ResizeDivider({ onDrag }: { onDrag: (delta: number) => void }) {
  const dragging = useRef(false);
  const lastX = useRef(0);

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!dragging.current) return;
      onDrag(e.clientX - lastX.current);
      lastX.current = e.clientX;
    };
    const onUp = () => {
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
  }, [onDrag]);

  return (
    <div
      className="w-1 flex-shrink-0 bg-border/40 hover:bg-primary/60 active:bg-primary cursor-col-resize transition-colors"
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
  const [leftWidth, setLeftWidth] = useState(320);

  return (
    <div className="flex flex-row p-4 h-full gap-0 overflow-hidden">
      {/* Painel esquerdo */}
      <Card
        className="p-4 bg-gradient-card border-border/50 flex flex-col min-h-0 min-w-0 flex-shrink-0"
        style={{ width: leftWidth }}
      >
        <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 mb-3 pb-2 border-b-2 border-primary flex-shrink-0">
          <div className="flex items-center gap-2 shrink-0">
            {leftPanel.titlePrefix}
            <h3 className="text-sm font-semibold text-primary whitespace-nowrap">
              {leftPanel.title}
            </h3>
          </div>
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

      <ResizeDivider onDrag={(d) => setLeftWidth((w) => Math.max(200, Math.min(700, w + d)))} />

      {/* Painel direito */}
      <Card
        className="flex-1 p-4 bg-gradient-card border-border/50 flex flex-col min-h-0 min-w-0 ml-4"
      >
        <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 mb-3 pb-2 border-b-2 border-primary flex-shrink-0">
          <div className="flex items-center gap-2 shrink-0">
            {rightPanel.titlePrefix}
            <h3 className="text-sm font-semibold text-primary whitespace-nowrap">
              {rightPanel.title}
            </h3>
          </div>
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
