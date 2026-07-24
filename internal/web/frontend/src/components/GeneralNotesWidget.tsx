import { useState } from "react";
import { StickyNote, X } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { NoteEntry } from "@/components/NoteEntry";
import { NotesModal } from "@/components/NotesModal";
import { GENERAL_NOTES_TAB, useNotes } from "@/hooks/useNotes";

interface GeneralNotesWidgetProps {
  cluster: string;
}

// Post-it flutuante com lembretes "gerais" do cluster (não escopados por aba) — visível
// em qualquer aba, ao contrário do botão "Notas" da TabNavigation (escopado à aba ativa).
// Colapsado por padrão (só o pill com contagem); expande num painel com preview das notas,
// cada uma também colapsada por padrão (NoteEntry). CRUD de fato acontece no NotesModal
// reaproveitado abaixo, travado em tab=GENERAL_NOTES_TAB — sem duplicar o editor Markdown.
export function GeneralNotesWidget({ cluster }: GeneralNotesWidgetProps) {
  const [panelOpen, setPanelOpen] = useState(false);
  const [showModal, setShowModal] = useState(false);
  const { data: notes = [] } = useNotes(cluster, GENERAL_NOTES_TAB);

  if (!cluster) return null;

  return (
    <>
      <div className="fixed bottom-4 left-4 z-40">
        {!panelOpen ? (
          <button
            onClick={() => setPanelOpen(true)}
            className="flex items-center gap-1.5 px-3 py-2 rounded-full bg-amber-400 dark:bg-amber-500 text-amber-950 shadow-lg hover:shadow-xl transition-shadow font-medium text-sm"
            title="Lembretes gerais (visíveis em qualquer aba)"
          >
            <StickyNote className="w-4 h-4" />
            Lembretes
            {notes.length > 0 && (
              <Badge variant="secondary" className="h-5 min-w-5 px-1.5 text-xs bg-amber-950/10 text-amber-950 hover:bg-amber-950/10">
                {notes.length}
              </Badge>
            )}
          </button>
        ) : (
          <div className="w-80 max-h-[70vh] flex flex-col bg-card border rounded-lg shadow-2xl overflow-hidden">
            <div className="flex items-center justify-between px-3 py-2 border-b bg-amber-400/20 dark:bg-amber-500/10 flex-shrink-0">
              <div className="flex items-center gap-1.5 text-sm font-medium">
                <StickyNote className="w-4 h-4" />
                Lembretes gerais
              </div>
              <button onClick={() => setPanelOpen(false)} className="text-muted-foreground hover:text-foreground" title="Fechar">
                <X className="w-4 h-4" />
              </button>
            </div>

            <div className="flex-1 min-h-0 overflow-y-auto p-2 flex flex-col gap-2">
              {notes.length === 0 && (
                <p className="text-sm text-muted-foreground p-2">Nenhum lembrete geral ainda neste cluster.</p>
              )}
              {notes.map((note) => (
                <NoteEntry key={note.id} note={note} defaultOpen={false} />
              ))}
            </div>

            <div className="p-2 border-t flex-shrink-0">
              <button
                onClick={() => setShowModal(true)}
                className="w-full text-sm py-1.5 rounded-md bg-primary text-primary-foreground hover:opacity-90 transition-opacity"
              >
                Gerenciar lembretes
              </button>
            </div>
          </div>
        )}
      </div>

      <NotesModal open={showModal} onOpenChange={setShowModal} cluster={cluster} tab={GENERAL_NOTES_TAB} />
    </>
  );
}
