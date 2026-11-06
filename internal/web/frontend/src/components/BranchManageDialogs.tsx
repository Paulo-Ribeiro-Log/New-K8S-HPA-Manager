import { useEffect, useState } from "react";
import { AlertCircle, Loader2, Pencil, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { apiClient } from "@/lib/api/client";

// Renomear e apagar branches (painel Branches do Code Editor).

export const PROTECTED_BRANCHES = ["main", "master"];

interface BaseProps {
  repoId: string;
  branch: string;
  /** Existe origin/<branch>. */
  onRemote: boolean;
  profileId?: string;
  onClose: () => void;
  /** Chamado após sucesso, com o texto a mostrar. */
  onDone: (message: string) => void | Promise<void>;
}

function errMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e ?? "");
}

function ErrorLine({ text }: { text: string }) {
  return (
    <div className="flex items-start gap-2 text-xs text-red-400">
      <AlertCircle className="w-3 h-3 flex-shrink-0 mt-0.5" />
      <span className="min-w-0 whitespace-pre-wrap [overflow-wrap:anywhere]">{text}</span>
    </div>
  );
}

export function RenameBranchDialog({ repoId, branch, onRemote, profileId, onClose, onDone }: BaseProps) {
  const [name, setName] = useState(branch);
  const [renameRemote, setRenameRemote] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const canRenameRemote = onRemote && !PROTECTED_BRANCHES.includes(branch);

  useEffect(() => { setName(branch); setError(""); }, [branch]);

  async function submit() {
    const target = name.trim();
    if (!target || target === branch || loading) return;
    setLoading(true); setError("");
    try {
      const r = await apiClient.codeEditorRenameBranch(repoId, branch, target, renameRemote && canRenameRemote, profileId);
      await onDone(r.warning ? `${branch} → ${target}\n⚠ ${r.warning}` : `${branch} → ${target}${r.remote_renamed ? " (local e origin)" : ""}`);
    } catch (e) {
      setError(errMessage(e) || "Erro ao renomear a branch");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open onOpenChange={(v) => { if (!v && !loading) onClose(); }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm"><Pencil className="w-4 h-4" />Renomear branch</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <p className="text-xs text-muted-foreground [overflow-wrap:anywhere]">
            Branch atual: <span className="font-mono text-foreground">{branch}</span>
          </p>
          <Input
            autoFocus
            className="font-mono text-xs"
            placeholder="Novo nome"
            value={name}
            disabled={loading}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") void submit(); }}
          />
          {canRenameRemote && (
            <label className="flex items-start gap-2 cursor-pointer text-xs">
              <Checkbox className="mt-0.5" checked={renameRemote} onCheckedChange={(v) => setRenameRemote(v === true)} disabled={loading} />
              <span>
                Renomear também no origin
                <span className="block text-[11px] text-muted-foreground">
                  Publica o nome novo e apaga <span className="font-mono">{branch}</span> do remoto. PRs abertos dessa branch serão fechados pelo GitHub.
                </span>
              </span>
            </label>
          )}
          {onRemote && !renameRemote && canRenameRemote && (
            <p className="text-[11px] text-amber-300">Só a branch local será renomeada; o origin continua com o nome antigo.</p>
          )}
          {error && <ErrorLine text={error} />}
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose} disabled={loading}>Cancelar</Button>
          <Button size="sm" onClick={() => void submit()} disabled={loading || !name.trim() || name.trim() === branch}>
            {loading ? <><Loader2 className="w-3 h-3 animate-spin mr-1" />Renomeando…</> : "Renomear"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

interface DeleteProps extends BaseProps {
  /** Existe a branch local (false = só aparece como origin/<branch>). */
  hasLocal: boolean;
}

export function DeleteBranchDialog({ repoId, branch, hasLocal, onRemote, profileId, onClose, onDone }: DeleteProps) {
  const protectedBranch = PROTECTED_BRANCHES.includes(branch);
  const canRemote = onRemote && !protectedBranch;
  const [alsoRemote, setAlsoRemote] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [notMerged, setNotMerged] = useState(false);

  const scope: "local" | "remote" | "both" = !hasLocal ? "remote" : alsoRemote && canRemote ? "both" : "local";

  async function run(force: boolean) {
    setLoading(true); setError("");
    try {
      await apiClient.codeEditorDeleteBranch(repoId, branch, scope, force, profileId);
      await onDone(`Branch ${branch} apagada${scope === "both" ? " (local e origin)" : scope === "remote" ? " do origin" : ""}`);
    } catch (e) {
      setError(errMessage(e) || "Erro ao apagar a branch");
      setNotMerged((e as { details?: { error_type?: string } } | null)?.details?.error_type === "not_merged");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open onOpenChange={(v) => { if (!v && !loading) onClose(); }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm"><Trash2 className="w-4 h-4 text-red-400" />Apagar branch</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <p className="text-xs [overflow-wrap:anywhere]">
            {scope === "remote" ? "Apagar do origin" : "Apagar"} <span className="font-mono text-foreground">{branch}</span>?
          </p>
          {protectedBranch && onRemote && (
            <p className="text-[11px] text-muted-foreground">
              <span className="font-mono">{branch}</span> é uma branch protegida: só a cópia local pode ser apagada por aqui.
            </p>
          )}
          {hasLocal && canRemote && (
            <label className="flex items-start gap-2 cursor-pointer text-xs">
              <Checkbox className="mt-0.5" checked={alsoRemote} onCheckedChange={(v) => setAlsoRemote(v === true)} disabled={loading} />
              <span>
                Apagar também do origin
                <span className="block text-[11px] text-muted-foreground">Remove a branch do GitHub; quem a usa perde a referência.</span>
              </span>
            </label>
          )}
          {scope === "remote" && (
            <p className="text-[11px] text-amber-300">A branch será removida do GitHub (origin). Esta ação não tem desfazer aqui.</p>
          )}
          {error && <ErrorLine text={error} />}
          {notMerged && (
            <p className="text-[11px] text-amber-300">
              A branch tem commits que não estão em nenhuma outra. Apagar mesmo assim descarta esses commits (ainda recuperáveis pelo reflog por um tempo).
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose} disabled={loading}>Cancelar</Button>
          {notMerged && (
            <Button variant="destructive" size="sm" onClick={() => void run(true)} disabled={loading}>Apagar mesmo assim</Button>
          )}
          {!notMerged && (
            <Button variant="destructive" size="sm" onClick={() => void run(false)} disabled={loading}>
              {loading ? <><Loader2 className="w-3 h-3 animate-spin mr-1" />Apagando…</> : "Apagar"}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
