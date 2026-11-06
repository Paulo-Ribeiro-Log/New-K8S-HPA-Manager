import { useEffect, useRef, useState } from "react";
import { Cherry, ChevronRight, Copy, Download, GitBranch, GitCommit, Loader2, Plus, RefreshCw, Search, Upload } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  apiClient,
  type CodeEditorBranches,
  type CodeEditorGitStatus,
  type CodeEditorLogEntry,
  type CodeEditorRemote,
} from "@/lib/api/client";
import { isStaged, isUntracked, scmBadge, scmColor } from "@/lib/gitStatus";

// Painel Git do Code Editor (lado direito, recolhível): branch, histórico com busca, remotes,
// sincronização (push/pull/fetch) e commit com seleção de arquivos. Não mantém uma cópia do status:
// recebe `status` e `branches` do editor, que já os recarrega após salvar/commitar/trocar de branch.

const LOG_LIMITS = [5, 10, 20, 50];
const LOG_LIMIT_KEY = "ce_git_log_limit";
const SEARCH_DEBOUNCE_MS = 300;

function readLogLimit(): number {
  try {
    const n = Number(localStorage.getItem(LOG_LIMIT_KEY));
    return LOG_LIMITS.includes(n) ? n : LOG_LIMITS[0];
  } catch {
    return LOG_LIMITS[0];
  }
}

function errMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e ?? "");
}

export interface GitSidePanelProps {
  repoId: string;
  status: CodeEditorGitStatus | null;
  branches: CodeEditorBranches | null;
  profileId?: string;
  onCollapse: () => void;
  onRefresh: () => void | Promise<void>;
  /** Troca de branch pelo fluxo do editor (confirma mudanças não salvas, recarrega árvore/abas). */
  onCheckout: (branch: string) => Promise<void>;
  onCreateBranch: () => void;
  /** Executa push/pull/fetch no diálogo de streaming do editor. */
  onRunSse: (dialog: { title: string; endpoint: string; body?: object }) => void;
  onCommitted: () => void | Promise<void>;
  onOpenFile: (path: string) => void;
  /** Cherry-pick do commit no branch atual (confirmação e conflitos ficam com o editor). `from` = branch de origem. */
  onCherryPick: (hash: string, from?: string) => void | Promise<void>;
  onToast: (kind: "success" | "error", message: string) => void;
}

function SectionTitle({ children, right }: { children: React.ReactNode; right?: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between mb-1">
      <h3 className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">{children}</h3>
      {right}
    </div>
  );
}

const selectClass =
  "h-7 min-w-0 rounded-md border border-input bg-background px-2 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50";

