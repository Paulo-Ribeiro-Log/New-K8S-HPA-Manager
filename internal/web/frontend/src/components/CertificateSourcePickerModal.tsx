import { useEffect, useMemo, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Loader2, History, FolderOpen, Folder, ArrowLeft, ChevronRight, Search, Pencil, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useCertificates } from "@/hooks/useCertificates";
import type { RollbackBackupInfo, ManualBackupInfo } from "@/types/certificates";

interface CertificateSourcePickerModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Cluster/namespace/secretName do certificado sendo instalado/atualizado agora — escopo da aba
   *  "Backup (Rollback)", que só lista backups DESSE secret (mesmo escopo do RollbackStore). */
  cluster: string;
  namespace: string;
  secretName: string;
  /** Chamado quando o usuário escolhe um backup — devolve o PEM bruto pra popular os campos de
   *  texto do fluxo manual, exatamente como se tivesse colado à mão. */
  onSelect: (tlsCrt: string, tlsKey: string) => void;
}

type Tab = "rollback" | "manual";

/**
 * CertificateSourcePickerModal — "escolher instalar do backup/rollback ou do backup apartado,
 * podendo navegar entre as pastas". Duas abas: Rollback (escopado ao secret atual, como o resto do
 * app já faz) e Backup Apartado (navegável entre QUALQUER secret que já tenha um backup manual —
 * útil pra reinstalar um cert de outro secret/cluster, não só o atual).
 */
