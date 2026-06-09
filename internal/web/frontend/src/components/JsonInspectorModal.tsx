import { useState, useEffect, useRef } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { CheckCircle2, XCircle, Copy, Minimize2, Braces, Info } from "lucide-react";
import { tryFormatJson, tokenizeJson, type JsonToken } from "@/lib/jsonFormatter";
import { toast } from "sonner";

// --- Syntax highlighting ---

const TOKEN_COLORS: Record<JsonToken["type"], string> = {
  key:         "text-blue-400",
  string:      "text-green-400",
  number:      "text-amber-400",
  boolean:     "text-purple-400",
  null:        "text-gray-400",
  punctuation: "text-gray-300",
  space:       "text-gray-100",
};

// Renderiza uma linha de JSON com syntax highlighting.
// Tokeniza cada linha individualmente para que o alinhamento com os números seja exato.
function HighlightedLine({ line }: { line: string }) {
  if (!line) return <span className="text-gray-100">{" "}</span>;
  const tokens = tokenizeJson(line);
  return (
    <>
      {tokens.map((t, i) => (
        <span key={i} className={TOKEN_COLORS[t.type]}>{t.value}</span>
      ))}
    </>
  );
}

// Painel de JSON válido: cada linha renderizada com número + syntax highlight
function ValidJsonPanel({ json }: { json: string }) {
  const lines = json.split("\n");

  return (
    <div className="font-mono text-xs leading-5 select-text">
      {lines.map((line, i) => (
        <div key={i} className="flex min-h-[1.25rem]">
          {/* Número da linha */}
          <span className="select-none text-right pr-3 border-r border-gray-700/60 mr-3 text-gray-600 flex-shrink-0 w-8">
            {i + 1}
          </span>
          {/* Conteúdo com highlight */}
          <pre className="flex-1 whitespace-pre-wrap break-all m-0 p-0 bg-transparent font-inherit text-inherit">
            <HighlightedLine line={line} />
          </pre>
        </div>
      ))}
    </div>
  );
}

// Painel de JSON inválido: texto bruto linha a linha, erro destacado
function InvalidJsonPanel({ text, errorLine }: { text: string; errorLine?: number }) {
  const lines = text.split("\n");

  return (
    <div className="font-mono text-xs leading-5 select-text">
      {lines.map((line, i) => {
        const isError = errorLine === i + 1;
        return (
          <div
            key={i}
            className={`flex min-h-[1.25rem] ${isError ? "rounded" : ""}`}
          >
            {/* Número da linha */}
            <span
              className={`select-none text-right pr-3 border-r border-gray-700/60 mr-3 flex-shrink-0 w-8 ${
                isError ? "text-red-400 font-bold" : "text-gray-600"
              }`}
            >
              {i + 1}
            </span>
            {/* Conteúdo */}
            <pre
              className={`flex-1 whitespace-pre-wrap break-all m-0 p-0 bg-transparent font-inherit ${
                isError ? "text-red-300 bg-red-900/30" : "text-gray-400"
              }`}
            >
              {line || " "}
            </pre>
          </div>
        );
      })}
    </div>
  );
}

// --- Botão flutuante de seleção ---

interface FloatingButtonProps {
  pos: { x: number; y: number };
  onClick: () => void;
}

export function JsonFloatingButton({ pos, onClick }: FloatingButtonProps) {
  return (
    <div
      style={{ position: "fixed", left: pos.x + 4, top: pos.y, zIndex: 9999 }}
      onMouseDown={(e) => {
        // Impede que o clique limpe a seleção antes de lermos o texto
        e.preventDefault();
        onClick();
      }}
      className="flex items-center gap-1 bg-blue-600 hover:bg-blue-700 text-white text-[10px] font-mono px-2 py-0.5 rounded shadow-lg border border-blue-500 cursor-pointer select-none"
    >
      <Braces className="w-3 h-3" />
      Formatar JSON
    </div>
  );
}

// --- Modal principal ---

interface Props {
  open: boolean;
  onClose: () => void;
  initialText?: string;
}