export function GitSidePanel({
  repoId,
  status,
  branches,
  profileId,
  onCollapse,
  onRefresh,
  onCheckout,
  onCreateBranch,
  onRunSse,
  onCommitted,
  onOpenFile,
  onCherryPick,
  onToast,
}: GitSidePanelProps) {
  const [limit, setLimit] = useState(readLogLimit);
  const [query, setQuery] = useState("");
  const [debouncedQuery, setDebouncedQuery] = useState("");
  // Branch cujo histórico é listado ("" = atual); permite escolher a origem do cherry-pick
  const [logRef, setLogRef] = useState("");
  const [log, setLog] = useState<CodeEditorLogEntry[]>([]);
  const [logLoading, setLogLoading] = useState(false);
  const [logError, setLogError] = useState("");
  const [remotes, setRemotes] = useState<CodeEditorRemote[] | null>(null);
  // Arquivos DESMARCADOS (e não os marcados): arquivo novo no status já entra marcado.
  const [unchecked, setUnchecked] = useState<Set<string>>(new Set());
  const [message, setMessage] = useState("");
  const [committing, setCommitting] = useState(false);
  const [switching, setSwitching] = useState(false);
  const logRequest = useRef(0);

  // Trocar de repositório limpa o que era do anterior.
  useEffect(() => {
    setQuery("");
    setDebouncedQuery("");
    setLogRef("");
    setUnchecked(new Set());
    setMessage("");
  }, [repoId]);

  useEffect(() => {
    const t = setTimeout(() => setDebouncedQuery(query), SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [query]);

  // Histórico: recarrega ao mudar repo, limite ou busca, e quando o status muda (commit, pull,
  // troca de branch). `status` é um objeto novo a cada recarga do editor.
  useEffect(() => {
    const id = ++logRequest.current;
    setLogLoading(true);
    apiClient
      .codeEditorGetLog(repoId, limit, debouncedQuery, logRef)
      .then((entries) => {
        if (id !== logRequest.current) return;
        setLog(entries);
        setLogError("");
      })
      .catch((e) => {
        if (id !== logRequest.current) return;
        setLog([]);
        setLogError(errMessage(e) || "Erro ao carregar o histórico");
      })
      .finally(() => {
        if (id === logRequest.current) setLogLoading(false);
      });
  }, [repoId, limit, debouncedQuery, logRef, status]);

  useEffect(() => {
    let cancelled = false;
    setRemotes(null);
    apiClient
      .codeEditorGetRemotes(repoId)
      .then((r) => !cancelled && setRemotes(r))
      .catch(() => !cancelled && setRemotes([]));
    return () => {
      cancelled = true;
    };
  }, [repoId]);

  const files = status?.files ?? [];
  const modifiedCount = files.filter((f) => !isUntracked(f.status)).length;
  const untrackedCount = files.length - modifiedCount;
  const selected = files.filter((f) => !unchecked.has(f.path));
  const allSelected = files.length > 0 && selected.length === files.length;
  const hasAhead = !!status?.ahead && status.ahead !== "0";
  const hasBehind = !!status?.behind && status.behind !== "0";
  const body = profileId ? { profile_id: profileId } : undefined;

  function toggleFile(path: string) {
    setUnchecked((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }

  function toggleAll() {
    setUnchecked(allSelected ? new Set(files.map((f) => f.path)) : new Set());
  }

  async function handleCheckout(branch: string) {
    if (!branch || branch === branches?.current) return;
    setSwitching(true);
    try {
      await onCheckout(branch);
    } catch {
      /* cancelado pelo usuário ou já avisado pelo editor */
    } finally {
      setSwitching(false);
    }
  }

  function changeLimit(n: number) {
    setLimit(n);
    try {
      localStorage.setItem(LOG_LIMIT_KEY, String(n));
    } catch {
      /* storage indisponível */
    }
  }

  function copyHash(hash: string) {
    navigator.clipboard
      .writeText(hash)
      .then(() => onToast("success", `Hash copiado: ${hash.slice(0, 7)}`))
      .catch(() => onToast("error", "Não foi possível copiar o hash"));
  }

  async function commit() {
    const msg = message.trim();
    if (!msg || selected.length === 0 || committing) return;
    setCommitting(true);
    try {
      // O commit leva só o que está marcado: o que já estava no staging e foi desmarcado sai do índice.
      const keep = new Set(selected.map((f) => f.path));
      const stagedOut = files.filter((f) => isStaged(f.status) && !keep.has(f.path)).map((f) => f.path);
      if (stagedOut.length > 0) await apiClient.codeEditorUnstage(repoId, stagedOut);
      await apiClient.codeEditorCommit(repoId, msg, selected.map((f) => f.path));
      setMessage("");
      setUnchecked(new Set());
      onToast("success", `Commit criado com ${selected.length} arquivo(s)`);
      await onCommitted();
    } catch (e) {
      onToast("error", errMessage(e) || "Erro ao commitar");
    } finally {
      setCommitting(false);
    }
  }

  return (
    <div className="flex flex-col h-full min-h-0 bg-card/20">
      {/* Cabeçalho */}
      <div className="flex items-center gap-1.5 px-2 py-1.5 border-b border-border/30 flex-shrink-0">
        <GitBranch className="w-3.5 h-3.5 text-primary" />
        <span className="text-xs font-semibold">Git</span>
        <div className="flex-1" />
        <button
          onClick={() => void onRefresh()}
          title="Atualizar status e histórico"
          className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-muted"
        >
          <RefreshCw className="w-3 h-3" />
        </button>
        <button
          onClick={onCollapse}
          title="Recolher painel Git"
          aria-label="Recolher painel Git"
          className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-muted"
        >
          <ChevronRight className="w-3.5 h-3.5" />
        </button>
      </div>

      {/* Conteúdo rolável */}
      <div className="flex-1 min-h-0 overflow-y-auto p-2 space-y-3">
        {/* Branch */}
        <section>
          <SectionTitle>Branch</SectionTitle>
          <div className="flex gap-1">
            <select
              className={`${selectClass} flex-1`}
              value={branches?.current ?? ""}
              disabled={!branches || switching}
              onChange={(e) => void handleCheckout(e.target.value)}
              aria-label="Branch atual"
            >
              {!branches && <option value="">Carregando...</option>}
              {(branches?.local ?? []).map((b) => (
                <option key={b} value={b}>
                  {b}
                  {b === branches?.current ? " (atual)" : ""}
                </option>
              ))}
            </select>
            <Button variant="outline" size="sm" className="h-7 px-2 text-xs gap-1" onClick={onCreateBranch} title="Criar nova branch">
              {switching ? <Loader2 className="w-3 h-3 animate-spin" /> : <Plus className="w-3 h-3" />}
              Nova
            </Button>
          </div>
        </section>

        {/* Histórico */}
        <section>
          <SectionTitle right={logLoading ? <Loader2 className="w-3 h-3 animate-spin text-muted-foreground" /> : undefined}>
            Histórico
          </SectionTitle>
          <select
            className={`${selectClass} w-full mb-1`}
            value={logRef}
            onChange={(e) => setLogRef(e.target.value)}
            title="Mostrar os commits de outro branch (origem do cherry-pick)"
            aria-label="Branch do histórico"
          >
            <option value="">Branch atual{branches ? ` (${branches.current})` : ""}</option>
            {[...(branches?.local ?? []), ...(branches?.remote ?? [])]
              .filter((b) => b !== branches?.current && !b.includes("->"))
              .map((b) => (
                <option key={b} value={b}>
                  {b}
                </option>
              ))}
          </select>
          <div className="flex gap-1 mb-1.5">
            <select
              className={`${selectClass} w-14 flex-shrink-0`}
              value={limit}
              onChange={(e) => changeLimit(Number(e.target.value))}
              title="Quantos commits mostrar"
              aria-label="Quantidade de commits"
            >
              {LOG_LIMITS.map((n) => (
                <option key={n} value={n}>
                  {n}
                </option>
              ))}
            </select>
            <div className="relative flex-1 min-w-0">
              <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground pointer-events-none" />
              <Input
                className="h-7 pl-7 text-xs"
                placeholder="Buscar revisão / mensagem..."
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </div>
          </div>
          {logError ? (
            <p className="text-[11px] text-red-400">{logError}</p>
          ) : log.length === 0 && !logLoading ? (
            <p className="text-[11px] text-muted-foreground px-1">
              {debouncedQuery ? "Nenhum commit encontrado." : "Sem commits."}
            </p>
          ) : (
            <div className="space-y-0.5">
              {log.map((c) => (
                <div key={c.hash} className="group flex items-center gap-1 rounded hover:bg-muted/40">
                  <button
                    onClick={() => copyHash(c.hash)}
                    title={`${c.hash}\n${c.author} · ${c.when}\n\n${c.message}\n\nClique para copiar o hash`}
                    className="min-w-0 flex-1 flex items-center gap-1.5 px-1.5 py-1 text-left text-xs"
                  >
                    <span className="font-mono text-[10px] text-primary flex-shrink-0">{c.hash.slice(0, 7)}</span>
                    <span className="truncate flex-1 text-foreground/80">{c.message}</span>
                    <Copy className="w-2.5 h-2.5 text-muted-foreground opacity-0 group-hover:opacity-100 flex-shrink-0" />
                  </button>
                  <button
                    onClick={() => void onCherryPick(c.hash, logRef || undefined)}
                    title={`Cherry-pick: aplicar este commit no branch atual${branches ? ` (${branches.current})` : ""}`}
                    aria-label={`Cherry-pick de ${c.hash.slice(0, 7)}`}
                    className="flex-shrink-0 mr-1 p-0.5 rounded text-primary hover:bg-muted"
                  >
                    <Cherry className="w-3 h-3" />
                  </button>
                </div>
              ))}
            </div>
          )}
        </section>

        {/* Remotes */}
        <section>
          <SectionTitle>Remotes</SectionTitle>
          {remotes === null ? (
            <p className="text-[11px] text-muted-foreground px-1">Carregando...</p>
          ) : remotes.length === 0 ? (
            <p className="text-[11px] text-muted-foreground px-1">Nenhum remote configurado.</p>
          ) : (
            <div className="space-y-0.5">
              {remotes.map((r) => (
                <div key={r.name} className="flex items-baseline gap-2 px-1 text-xs">
                  <span className="font-semibold flex-shrink-0">{r.name}</span>
                  <span className="font-mono text-[10px] text-muted-foreground truncate" title={r.url}>
                    {r.url}
                  </span>
                </div>
              ))}
            </div>
          )}
        </section>

        {/* Sincronizar */}
        <section>
          <SectionTitle>Sincronizar</SectionTitle>
          <div className="flex gap-1">
            <Button
              variant="outline"
              size="sm"
              className="flex-1 h-7 text-xs gap-1"
              onClick={() => onRunSse({ title: "Git Push", endpoint: `/code-editor/repos/${repoId}/push`, body })}
            >
              <Upload className="w-3 h-3" />
              Push
              {hasAhead && <span className="text-[10px] text-blue-400">↑{status?.ahead}</span>}
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="flex-1 h-7 text-xs gap-1"
              onClick={() => onRunSse({ title: "Git Pull", endpoint: `/code-editor/repos/${repoId}/pull`, body })}
            >
              <Download className="w-3 h-3" />
              Pull
              {hasBehind && <span className="text-[10px] text-yellow-400">↓{status?.behind}</span>}
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="flex-1 h-7 text-xs gap-1"
              onClick={() => onRunSse({ title: "Git Fetch", endpoint: `/code-editor/repos/${repoId}/fetch`, body })}
            >
              <RefreshCw className="w-3 h-3" />
              Fetch
            </Button>
          </div>
        </section>
      </div>

      {/* Commit: fixo embaixo, a lista de arquivos rola por dentro */}
      <div className="flex-shrink-0 border-t border-border/30 p-2 space-y-1.5">
        <SectionTitle
          right={
            files.length > 0 ? (
              <button onClick={toggleAll} className="text-[10px] text-muted-foreground hover:text-foreground px-1 rounded hover:bg-muted">
                {allSelected ? "− todos" : "+ todos"}
              </button>
            ) : undefined
          }
        >
          Commit
        </SectionTitle>
        <p className="text-[11px] text-muted-foreground text-center">
          {files.length === 0
            ? "Nada para commitar"
            : `${modifiedCount} modificado(s)${untrackedCount > 0 ? ` · ${untrackedCount} não rastreado(s)` : ""}`}
        </p>

        {files.length > 0 && (
          <div className="max-h-44 overflow-y-auto rounded border border-border/40 bg-background/40 p-0.5">
            {files.map((f) => {
              const name = f.path.split("/").pop() ?? f.path;
              const dir = f.path.includes("/") ? f.path.slice(0, f.path.lastIndexOf("/")) : "";
              return (
                <div key={f.path} className="flex items-center gap-1.5 px-1 py-0.5 rounded hover:bg-muted/40 text-xs">
                  <Checkbox
                    className="h-3.5 w-3.5 flex-shrink-0"
                    checked={!unchecked.has(f.path)}
                    onCheckedChange={() => toggleFile(f.path)}
                    aria-label={`Incluir ${f.path} no commit`}
                  />
                  <span className={`font-bold w-3 text-center flex-shrink-0 ${scmColor(f.status)}`}>{scmBadge(f.status)}</span>
                  <button
                    className="min-w-0 flex-1 truncate text-left font-mono text-foreground/70 hover:text-foreground"
                    title={f.path}
                    onClick={() => onOpenFile(f.path)}
                  >
                    {name}
                    {dir && <span className="ml-1.5 text-[10px] text-muted-foreground">{dir}</span>}
                  </button>
                </div>
              );
            })}
          </div>
        )}

        <textarea
          className="w-full min-h-[56px] resize-y rounded-md border border-input bg-background px-2 py-1.5 text-xs placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-50"
          placeholder="Mensagem do commit... (Ctrl+Enter)"
          value={message}
          disabled={files.length === 0 || committing}
          onChange={(e) => setMessage(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
              e.preventDefault();
              void commit();
            }
          }}
        />
        <Button
          className="w-full h-8 text-xs gap-1"
          disabled={!message.trim() || selected.length === 0 || committing}
          onClick={() => void commit()}
        >
          {committing ? <Loader2 className="w-3 h-3 animate-spin" /> : <GitCommit className="w-3 h-3" />}
          {selected.length > 0 ? `Commit ${selected.length} alteraç${selected.length === 1 ? "ão" : "ões"}` : "Commit"}
        </Button>
      </div>
    </div>
  );
}
