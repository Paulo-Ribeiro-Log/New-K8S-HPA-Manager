import { useEffect, useRef, useState } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Copy, Check, FileWarning } from "lucide-react";

interface SpinnakerStageLogModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  stageName: string;
  log: string;
}

// Modal redimensionável (mesmo padrão de resize de PodQuickViewModal.tsx — bordas direita/
// inferior + canto, `modalSize` em vez de max-w fixo) pra mostrar o texto de falha reconstruído
// de um stage do Spinnaker (Stage.FailureLog no backend). Gatilho: botão "Ver log" na tabela de
// etapas do SpinnakerRolloutModal, só em etapas com log não-vazio (== etapas que falharam de
// verdade). Modal próprio, não reaproveita LogsViewer (esse é pra logs de pod via kubectl, texto
// aqui já vem pronto do backend — não há stream nem filtro por container).
export function SpinnakerStageLogModal({ open, onOpenChange, stageName, log }: SpinnakerStageLogModalProps) {
  const [modalSize, setModalSize] = useState({ width: 820, height: 560 });
  const [copied, setCopied] = useState(false);
  const resizing = useRef(false);
  const resizeDir = useRef<"se" | "e" | "s">("se");
  const lastResizePos = useRef({ x: 0, y: 0 });

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!resizing.current) return;
      const dx = e.clientX - lastResizePos.current.x;
      const dy = e.clientY - lastResizePos.current.y;
      lastResizePos.current = { x: e.clientX, y: e.clientY };
      setModalSize((prev) => ({
        width: resizeDir.current !== "s" ? Math.max(480, prev.width + dx) : prev.width,
        height: resizeDir.current !== "e" ? Math.max(320, prev.height + dy) : prev.height,
      }));
    };
    const onUp = () => {
      if (!resizing.current) return;
      resizing.current = false;
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

  const startResize = (dir: "se" | "e" | "s") => (e: React.MouseEvent) => {
    e.preventDefault();
    resizing.current = true;
    resizeDir.current = dir;
    lastResizePos.current = { x: e.clientX, y: e.clientY };
    document.body.style.cursor = `${dir}-resize`;
    document.body.style.userSelect = "none";
  };

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(log);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard indisponível (ex: contexto não-seguro) — sem feedback de erro, só não copia
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="flex flex-col p-0 gap-0 overflow-hidden"
        style={{ width: modalSize.width, height: modalSize.height, maxWidth: "96vw", maxHeight: "96vh" }}
      >
        <DialogHeader className="px-4 pt-4 pb-3 border-b border-border flex-shrink-0">
          <DialogTitle className="flex items-center gap-2 font-mono text-sm">
            <FileWarning className="h-4 w-4 text-amber-500 shrink-0" />
            {stageName}
          </DialogTitle>
        </DialogHeader>

        <div className="flex-1 min-h-0 overflow-auto p-4">
          <pre className="text-xs font-mono whitespace-pre-wrap break-words text-foreground">{log}</pre>
        </div>

        <div className="flex items-center justify-end gap-2 px-4 py-2.5 border-t border-border flex-shrink-0">
          <Button variant="outline" size="sm" onClick={handleCopy}>
            {copied ? <Check className="h-3.5 w-3.5 mr-1.5" /> : <Copy className="h-3.5 w-3.5 mr-1.5" />}
            {copied ? "Copiado" : "Copiar"}
          </Button>
        </div>

        {/* Handles de resize — mesmo padrão de PodQuickViewModal.tsx */}
        <div
          className="absolute top-0 right-0 w-1.5 h-full cursor-e-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={startResize("e")}
        />
        <div
          className="absolute bottom-0 left-0 w-full h-1.5 cursor-s-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={startResize("s")}
        />
        <div
          className="absolute bottom-0 right-0 w-4 h-4 cursor-se-resize z-50 flex items-end justify-end pr-0.5 pb-0.5"
          onMouseDown={startResize("se")}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" className="text-muted-foreground/40 hover:text-primary/60">
            <path d="M9 1 L9 9 L1 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round" />
            <path d="M9 5 L9 9 L5 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round" />
          </svg>
        </div>
      </DialogContent>
    </Dialog>
  );
}