export function CertificateSourcePickerModal({
  open,
  onOpenChange,
  cluster,
  namespace,
  secretName,
  onSelect,
}: CertificateSourcePickerModalProps) {
  const {
    listRollbacks,
    getRollbackContent,
    listManualBackupSecrets,
    listManualBackups,
    getManualBackupContent,
    updateManualBackupComment,
    deleteManualBackup,
  } = useCertificates();

  const [tab, setTab] = useState<Tab>("rollback");

  const [rollbacks, setRollbacks] = useState<RollbackBackupInfo[]>([]);
  const [loadingRollbacks, setLoadingRollbacks] = useState(false);

  const [manualSecrets, setManualSecrets] = useState<string[]>([]);
  const [loadingManualSecrets, setLoadingManualSecrets] = useState(false);
  const [selectedManualSecret, setSelectedManualSecret] = useState<string | null>(null);
  const [manualBackups, setManualBackups] = useState<ManualBackupInfo[]>([]);
  const [loadingManualBackups, setLoadingManualBackups] = useState(false);

  const [applying, setApplying] = useState<string | null>(null); // backup_id em aplicação, pro spinner do botão

  // Edição de nota (comentário) de um backup apartado — inline, uma linha por vez.
  const [editingBackupId, setEditingBackupId] = useState<string | null>(null);
  const [editCommentText, setEditCommentText] = useState("");
  const [savingComment, setSavingComment] = useState(false);

  // Exclusão de um backup apartado — ação destrutiva, sempre atrás de confirmação.
  const [backupToDelete, setBackupToDelete] = useState<ManualBackupInfo | null>(null);
  const [deletingBackup, setDeletingBackup] = useState(false);

  // Busca client-side por lista — nomes/subjects/comentários, não exige recarregar do servidor.
  const [rollbackSearch, setRollbackSearch] = useState("");
  const [manualSecretSearch, setManualSecretSearch] = useState("");
  const [manualBackupSearch, setManualBackupSearch] = useState("");

  const filteredRollbacks = useMemo(() => {
    const q = rollbackSearch.trim().toLowerCase();
    if (!q) return rollbacks;
    return rollbacks.filter(
      (b) => b.subject.toLowerCase().includes(q) || new Date(b.backed_up_at).toLocaleString("pt-BR").toLowerCase().includes(q)
    );
  }, [rollbacks, rollbackSearch]);

  const filteredManualSecrets = useMemo(() => {
    const q = manualSecretSearch.trim().toLowerCase();
    if (!q) return manualSecrets;
    return manualSecrets.filter((name) => name.toLowerCase().includes(q));
  }, [manualSecrets, manualSecretSearch]);

  const filteredManualBackups = useMemo(() => {
    const q = manualBackupSearch.trim().toLowerCase();
    if (!q) return manualBackups;
    return manualBackups.filter(
      (b) =>
        b.subject.toLowerCase().includes(q) ||
        (b.comment ?? "").toLowerCase().includes(q) ||
        new Date(b.backed_up_at).toLocaleString("pt-BR").toLowerCase().includes(q)
    );
  }, [manualBackups, manualBackupSearch]);

  useEffect(() => {
    if (!open) {
      setTab("rollback");
      setSelectedManualSecret(null);
      setRollbackSearch("");
      setManualSecretSearch("");
      setManualBackupSearch("");
      setEditingBackupId(null);
      setBackupToDelete(null);
      return;
    }
    setLoadingRollbacks(true);
    listRollbacks(cluster, namespace, secretName)
      .then(setRollbacks)
      .catch(() => setRollbacks([]))
      .finally(() => setLoadingRollbacks(false));

    setLoadingManualSecrets(true);
    listManualBackupSecrets()
      .then(setManualSecrets)
      .catch(() => setManualSecrets([]))
      .finally(() => setLoadingManualSecrets(false));
  }, [open, cluster, namespace, secretName, listRollbacks, listManualBackupSecrets]);

  const openManualSecretFolder = (name: string) => {
    setSelectedManualSecret(name);
    setManualBackupSearch("");
    setEditingBackupId(null);
    setLoadingManualBackups(true);
    listManualBackups(name)
      .then(setManualBackups)
      .catch(() => setManualBackups([]))
      .finally(() => setLoadingManualBackups(false));
  };

  const handleStartEditComment = (b: ManualBackupInfo) => {
    setEditingBackupId(b.backup_id);
    setEditCommentText(b.comment ?? "");
  };

  const handleSaveComment = async (backupId: string) => {
    if (!selectedManualSecret) return;
    setSavingComment(true);
    try {
      await updateManualBackupComment(selectedManualSecret, backupId, editCommentText);
      setManualBackups((prev) =>
        prev.map((x) => (x.backup_id === backupId ? { ...x, comment: editCommentText } : x))
      );
      setEditingBackupId(null);
      toast.success("Nota atualizada");
    } catch (err) {
      toast.error("Erro ao atualizar nota", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setSavingComment(false);
    }
  };

  const handleConfirmDeleteBackup = async () => {
    if (!backupToDelete || !selectedManualSecret) return;
    setDeletingBackup(true);
    try {
      await deleteManualBackup(selectedManualSecret, backupToDelete.backup_id);
      setManualBackups((prev) => prev.filter((x) => x.backup_id !== backupToDelete.backup_id));
      // Se essa era a única cópia do secret, a "pasta" some da lista de nível 1 — atualiza também.
      listManualBackupSecrets().then(setManualSecrets).catch(() => {});
      toast.success("Backup removido");
      setBackupToDelete(null);
    } catch (err) {
      toast.error("Erro ao remover backup", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setDeletingBackup(false);
    }
  };

  const handleUseRollback = async (backupId: string) => {
    setApplying(backupId);
    try {
      const { tls_crt, tls_key } = await getRollbackContent(cluster, namespace, secretName, backupId);
      onSelect(tls_crt, tls_key);
      toast.success("Certificado carregado do backup");
      onOpenChange(false);
    } catch (err) {
      toast.error("Erro ao carregar backup", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setApplying(null);
    }
  };

  const handleUseManualBackup = async (backupId: string) => {
    if (!selectedManualSecret) return;
    setApplying(backupId);
    try {
      const { tls_crt, tls_key } = await getManualBackupContent(selectedManualSecret, backupId);
      onSelect(tls_crt, tls_key);
      toast.success("Certificado carregado do backup apartado");
      onOpenChange(false);
    } catch (err) {
      toast.error("Erro ao carregar backup", {
        description: err instanceof Error ? err.message : "Erro desconhecido",
      });
    } finally {
      setApplying(null);
    }
  };

  return (
    <>
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[38.4rem] max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>Escolher certificado de um backup</DialogTitle>
          <DialogDescription>
            Selecione um backup pra pré-preencher o certificado e a chave abaixo — sem precisar copiar/colar.
          </DialogDescription>
        </DialogHeader>

        {/* Abas manuais — shadcn <Tabs> quebra flex-1 min-h-0 em modais (ver CLAUDE.md) */}
        <div className="flex border-b border-border flex-shrink-0">
          <button
            className={`px-3 py-1.5 text-xs font-medium border-b-2 -mb-px ${
              tab === "rollback" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"
            }`}
            onClick={() => setTab("rollback")}
          >
            Backup (Rollback)
          </button>
          <button
            className={`px-3 py-1.5 text-xs font-medium border-b-2 -mb-px ${
              tab === "manual" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"
            }`}
            onClick={() => setTab("manual")}
          >
            Backup Apartado
          </button>
        </div>

        {tab === "rollback" && (
          <div className="flex-1 min-h-0 overflow-y-auto">
            {loadingRollbacks ? (
              <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" /> Carregando backups...
              </div>
            ) : rollbacks.length === 0 ? (
              <p className="text-sm text-muted-foreground py-8 text-center">
                Nenhum backup encontrado para {namespace}/{secretName}.
              </p>
            ) : (
              <>
                <div className="relative mb-2">
                  <Search className="absolute left-2 top-2 h-4 w-4 text-muted-foreground" />
                  <Input
                    placeholder="Buscar por data ou subject..."
                    value={rollbackSearch}
                    onChange={(e) => setRollbackSearch(e.target.value)}
                    className="pl-8 h-8 text-sm"
                  />
                </div>
                {filteredRollbacks.length === 0 && (
                  <p className="text-xs text-muted-foreground text-center py-4">Nenhum resultado para a busca.</p>
                )}
              <ScrollArea className="max-h-[40vh] pr-4">
                <div className="space-y-2">
                  {filteredRollbacks.map((b) => (
                    <div
                      key={b.backup_id}
                      className="flex items-center justify-between gap-3 rounded border border-border/50 p-2.5 text-xs"
                    >
                      <div className="space-y-0.5 min-w-0">
                        <p className="font-medium">{new Date(b.backed_up_at).toLocaleString("pt-BR")}</p>
                        <p className="text-muted-foreground truncate" title={b.subject}>
                          {b.subject || "(subject indisponível)"}
                        </p>
                      </div>
                      <Button
                        variant="outline"
                        size="sm"
                        className="flex-shrink-0"
                        disabled={applying === b.backup_id}
                        onClick={() => handleUseRollback(b.backup_id)}
                      >
                        {applying === b.backup_id ? (
                          <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                        ) : (
                          <History className="h-3.5 w-3.5 mr-1.5" />
                        )}
                        Usar este
                      </Button>
                    </div>
                  ))}
                </div>
              </ScrollArea>
              </>
            )}
          </div>
        )}

        {tab === "manual" && (
          <div className="flex-1 min-h-0 overflow-y-auto">
            {selectedManualSecret === null ? (
              // Nível 1: navegar entre as "pastas" (secrets) que têm backup apartado
              loadingManualSecrets ? (
                <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" /> Carregando pastas...
                </div>
              ) : manualSecrets.length === 0 ? (
                <p className="text-sm text-muted-foreground py-8 text-center">
                  Nenhum backup apartado salvo ainda. Use o botão "Copiar para Backup" no detalhe de
                  um certificado pra criar um.
                </p>
              ) : (
                <>
                  <div className="relative mb-2">
                    <Search className="absolute left-2 top-2 h-4 w-4 text-muted-foreground" />
                    <Input
                      placeholder="Buscar pasta (nome do secret)..."
                      value={manualSecretSearch}
                      onChange={(e) => setManualSecretSearch(e.target.value)}
                      className="pl-8 h-8 text-sm"
                    />
                  </div>
                  {filteredManualSecrets.length === 0 && (
                    <p className="text-xs text-muted-foreground text-center py-4">Nenhum resultado para a busca.</p>
                  )}
                <ScrollArea className="max-h-[40vh] pr-4">
                  <div className="space-y-1">
                    {filteredManualSecrets.map((name) => (
                      <button
                        key={name}
                        className="w-full flex items-center gap-2 rounded border border-border/50 p-2.5 text-xs hover:bg-accent/50 transition-colors text-left"
                        onClick={() => openManualSecretFolder(name)}
                      >
                        <Folder className="h-4 w-4 text-blue-400 flex-shrink-0" />
                        <span className="font-medium truncate flex-1">{name}</span>
                        <ChevronRight className="h-3.5 w-3.5 text-muted-foreground flex-shrink-0" />
                      </button>
                    ))}
                  </div>
                </ScrollArea>
                </>
              )
            ) : (
              // Nível 2: backups dentro da pasta escolhida
              <div className="space-y-2">
                <button
                  className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                  onClick={() => setSelectedManualSecret(null)}
                >
                  <ArrowLeft className="h-3.5 w-3.5" /> Voltar às pastas
                </button>
                <div className="flex items-center gap-2 text-xs font-medium">
                  <FolderOpen className="h-4 w-4 text-blue-400" />
                  {selectedManualSecret}
                </div>
                {loadingManualBackups ? (
                  <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
                    <Loader2 className="h-4 w-4 animate-spin" /> Carregando backups...
                  </div>
                ) : manualBackups.length === 0 ? (
                  <p className="text-sm text-muted-foreground py-8 text-center">Nenhum backup nesta pasta.</p>
                ) : (
                  <>
                    <div className="relative">
                      <Search className="absolute left-2 top-2 h-4 w-4 text-muted-foreground" />
                      <Input
                        placeholder="Buscar por data, subject ou comentário..."
                        value={manualBackupSearch}
                        onChange={(e) => setManualBackupSearch(e.target.value)}
                        className="pl-8 h-8 text-sm"
                      />
                    </div>
                    {filteredManualBackups.length === 0 && (
                      <p className="text-xs text-muted-foreground text-center py-4">Nenhum resultado para a busca.</p>
                    )}
                  <ScrollArea className="max-h-[34vh] pr-4">
                    <div className="space-y-2">
                      {filteredManualBackups.map((b) => (
                        <div
                          key={b.backup_id}
                          className="rounded border border-border/50 p-2.5 text-xs space-y-1.5"
                        >
                          <div className="flex items-center justify-between gap-3">
                            <div className="space-y-0.5 min-w-0">
                              <p className="font-medium">{new Date(b.backed_up_at).toLocaleString("pt-BR")}</p>
                              <p className="text-muted-foreground truncate" title={b.subject}>
                                {b.subject || "(subject indisponível)"}
                                {b.not_after && ` — válido até ${new Date(b.not_after).toLocaleDateString("pt-BR")}`}
                              </p>
                              {editingBackupId !== b.backup_id && b.comment && (
                                <p className="text-muted-foreground italic truncate" title={b.comment}>
                                  "{b.comment}"
                                </p>
                              )}
                            </div>
                            <div className="flex items-center gap-1 flex-shrink-0">
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-7 w-7"
                                title="Editar nota"
                                onClick={() => handleStartEditComment(b)}
                              >
                                <Pencil className="h-3.5 w-3.5" />
                              </Button>
                              <Button
                                variant="ghost"
                                size="icon"
                                className="h-7 w-7 text-destructive hover:text-destructive"
                                title="Remover backup"
                                onClick={() => setBackupToDelete(b)}
                              >
                                <Trash2 className="h-3.5 w-3.5" />
                              </Button>
                              <Button
                                variant="outline"
                                size="sm"
                                className="flex-shrink-0"
                                disabled={applying === b.backup_id}
                                onClick={() => handleUseManualBackup(b.backup_id)}
                              >
                                {applying === b.backup_id ? (
                                  <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                                ) : (
                                  <FolderOpen className="h-3.5 w-3.5 mr-1.5" />
                                )}
                                Usar este
                              </Button>
                            </div>
                          </div>
                          {editingBackupId === b.backup_id && (
                            <div className="flex items-center gap-1.5 pt-1 border-t border-border/30">
                              <Input
                                value={editCommentText}
                                onChange={(e) => setEditCommentText(e.target.value)}
                                placeholder="Comentário..."
                                className="h-7 text-xs flex-1"
                                autoFocus
                                onKeyDown={(e) => {
                                  if (e.key === "Enter") handleSaveComment(b.backup_id);
                                  if (e.key === "Escape") setEditingBackupId(null);
                                }}
                              />
                              <Button
                                size="sm"
                                className="h-7 flex-shrink-0"
                                disabled={savingComment}
                                onClick={() => handleSaveComment(b.backup_id)}
                              >
                                {savingComment && <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" />}
                                Salvar
                              </Button>
                              <Button
                                size="sm"
                                variant="outline"
                                className="h-7 flex-shrink-0"
                                disabled={savingComment}
                                onClick={() => setEditingBackupId(null)}
                              >
                                Cancelar
                              </Button>
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  </ScrollArea>
                  </>
                )}
              </div>
            )}
          </div>
        )}

        <DialogFooter className="flex-shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancelar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

      <AlertDialog open={!!backupToDelete} onOpenChange={(next) => !next && !deletingBackup && setBackupToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Confirmar Exclusão</AlertDialogTitle>
            <AlertDialogDescription>
              Tem certeza que deseja remover o backup de{" "}
              <strong>{backupToDelete && new Date(backupToDelete.backed_up_at).toLocaleString("pt-BR")}</strong>
              {backupToDelete?.subject && <> ({backupToDelete.subject})</>}? Esta ação não pode ser desfeita.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deletingBackup}>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleConfirmDeleteBackup}
              disabled={deletingBackup}
              className="bg-destructive hover:bg-destructive/90"
            >
              {deletingBackup && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Remover
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
