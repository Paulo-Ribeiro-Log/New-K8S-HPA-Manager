import { useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { toast } from "sonner";
import { Eye, Pencil, Plus, Save, Search, StickyNote, X } from "lucide-react";

import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { MarkdownToolbar } from "@/components/MarkdownToolbar";
import { NoteEntry } from "@/components/NoteEntry";
import { GENERAL_NOTES_TAB, useCreateNote, useDeleteNote, useNotes, useSearchNotes, useUpdateNote } from "@/hooks/useNotes";
import { useUserProfile } from "@/hooks/useUserProfile";
import type { Note } from "@/lib/api/types";

interface NotesModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  tab: string;
}

export function NotesModal({ open, onOpenChange, cluster, tab }: NotesModalProps) {
  const { user } = useUserProfile();

  // Escopo "Aba atual" x "Lembretes gerais" — as duas contagens ficam sempre disponíveis
  // (react-query cacheia por chave, então trocar de escopo não refaz a request do que já
  // foi buscado) pra mostrar o badge nos dois botões do seletor sem custo extra.
  const [scope, setScope] = useState<"tab" | "general">("tab");
  const effectiveTab = scope === "general" ? GENERAL_NOTES_TAB : tab;
  const { data: tabNotes = [], isLoading: tabLoading } = useNotes(cluster, tab);
  const { data: generalNotes = [], isLoading: generalLoading } = useNotes(cluster, GENERAL_NOTES_TAB);
  const notes = scope === "general" ? generalNotes : tabNotes;
  const isLoading = scope === "general" ? generalLoading : tabLoading;

  const createNote = useCreateNote(cluster, effectiveTab);
  const updateNote = useUpdateNote(cluster, effectiveTab);
  const deleteNote = useDeleteNote(cluster, effectiveTab);

  const [composing, setComposing] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [draft, setDraft] = useState("");
  const [preview, setPreview] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const [searchQuery, setSearchQuery] = useState("");
  const isSearching = searchQuery.trim().length >= 2;
  const { data: searchResults = [], isFetching: searchLoading } = useSearchNotes(searchQuery);

  const cancelCompose = () => {
    setComposing(false);
    setEditingId(null);
    setDraft("");
    setPreview(false);
  };

  const switchScope = (next: "tab" | "general") => {
    if (next === scope) return;
    setScope(next);
    cancelCompose();
  };

  const startNew = () => {
    setComposing(true);
    setEditingId(null);
    setDraft("");
    setPreview(false);
  };

  const startEdit = (note: Note) => {
    setComposing(true);
    setEditingId(note.id);
    setDraft(note.content);
    setPreview(false);
  };

  const handleSave = async () => {
    if (!draft.trim()) return;
    try {
      if (editingId != null) {
        await updateNote.mutateAsync({ id: editingId, content: draft });
        toast.success("Nota atualizada");
      } else {
        await createNote.mutateAsync(draft);
        toast.success("Nota salva");
      }
      cancelCompose();
    } catch {
      toast.error("Falha ao salvar nota");
    }
  };

  const handleDelete = async (id: number) => {
    try {
      await deleteNote.mutateAsync(id);
      toast.success("Nota excluída");
    } catch {
      toast.error("Falha ao excluir nota");
    }
  };

  const saving = createNote.isPending || updateNote.isPending;
  const scopeLabel = scope === "general" ? "Lembretes gerais (todas as abas)" : tab;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="h-[85vh] max-w-2xl flex flex-col overflow-hidden">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <StickyNote className="w-4 h-4" />
            Notas — {cluster || "sem cluster"} / {scopeLabel}
          </DialogTitle>
        </DialogHeader>

        {!cluster ? (
          <div className="flex-1 min-h-0 flex items-center justify-center text-sm text-muted-foreground">
            Selecione um cluster para ver e criar notas.
          </div>
        ) : (
          <div className="flex-1 min-h-0 flex flex-col gap-3">
            <div className="flex gap-1.5 flex-shrink-0">
              <button
                onClick={() => switchScope("tab")}
                className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border transition-colors ${
                  scope === "tab"
                    ? "bg-primary text-primary-foreground border-primary"
                    : "text-muted-foreground border-border hover:bg-muted"
                }`}
              >
                Aba atual ({tab})
                {tabNotes.length > 0 && (
                  <Badge
                    variant="secondary"
                    className={`h-4 min-w-4 px-1 text-[10px] ${scope === "tab" ? "bg-primary-foreground/20 text-primary-foreground hover:bg-primary-foreground/20" : ""}`}
                  >
                    {tabNotes.length}
                  </Badge>
                )}
              </button>
              <button
                onClick={() => switchScope("general")}
                className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium border transition-colors ${
                  scope === "general"
                    ? "bg-amber-400 text-amber-950 border-amber-400 dark:bg-amber-500 dark:text-amber-950 dark:border-amber-500"
                    : "text-muted-foreground border-border hover:bg-muted"
                }`}
              >
                <StickyNote className="w-3.5 h-3.5" />
                Lembretes gerais
                {generalNotes.length > 0 && (
                  <Badge
                    variant="secondary"
                    className={`h-4 min-w-4 px-1 text-[10px] ${scope === "general" ? "bg-amber-950/15 text-amber-950 hover:bg-amber-950/15" : ""}`}
                  >
                    {generalNotes.length}
                  </Badge>
                )}
              </button>
            </div>

            <div className="relative flex-shrink-0">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
              <Input
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Buscar em todas as notas (todos os clusters/abas)..."
                className="pl-8 pr-8 h-8 text-sm"
              />
              {searchQuery && (
                <button
                  onClick={() => setSearchQuery("")}
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                  title="Limpar busca"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              )}
            </div>

            {isSearching ? (
              <ScrollArea className="flex-1 min-h-0">
                <div className="flex flex-col gap-3 pr-3">
                  {searchLoading && <p className="text-sm text-muted-foreground">Buscando...</p>}
                  {!searchLoading && searchResults.length === 0 && (
                    <p className="text-sm text-muted-foreground">Nenhuma nota encontrada.</p>
                  )}
                  {searchResults.map((note) => (
                    <NoteEntry key={note.id} note={note} showScopeBadges defaultOpen={false} />
                  ))}
                </div>
              </ScrollArea>
            ) : (
              <>
                {!composing && (
                  <Button size="sm" onClick={startNew} className="self-start">
                    <Plus className="w-4 h-4 mr-1" /> {scope === "general" ? "Novo lembrete" : "Nova nota"}
                  </Button>
                )}

                {composing && (
                  <div className="flex flex-col gap-2 border rounded-md p-3 flex-shrink-0">
                    <div className="flex items-center justify-between">
                      <MarkdownToolbar textareaRef={textareaRef} value={draft} onChange={setDraft} />
                      <Button variant="ghost" size="sm" onClick={() => setPreview((p) => !p)} title={preview ? "Editar" : "Pré-visualizar"}>
                        {preview ? <Pencil className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                      </Button>
                    </div>

                    {preview ? (
                      <div className="prose prose-sm dark:prose-invert max-w-none border rounded p-2 min-h-[150px]">
                        <ReactMarkdown remarkPlugins={[remarkGfm]}>
                          {draft || "*nada para pré-visualizar*"}
                        </ReactMarkdown>
                      </div>
                    ) : (
                      <Textarea
                        ref={textareaRef}
                        value={draft}
                        onChange={(e) => setDraft(e.target.value)}
                        className="min-h-[150px] font-mono text-sm"
                        placeholder="Escreva em Markdown..."
                      />
                    )}

                    <div className="flex justify-end gap-2">
                      <Button variant="outline" size="sm" onClick={cancelCompose}>
                        <X className="w-4 h-4 mr-1" /> Cancelar
                      </Button>
                      <Button size="sm" onClick={handleSave} disabled={saving || !draft.trim()}>
                        <Save className="w-4 h-4 mr-1" /> Salvar
                      </Button>
                    </div>
                  </div>
                )}

                <ScrollArea className="flex-1 min-h-0">
                  <div className="flex flex-col gap-3 pr-3">
                    {isLoading && <p className="text-sm text-muted-foreground">Carregando...</p>}
                    {!isLoading && notes.length === 0 && (
                      <p className="text-sm text-muted-foreground">
                        {scope === "general"
                          ? "Nenhum lembrete geral ainda neste cluster."
                          : "Nenhuma nota ainda neste cluster/aba."}
                      </p>
                    )}
                    {notes.map((note) => {
                      const isAuthor = !!user?.email && note.user_email === user.email;
                      return (
                        <NoteEntry
                          key={note.id}
                          note={note}
                          isAuthor={isAuthor}
                          onEdit={() => startEdit(note)}
                          onDelete={() => handleDelete(note.id)}
                          defaultOpen={false}
                        />
                      );
                    })}
                  </div>
                </ScrollArea>
              </>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
