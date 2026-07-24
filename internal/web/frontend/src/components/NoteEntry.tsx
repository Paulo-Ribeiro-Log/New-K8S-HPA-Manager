import { useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { ChevronDown, ChevronRight, Pencil, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { GENERAL_NOTES_TAB } from "@/hooks/useNotes";
import type { Note } from "@/lib/api/types";

interface NoteEntryProps {
  note: Note;
  isAuthor?: boolean;
  onEdit?: () => void;
  onDelete?: () => void;
  showScopeBadges?: boolean;
  defaultOpen?: boolean;
}

// Deriva um "título" a partir da 1ª linha não-vazia do conteúdo — não há campo Title
// próprio (o modelo de dados continua só Markdown livre, sem exigir preencher um campo
// extra ao salvar). Remove marcadores de Markdown (#, -, *, >, "1.") pra não vazar sintaxe
// crua no título exibido.
function deriveTitle(content: string): string {
  const line = content.split("\n").find((l) => l.trim().length > 0)?.trim() ?? "";
  const stripped = line
    .replace(/^#{1,6}\s+/, "")
    .replace(/^[-*>]\s+/, "")
    .replace(/^\d+\.\s+/, "");
  const title = stripped || "(nota sem título)";
  return title.length > 70 ? title.slice(0, 70) + "…" : title;
}

// Item de nota colapsável (colapsado por padrão) — usado na lista escopada do NotesModal,
// nos resultados de busca cross-cluster/aba e no GeneralNotesWidget. Único lugar que sabe
// renderizar uma nota; evita duplicar a lógica de preview/markdown entre os 3 lugares.
export function NoteEntry({ note, isAuthor, onEdit, onDelete, showScopeBadges, defaultOpen = false }: NoteEntryProps) {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <Collapsible open={open} onOpenChange={setOpen} className="border rounded-md overflow-hidden">
      <CollapsibleTrigger
        className="w-full flex items-center justify-between gap-2 p-3 text-left hover:bg-muted/50"
        title={`Nota de ${note.user_email}`}
      >
        <div className="flex items-center gap-2 min-w-0 flex-1">
          {open ? (
            <ChevronDown className="w-3.5 h-3.5 flex-shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="w-3.5 h-3.5 flex-shrink-0 text-muted-foreground" />
          )}
          <div className="min-w-0 flex-1">
            <p className="text-sm font-medium truncate">{deriveTitle(note.content)}</p>
            <p className="text-xs text-muted-foreground">{new Date(note.created_at).toLocaleString("pt-BR")}</p>
          </div>
        </div>
        {showScopeBadges && (
          <div className="flex items-center gap-1 flex-shrink-0">
            <Badge variant="outline" className="text-[10px] font-normal">{note.cluster}</Badge>
            <Badge variant="outline" className="text-[10px] font-normal">
              {note.tab === GENERAL_NOTES_TAB ? "geral" : note.tab}
            </Badge>
          </div>
        )}
      </CollapsibleTrigger>

      <CollapsibleContent className="px-3 pb-3">
        {isAuthor && (onEdit || onDelete) && (
          <div className="flex justify-end gap-1 mb-2">
            {onEdit && (
              <Button variant="ghost" size="sm" onClick={onEdit} title="Editar">
                <Pencil className="w-3.5 h-3.5" />
              </Button>
            )}
            {onDelete && (
              <Button variant="ghost" size="sm" onClick={onDelete} title="Excluir">
                <Trash2 className="w-3.5 h-3.5" />
              </Button>
            )}
          </div>
        )}
        <div className="prose prose-sm dark:prose-invert max-w-none">
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{note.content}</ReactMarkdown>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
