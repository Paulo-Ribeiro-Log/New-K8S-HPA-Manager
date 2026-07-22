import type { RefObject } from "react";
import { Bold, Italic, List, ListOrdered, Link2, Code, Quote } from "lucide-react";
import { Button } from "@/components/ui/button";

interface FormatAction {
  icon: React.ReactNode;
  title: string;
  wrap: (selected: string) => string;
}

const ACTIONS: FormatAction[] = [
  { icon: <Bold className="w-3.5 h-3.5" />, title: "Negrito", wrap: (s) => `**${s || "texto"}**` },
  { icon: <Italic className="w-3.5 h-3.5" />, title: "Itálico", wrap: (s) => `*${s || "texto"}*` },
  {
    icon: <List className="w-3.5 h-3.5" />,
    title: "Lista",
    wrap: (s) => (s ? s.split("\n").map((l) => `- ${l}`).join("\n") : "- item"),
  },
  {
    icon: <ListOrdered className="w-3.5 h-3.5" />,
    title: "Lista numerada",
    wrap: (s) => (s ? s.split("\n").map((l, i) => `${i + 1}. ${l}`).join("\n") : "1. item"),
  },
  { icon: <Link2 className="w-3.5 h-3.5" />, title: "Link", wrap: (s) => `[${s || "texto"}](url)` },
  { icon: <Code className="w-3.5 h-3.5" />, title: "Código inline", wrap: (s) => `\`${s || "código"}\`` },
  { icon: <Quote className="w-3.5 h-3.5" />, title: "Citação", wrap: (s) => `> ${s || "citação"}` },
];

interface MarkdownToolbarProps {
  textareaRef: RefObject<HTMLTextAreaElement>;
  value: string;
  onChange: (value: string) => void;
}

/**
 * Toolbar de formatação Markdown sobre um <textarea> puro — envolve a seleção atual
 * com a sintaxe (ou insere um placeholder se nada estiver selecionado) e reposiciona
 * o cursor após o re-render (o textarea é controlado).
 */
export function MarkdownToolbar({ textareaRef, value, onChange }: MarkdownToolbarProps) {
  const apply = (action: FormatAction) => {
    const ta = textareaRef.current;
    if (!ta) return;

    const start = ta.selectionStart;
    const end = ta.selectionEnd;
    const selected = value.slice(start, end);
    const replacement = action.wrap(selected);
    const next = value.slice(0, start) + replacement + value.slice(end);

    onChange(next);

    requestAnimationFrame(() => {
      ta.focus();
      const pos = start + replacement.length;
      ta.setSelectionRange(pos, pos);
    });
  };

  return (
    <div className="flex items-center gap-1">
      {ACTIONS.map((action) => (
        <Button
          key={action.title}
          type="button"
          variant="ghost"
          size="sm"
          title={action.title}
          onClick={() => apply(action)}
        >
          {action.icon}
        </Button>
      ))}
    </div>
  );
}