export function JsonInspectorModal({ open, onClose, initialText = "" }: Props) {
  const [input, setInput] = useState(initialText);
  const result = tryFormatJson(input);

  // Resize state — mesmo padrão do PodQuickViewModal
  const [modalSize, setModalSize] = useState({ width: 960, height: 600 });
  const resizing = useRef(false);
  const resizeDir = useRef<"se" | "e" | "s">("se");
  const lastResizePos = useRef({ x: 0, y: 0 });

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      if (!resizing.current) return;
      const dx = e.clientX - lastResizePos.current.x;
      const dy = e.clientY - lastResizePos.current.y;
      lastResizePos.current = { x: e.clientX, y: e.clientY };
      setModalSize(prev => ({
        width:  resizeDir.current !== "s" ? Math.max(600, prev.width  + dx) : prev.width,
        height: resizeDir.current !== "e" ? Math.max(400, prev.height + dy) : prev.height,
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
    return () => { window.removeEventListener("mousemove", onMove); window.removeEventListener("mouseup", onUp); };
  }, []);

  useEffect(() => {
    if (open) setInput(initialText);
  }, [open, initialText]);

  const copyFormatted = () => {
    if (!result.valid) return;
    navigator.clipboard
      .writeText(result.formatted)
      .then(() => toast.success("JSON formatado copiado"))
      .catch(() => toast.error("Falha ao copiar"));
  };

  const copyMinified = () => {
    if (!result.valid) return;
    try {
      const minified = JSON.stringify(JSON.parse(result.formatted));
      navigator.clipboard
        .writeText(minified)
        .then(() => toast.success("JSON minificado copiado"))
        .catch(() => toast.error("Falha ao copiar"));
    } catch {
      // já validado acima
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent
        className="flex flex-col gap-3 p-6 overflow-hidden"
        style={{ width: modalSize.width, height: modalSize.height, maxWidth: "96vw", maxHeight: "96vh" }}
      >
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2 text-sm">
            <Braces className="w-4 h-4 text-blue-400" />
            Inspetor JSON
          </DialogTitle>
        </DialogHeader>

        {/* Barra de status */}
        <div className="flex items-center gap-2 flex-shrink-0 flex-wrap">
          {result.valid ? (
            <Badge variant="outline" className="text-green-400 border-green-400/40 gap-1">
              <CheckCircle2 className="w-3 h-3" /> JSON válido
            </Badge>
          ) : (
            <Badge variant="outline" className="text-red-400 border-red-400/40 gap-1">
              <XCircle className="w-3 h-3" /> JSON inválido
            </Badge>
          )}

          {result.valid && (
            <span className="text-[10px] text-muted-foreground font-mono">
              {result.lineCount} linhas · {result.charCount} chars
            </span>
          )}

          {result.wasExtracted && (
            <span className="flex items-center gap-1 text-[10px] text-amber-400/80 font-mono">
              <Info className="w-3 h-3" />
              JSON extraído do texto selecionado
            </span>
          )}

          {!result.valid && result.errorLine && (
            <span className="text-[10px] text-red-400/80 font-mono">
              linha {result.errorLine}, coluna {result.errorCol}
            </span>
          )}

          <div className="flex-1" />
          <Button
            size="sm"
            variant="outline"
            className="h-6 text-xs gap-1"
            onClick={copyFormatted}
            disabled={!result.valid}
          >
            <Copy className="w-3 h-3" /> Copiar formatado
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-6 text-xs gap-1"
            onClick={copyMinified}
            disabled={!result.valid}
          >
            <Minimize2 className="w-3 h-3" /> Copiar minificado
          </Button>
        </div>

        {/* Split view: entrada | saída */}
        <div className="flex-1 flex gap-3 min-h-0">

          {/* Entrada (editável) */}
          <div className="flex-1 flex flex-col min-h-0">
            <div className="text-[10px] text-muted-foreground mb-1 uppercase tracking-wider font-medium">
              Entrada
            </div>
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              className="flex-1 min-h-0 resize-none rounded-md border border-border bg-background p-3 font-mono text-xs leading-5 focus:outline-none focus:border-primary"
              placeholder="Cole ou edite o JSON aqui..."
              spellCheck={false}
            />
            {!result.valid && input.trim() && (
              <div className="mt-1.5 text-[10px] text-red-400 font-mono break-all leading-4">
                ✗ {result.error}
              </div>
            )}
          </div>

          {/* Saída formatada */}
          <div className="flex-1 flex flex-col min-h-0">
            <div className="text-[10px] text-muted-foreground mb-1 uppercase tracking-wider font-medium">
              Formatado
            </div>
            <div className="flex-1 min-h-0 overflow-auto rounded-md border border-border bg-[#0d1117] p-3">
              {result.valid ? (
                <ValidJsonPanel json={result.formatted} />
              ) : input.trim() ? (
                <InvalidJsonPanel text={input.trim()} errorLine={result.errorLine} />
              ) : (
                <span className="text-muted-foreground text-xs font-mono">
                  Cole ou escreva um JSON na entrada...
                </span>
              )}
            </div>
          </div>

        </div>

        {/* Handles de resize — mesmo padrão do PodQuickViewModal */}
        {/* Borda direita */}
        <div
          className="absolute top-0 right-0 w-1.5 h-full cursor-e-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "e";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "e-resize";
            document.body.style.userSelect = "none";
          }}
        />
        {/* Borda inferior */}
        <div
          className="absolute bottom-0 left-0 w-full h-1.5 cursor-s-resize hover:bg-primary/20 transition-colors z-50"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "s";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "s-resize";
            document.body.style.userSelect = "none";
          }}
        />
        {/* Canto inferior direito (se-resize) */}
        <div
          className="absolute bottom-0 right-0 w-4 h-4 cursor-se-resize z-50 flex items-end justify-end pr-0.5 pb-0.5"
          onMouseDown={(e) => {
            e.preventDefault();
            resizing.current = true;
            resizeDir.current = "se";
            lastResizePos.current = { x: e.clientX, y: e.clientY };
            document.body.style.cursor = "se-resize";
            document.body.style.userSelect = "none";
          }}
        >
          <svg width="10" height="10" viewBox="0 0 10 10" className="text-muted-foreground/40 hover:text-primary/60">
            <path d="M9 1 L9 9 L1 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round"/>
            <path d="M9 5 L9 9 L5 9" stroke="currentColor" strokeWidth="1.5" fill="none" strokeLinecap="round"/>
          </svg>
        </div>
      </DialogContent>
    </Dialog>
  );
}
