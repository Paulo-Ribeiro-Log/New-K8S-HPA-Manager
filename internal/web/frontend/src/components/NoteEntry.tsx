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

function firstLine(content: string): string {
  const line = content.split("\n").find((l) => l.trim().length > 0)?.trim() ?? "";
  return line.length > 100 ? line.slice(0, 100) + "…" : line;
}

// Item de nota colapsável (colapsado por padrão) — usado na lista escopada do NotesModal,
// nos resultados de busca cross-cluster/aba e no GeneralNotesWidget. Único lugar que sabe
// renderizar uma nota; evita duplicar a lógica de preview/markdown entre os 3 lugares.
export function NoteEntry({ note, isAuthor, onEdit, onDelete, showScopeBadges, defaultOpen = false }: NoteEntryProps) {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <Collapsible open={open} onOpenChange={setOpen} className="border rounded-md overflow-hidden">
      <CollapsibleTrigger className="w-full flex items-center justify-between gap-2 p-3 text-left hover:bg-muted/50">
        <div className="flex items-center gap-2 min-w-0 flex-1">
          {open ? (
            <ChevronDown className="w-3.5 h-3.5 flex-shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="w-3.5 h-3.5 flex-shrink-0 text-muted-foreground" />
          )}
          <span className="text-xs text-muted-foreground truncate">
            {note.user_email} — {new Date(note.created_at).toLocaleString("pt-BR")}
          </span>
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

      {!open && (
        <p className="px-3 pb-3 -mt-1 text-sm text-muted-foreground truncate">
          {firstLine(note.content) || "(nota vazia)"}
        </p>
      )}

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
