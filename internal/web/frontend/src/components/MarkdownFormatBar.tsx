import { Bold, CornerDownLeft, Code, Code2, Heading1, Heading2, Heading3, Italic, Link2, List, ListChecks, ListOrdered, Minus, Quote, SquareCheck, Strikethrough, Table2, WrapText } from "lucide-react";
import type { ReactNode } from "react";
import type { FormatAction } from "@/lib/markdownFormat";

const COLORS: { c: string; label: string }[] = [
  { c: "#ef4444", label: "Vermelho" }, { c: "#f59e0b", label: "Laranja" }, { c: "#22c55e", label: "Verde" },
  { c: "#3b82f6", label: "Azul" }, { c: "#a855f7", label: "Roxo" }, { c: "#94a3b8", label: "Cinza" },
];

const GROUPS: { a: FormatAction; t: string; i: ReactNode }[][] = [
  [
    { a: "bold", t: "Negrito (**texto**)", i: <Bold className="w-3.5 h-3.5" /> },
    { a: "italic", t: "Itálico (*texto*)", i: <Italic className="w-3.5 h-3.5" /> },
    { a: "strike", t: "Riscado (~~texto~~)", i: <Strikethrough className="w-3.5 h-3.5" /> },
    { a: "code", t: "Código em linha (`texto`)", i: <Code className="w-3.5 h-3.5" /> },
  ],
  [
    { a: "h1", t: "Título 1", i: <Heading1 className="w-3.5 h-3.5" /> },
    { a: "h2", t: "Título 2", i: <Heading2 className="w-3.5 h-3.5" /> },
    { a: "h3", t: "Título 3", i: <Heading3 className="w-3.5 h-3.5" /> },
  ],
  [
    { a: "ul", t: "Lista com marcadores", i: <List className="w-3.5 h-3.5" /> },
    { a: "ol", t: "Lista numerada", i: <ListOrdered className="w-3.5 h-3.5" /> },
    { a: "task", t: "Lista de tarefas (- [ ])", i: <ListChecks className="w-3.5 h-3.5" /> },
    { a: "quote", t: "Citação", i: <Quote className="w-3.5 h-3.5" /> },
  ],
  [
    { a: "link", t: "Link", i: <Link2 className="w-3.5 h-3.5" /> },
    { a: "codeblock", t: "Bloco de código", i: <Code2 className="w-3.5 h-3.5" /> },
    { a: "table", t: "Inserir tabela 3×2", i: <Table2 className="w-3.5 h-3.5" /> },
    { a: "hr", t: "Linha horizontal", i: <Minus className="w-3.5 h-3.5" /> },
    { a: "break", t: "Quebra de linha no texto (dois espaços + Enter; atalho Shift+Enter)", i: <CornerDownLeft className="w-3.5 h-3.5" /> },
    { a: "br", t: "Quebra de linha <br> (a que vale dentro de tabela)", i: <WrapText className="w-3.5 h-3.5" /> },
    { a: "check", t: "Caixinha ☐ (alterna ☐/☑ se estiver selecionada)", i: <SquareCheck className="w-3.5 h-3.5" /> },
  ],
];

/** Barra de formatação dos arquivos .md. Age na seleção do editor; os botões não tiram o foco dele. */
export function MarkdownFormatBar({ onAction, softBreaks, onToggleSoftBreaks }: {
  onAction: (a: FormatAction) => void;
  softBreaks: boolean;
  onToggleSoftBreaks: () => void;
}) {
  return (
    <div className="flex flex-shrink-0 flex-wrap items-center gap-x-2 gap-y-0.5 border-b border-border/50 bg-slate-900/60 px-2 py-1" role="toolbar" aria-label="Formatação Markdown">
      {GROUPS.map((g, gi) => (
        <div key={gi} className="flex items-center gap-0.5 border-r border-border/40 pr-2 last:border-r-0">
          {g.map((b) => (
            <button key={b.a} type="button" title={b.t} aria-label={b.t}
              onMouseDown={(e) => e.preventDefault()} onClick={() => onAction(b.a)}
              className="flex h-6 w-6 items-center justify-center rounded text-slate-300 hover:bg-slate-700 hover:text-white">
              {b.i}
            </button>
          ))}
        </div>
      ))}
      <button type="button" onMouseDown={(e) => e.preventDefault()} onClick={onToggleSoftBreaks} aria-pressed={softBreaks}
        title={"Preview: o Enter simples vira quebra de linha (como nos comentários do GitHub). Desligado, segue o Markdown padrão (README no GitHub junta as linhas). Só afeta o preview, não o arquivo."}
        className={`h-6 rounded px-2 text-[11px] ${softBreaks ? "bg-sky-600/40 text-sky-100" : "text-slate-400 hover:bg-slate-700"}`}>
        Enter = quebra
      </button>
      <div className="flex items-center gap-1" title="Cor do texto selecionado (<span style=color>)">
        <span className="text-[10px] text-muted-foreground">Cor</span>
        {COLORS.map((c) => (
          <button key={c.c} type="button" title={`Texto ${c.label.toLowerCase()}`} aria-label={`Texto ${c.label.toLowerCase()}`}
            onMouseDown={(e) => e.preventDefault()} onClick={() => onAction(`color:${c.c}`)}
            className="h-3.5 w-3.5 rounded-full border border-white/20 hover:scale-125" style={{ backgroundColor: c.c }} />
        ))}
      </div>
    </div>
  );
}
