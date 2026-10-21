import type { ReactNode } from "react";
import { Copy } from "lucide-react";
import { copyText } from "@/lib/clipboard";

// Valor copiável individualmente: o texto continua selecionável e um ícone (visível ao passar o
// mouse) copia só este valor. Não dispara o clique da linha.
export function CopyValue({ value, label, className = "", children }: { value: string; label: string; className?: string; children?: ReactNode }) {
  if (!value) return null;
  return (
    <span className="group inline-flex items-center gap-1 min-w-0">
      <span className={`select-text ${className}`}>{children ?? value}</span>
      <button
        type="button"
        title={`Copiar ${label}`}
        aria-label={`Copiar ${label}`}
        onClick={(e) => {
          e.stopPropagation();
          copyText(value, `${label} (${value})`);
        }}
        className="shrink-0 opacity-0 group-hover:opacity-100 focus:opacity-100 text-muted-foreground hover:text-foreground"
      >
        <Copy className="w-3 h-3" />
      </button>
    </span>
  );
}
