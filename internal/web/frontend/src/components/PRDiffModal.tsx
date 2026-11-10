import { useEffect, useMemo, useState } from "react";
import { AlertCircle, CheckCircle2, ChevronDown, ChevronUp, Copy, ExternalLink, FileDiff, GitMerge, GitPullRequest, Loader2, MessageSquare, RefreshCw, XCircle } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { parsePatch, type DiffLine } from "@/lib/diffLines";
import { apiClient, type CodeEditorPRDiffResponse, type CodeEditorPRMergeStatus, type CodeEditorPRReviews, type CodeEditorReviewEvent } from "@/lib/api/client";

// Diff do PR da branch atual (somente leitura): arquivos alterados à esquerda e o patch do
// arquivo selecionado à direita, como na aba "Files changed" do GitHub.

interface PRDiffModalProps {
  repoId: string;
  profileId?: string;
  /** Número do PR; sem ele, usa o PR aberto mais recente da branch (a atual, se `branch` não vier). */
  prNumber?: number;
  /** Branch com PR (não precisa ser a atual); sem ela, a branch atual do repositório. */
  branch?: string;
  onClose: () => void;
  /** Chamado depois de um merge feito aqui (o painel de branches atualiza os selos). */
  onMerged?: () => void;
}

const STATUS_BADGE: Record<string, { label: string; cls: string }> = {
  added: { label: "A", cls: "text-emerald-400" },
  modified: { label: "M", cls: "text-amber-300" },
  removed: { label: "D", cls: "text-red-400" },
  renamed: { label: "R", cls: "text-sky-400" },
};

// Como cada estado de revisão aparece na lista de revisores.
const REVIEW_STATE: Record<string, { label: string; cls: string }> = {
  APPROVED: { label: "aprovou", cls: "text-emerald-400 border-emerald-500/40 bg-emerald-500/10" },
  CHANGES_REQUESTED: { label: "pediu alterações", cls: "text-red-400 border-red-500/40 bg-red-500/10" },
  COMMENTED: { label: "comentou", cls: "text-muted-foreground border-border/60 bg-muted/30" },
  DISMISSED: { label: "revisão descartada", cls: "text-amber-300 border-amber-500/40 bg-amber-500/10" },
};

const EVENT_CHOICES: { id: CodeEditorReviewEvent; label: string; hint: string }[] = [
  { id: "COMMENT", label: "Comentar", hint: "só deixa um comentário, sem aprovar nem bloquear" },
  { id: "APPROVE", label: "Aprovar", hint: "aprova este commit do PR" },
  { id: "REQUEST_CHANGES", label: "Solicitar alterações", hint: "bloqueia o merge até as alterações (exige um texto)" },
];

function lineClass(kind: DiffLine["kind"]): string {
  if (kind === "hunk") return "text-sky-300 bg-sky-500/10";
  if (kind === "add") return "text-emerald-300 bg-emerald-500/10";
  if (kind === "del") return "text-red-300 bg-red-500/10";
  return "text-zinc-300";
}

export function PRDiffModal({ repoId, profileId, prNumber, branch, onClose, onMerged }: PRDiffModalProps) {
  const [data, setData] = useState<CodeEditorPRDiffResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [idx, setIdx] = useState(0);
  const [copied, setCopied] = useState(false);
  // Revisão do PR (aprovar / pedir alterações / comentar), enviada ao GitHub em nome do PAT do perfil.
  const [rev, setRev] = useState<CodeEditorPRReviews | null>(null);
  const [revError, setRevError] = useState("");
  const [revOpen, setRevOpen] = useState(false);
  const [event, setEvent] = useState<CodeEditorReviewEvent>("COMMENT");
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState("");
  const [reloadKey, setReloadKey] = useState(0); // recarrega o diff e as revisões
  // Merge do PR: estado no GitHub, método e se apaga a branch depois.
  const [mstat, setMstat] = useState<CodeEditorPRMergeStatus | null>(null);
  const [mstatError, setMstatError] = useState("");
  const [mergeOpen, setMergeOpen] = useState(false);
  const [method, setMethod] = useState<"merge" | "squash" | "rebase">("merge");
  const [deleteBranch, setDeleteBranch] = useState(false);
  const [merging, setMerging] = useState(false);
  const [mergeError, setMergeError] = useState("");
  const [mergedNow, setMergedNow] = useState<{ branchDeleted: boolean; branchError?: string } | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    apiClient
      .codeEditorGetPRDiff(repoId, prNumber, profileId, branch)
      .then((r) => { if (!cancelled) { setData(r); setIdx(0); } })
      .catch((e) => { if (!cancelled) setError(e instanceof Error ? e.message : "Erro ao buscar o diff do PR"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [repoId, prNumber, profileId, branch, reloadKey]);

  // Revisões do PR: quem aprovou/pediu alterações, o autor e se este usuário pode aprovar.
  const prNum = data?.pr.number;
  useEffect(() => {
    if (!prNum) return;
    let cancelled = false;
    setRevError("");
    apiClient
      .codeEditorPRReviews(repoId, prNum, profileId, branch)
      .then((r) => { if (!cancelled) setRev(r); })
      .catch((e) => { if (!cancelled) { setRev(null); setRevError(e instanceof Error ? e.message : "Erro ao buscar as revisões"); } });
    return () => { cancelled = true; };
  }, [repoId, prNum, profileId, branch, reloadKey]);

  useEffect(() => {
    if (!prNum) return;
    let cancelled = false;
    setMstatError("");
    apiClient
      .codeEditorPRMergeStatus(repoId, prNum, profileId)
      .then((r) => { if (!cancelled) setMstat(r); })
      .catch((e) => { if (!cancelled) { setMstat(null); setMstatError(e instanceof Error ? e.message : "Erro ao consultar o merge"); } });
    return () => { cancelled = true; };
  }, [repoId, prNum, profileId, reloadKey]);

  const reviewedSha = data?.pr.head_sha ?? "";
  // O PR andou depois que o diff foi carregado: o que se vê não é mais o que seria aprovado.
  const diffOutdated = !!rev && !!reviewedSha && rev.pr.head_sha !== reviewedSha;
  const choiceBlocked = (ev: CodeEditorReviewEvent): string => {
    if (!rev) return "revisões não carregadas";
    if (!rev.can_comment) return rev.approve_block_reason ?? "o PR não recebe revisão";
    if (ev !== "COMMENT" && !rev.can_approve) return rev.approve_block_reason ?? "";
    return "";
  };
  const needsText = event !== "APPROVE";
  const blocked = choiceBlocked(event);
  const canSend = !sending && !blocked && !diffOutdated && !!reviewedSha && (!needsText || text.trim() !== "");

  const mergeBlocked = mergedNow ? "o PR já foi mergeado" : mstatError ? "estado do merge indisponível" : !mstat ? "consultando o estado do PR…" : mstat.merge_block_reason ?? "";
  const canMerge = !merging && !mergeBlocked && !diffOutdated && !!reviewedSha;

  async function doMerge() {
    if (!data || !canMerge) return;
    const label = method === "merge" ? "merge commit" : method === "squash" ? "squash" : "rebase";
    if (!window.confirm(
      `Mergear o PR #${data.pr.number} "${data.pr.title}" em ${data.pr.base} (${label})?\n\nO merge é feito no GitHub em nome de ${rev?.viewer || "seu usuário do GitHub"}, com o commit ${reviewedSha.slice(0, 7)} (o que você viu neste diff)${deleteBranch ? " e a branch de origem será apagada depois" : ""}. Não dá para desfazer por aqui.`,
    )) return;
    setMerging(true);
    setMergeError("");
    try {
      const r = await apiClient.codeEditorMergePR(repoId, { pr: data.pr.number, head_sha: reviewedSha, method, delete_branch: deleteBranch }, profileId);
      setMergedNow({ branchDeleted: r.branch_deleted, branchError: r.branch_error });
      toast.success(`PR #${data.pr.number} mergeado${r.branch_deleted ? " e branch apagada" : ""}`);
      if (r.branch_error) toast.warning(`Merge feito, mas a branch não foi apagada: ${r.branch_error}`);
      onMerged?.();
    } catch (e) {
      setMergeError(e instanceof Error ? e.message : "Erro ao mergear o PR");
    } finally {
      setMerging(false);
    }
  }

  async function sendReview() {
    if (!data || !canSend) return;
    const what = event === "APPROVE" ? "Aprovar" : event === "REQUEST_CHANGES" ? "Solicitar alterações em" : "Comentar em";
    if (event !== "COMMENT" && !window.confirm(
      `${what} o PR #${data.pr.number} "${data.pr.title}"?\n\nA revisão é enviada ao GitHub em nome de ${rev?.viewer || "seu usuário do GitHub"}, para o commit ${reviewedSha.slice(0, 7)} (o que você viu neste diff).`,
    )) return;
    setSending(true);
    setSendError("");
    try {
      await apiClient.codeEditorSubmitPRReview(repoId, { pr: data.pr.number, event, body: text.trim() || undefined, head_sha: reviewedSha }, profileId, branch);
      toast.success(event === "APPROVE" ? `PR #${data.pr.number} aprovado` : event === "REQUEST_CHANGES" ? `Alterações solicitadas no PR #${data.pr.number}` : "Comentário enviado");
      setText("");
      setEvent("COMMENT");
      const fresh = await apiClient.codeEditorPRReviews(repoId, data.pr.number, profileId, branch);
      setRev(fresh);
    } catch (e) {
      setSendError(e instanceof Error ? e.message : "Erro ao enviar a revisão");
    } finally {
      setSending(false);
    }
  }

  const files = data?.files ?? [];
  const file = files[idx];
  const lines = useMemo(() => parsePatch(file?.patch ?? ""), [file]);
  const totals = useMemo(
    () => files.reduce((t, f) => ({ add: t.add + f.additions, del: t.del + f.deletions }), { add: 0, del: 0 }),
    [files],
  );

  function copyLink() {
    if (!data?.pr.url) return;
    navigator.clipboard.writeText(data.pr.url).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }).catch(() => {});
  }

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-6xl h-[85vh] flex flex-col gap-2 p-4">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2 text-sm pr-6">
            <GitPullRequest className="w-4 h-4 flex-shrink-0" />
            <span className="truncate">{data ? `PR #${data.pr.number} — ${data.pr.title}` : "Diff do Pull Request"}</span>
          </DialogTitle>
        </DialogHeader>

        {data && (
          <div className="flex items-center gap-3 text-xs text-muted-foreground flex-shrink-0">
            <span>{files.length} arquivo(s)</span>
            <span className="text-emerald-400">+{totals.add}</span>
            <span className="text-red-400">-{totals.del}</span>
            <span className="font-mono">→ {data.pr.base}</span>
            <div className="flex-1" />
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1" onClick={copyLink}>
              <Copy className="w-3 h-3" />{copied ? "Copiado!" : "Copiar link"}
            </Button>
            <Button variant="outline" size="sm" className="h-6 text-xs gap-1" onClick={() => window.open(data.pr.url, "_blank")}>
              <ExternalLink className="w-3 h-3" />Abrir no GitHub
            </Button>
          </div>
        )}

        <div className="flex-1 min-h-0 flex border border-border/40 rounded overflow-hidden">
          {loading ? (
            <div className="flex items-center gap-2 p-4 text-sm text-muted-foreground"><Loader2 className="w-4 h-4 animate-spin" />Buscando o diff no GitHub…</div>
          ) : error ? (
            <div className="flex items-start gap-2 p-4 text-sm text-red-400"><AlertCircle className="w-4 h-4 mt-0.5 flex-shrink-0" /><span className="whitespace-pre-wrap break-words">{error}</span></div>
          ) : files.length === 0 ? (
            <div className="p-4 text-sm text-muted-foreground">O PR não tem arquivos alterados.</div>
          ) : (
            <>
              <div className="w-72 flex-shrink-0 overflow-y-auto border-r border-border/40">
                {files.map((f, i) => {
                  const b = STATUS_BADGE[f.status] ?? { label: "?", cls: "text-muted-foreground" };
                  const name = f.filename.split("/").pop() ?? f.filename;
                  const dir = f.filename.includes("/") ? f.filename.slice(0, f.filename.lastIndexOf("/")) : "";
                  return (
                    <button
                      key={f.filename}
                      onClick={() => setIdx(i)}
                      title={f.previous_filename ? `${f.previous_filename} → ${f.filename}` : f.filename}
                      className={`w-full flex items-center gap-1.5 px-2 py-1 text-left text-xs hover:bg-muted/40 ${i === idx ? "bg-muted/60" : ""}`}
                    >
                      <span className={`font-bold w-3 text-center flex-shrink-0 ${b.cls}`}>{b.label}</span>
                      <span className="min-w-0 flex-1 truncate font-mono">
                        {name}
                        {dir && <span className="ml-1.5 text-[10px] text-muted-foreground">{dir}</span>}
                      </span>
                      <span className="text-[10px] flex-shrink-0">
                        <span className="text-emerald-400">+{f.additions}</span> <span className="text-red-400">-{f.deletions}</span>
                      </span>
                    </button>
                  );
                })}
              </div>
              <div className="flex-1 min-w-0 flex flex-col bg-[#1e1e1e]">
                {file && (
                  <div className="flex items-center gap-2 px-3 py-1 border-b border-zinc-700 text-xs text-zinc-300 flex-shrink-0">
                    <FileDiff className="w-3 h-3 flex-shrink-0" />
                    <span className="font-mono truncate">{file.previous_filename ? `${file.previous_filename} → ${file.filename}` : file.filename}</span>
                  </div>
                )}
                <div className="flex-1 min-h-0 overflow-auto">
                  {lines.length === 0 ? (
                    <div className="p-4 text-sm text-zinc-400">
                      Sem diff para exibir (arquivo binário, renomeado sem alterações ou grande demais para o GitHub). Use “Abrir no GitHub”.
                    </div>
                  ) : (
                    <pre className="font-mono text-[12.5px] leading-[1.45] py-1 min-w-max">
                      {lines.map((line, i) => (
                        <div key={i} className={`flex whitespace-pre ${lineClass(line.kind)}`}>
                          <span className="w-12 flex-shrink-0 select-none pr-2 text-right text-zinc-500" aria-hidden>{line.oldNo ?? ""}</span>
                          <span className="w-12 flex-shrink-0 select-none pr-2 text-right text-zinc-500 border-r border-zinc-700/60" aria-hidden>{line.newNo ?? ""}</span>
                          <span className="px-3">{line.text || " "}</span>
                        </div>
                      ))}
                    </pre>
                  )}
                </div>
              </div>
            </>
          )}
        </div>

        {data && (
          <div className="flex-shrink-0 border border-border/40 rounded">
            <button
              type="button"
              className="w-full flex items-center gap-2 px-3 py-1.5 text-xs hover:bg-muted/40"
              onClick={() => setRevOpen((o) => !o)}
            >
              <MessageSquare className="w-3.5 h-3.5" />
              <span className="font-medium">Revisão do PR</span>
              <div className="flex items-center gap-1.5 flex-wrap min-w-0">
                {rev?.reviews.map((r) => {
                  const st = REVIEW_STATE[r.state] ?? REVIEW_STATE.COMMENTED;
                  return (
                    <span key={r.user} className={`px-1.5 py-0.5 rounded border text-[10px] ${st.cls}`}
                      title={r.stale ? "Feita num commit anterior ao atual do PR" : `Em ${new Date(r.submitted_at).toLocaleString("pt-BR")}`}>
                      {r.user} {st.label}{r.stale ? " (commit antigo)" : ""}
                    </span>
                  );
                })}
                {rev && rev.reviews.length === 0 && <span className="text-muted-foreground">ninguém revisou ainda</span>}
                {revError && <span className="text-red-400 truncate" title={revError}>revisões indisponíveis</span>}
              </div>
              <div className="flex-1" />
              {revOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronUp className="w-3.5 h-3.5" />}
            </button>

            {revOpen && (
              <div className="border-t border-border/40 p-3 space-y-2 text-xs max-h-[38vh] overflow-y-auto">
                {revError && <div className="text-red-400 whitespace-pre-wrap break-words">{revError}</div>}

                {diffOutdated && (
                  <div className="flex items-center gap-2 rounded border border-amber-500/40 bg-amber-500/10 px-2 py-1.5 text-amber-300">
                    <AlertCircle className="w-3.5 h-3.5 flex-shrink-0" />
                    <span className="flex-1">O PR recebeu commits novos depois que este diff foi carregado. Recarregue o diff e revise o que mudou antes de enviar a revisão.</span>
                    <Button size="sm" variant="outline" className="h-6 text-xs gap-1" onClick={() => setReloadKey((k) => k + 1)}>
                      <RefreshCw className="w-3 h-3" />Recarregar
                    </Button>
                  </div>
                )}

                {rev && !rev.can_approve && rev.approve_block_reason && (
                  <div className="flex items-start gap-2 text-muted-foreground">
                    <AlertCircle className="w-3.5 h-3.5 flex-shrink-0 mt-px" />
                    <span>{rev.approve_block_reason}.</span>
                  </div>
                )}

                <div className="flex items-center gap-1.5 flex-wrap">
                  {EVENT_CHOICES.map((c) => {
                    const why = choiceBlocked(c.id);
                    const active = event === c.id;
                    return (
                      <button
                        key={c.id}
                        type="button"
                        disabled={!!why}
                        title={why || c.hint}
                        onClick={() => { setEvent(c.id); setSendError(""); }}
                        className={`inline-flex items-center gap-1 px-2 py-1 rounded border text-xs disabled:opacity-40 disabled:cursor-not-allowed ${
                          active
                            ? c.id === "APPROVE" ? "border-emerald-500 bg-emerald-500/15 text-emerald-300"
                              : c.id === "REQUEST_CHANGES" ? "border-red-500 bg-red-500/15 text-red-300"
                              : "border-primary bg-primary/10 text-foreground"
                            : "border-border/60 text-muted-foreground hover:text-foreground"
                        }`}
                      >
                        {c.id === "APPROVE" ? <CheckCircle2 className="w-3 h-3" /> : c.id === "REQUEST_CHANGES" ? <XCircle className="w-3 h-3" /> : <MessageSquare className="w-3 h-3" />}
                        {c.label}
                      </button>
                    );
                  })}
                </div>

                <textarea
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  rows={3}
                  placeholder={needsText ? "Escreva o comentário (obrigatório)…" : "Comentário opcional junto da aprovação…"}
                  className="w-full rounded border border-border/60 bg-background px-2 py-1.5 text-xs focus:outline-none focus:ring-1 focus:ring-primary/50"
                />

                {sendError && <div className="text-red-400 whitespace-pre-wrap break-words">{sendError}</div>}

                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">
                    Será enviada ao GitHub {rev?.viewer ? <>como <b>{rev.viewer}</b></> : "em nome do seu usuário"}, para o commit{" "}
                    <span className="font-mono">{reviewedSha ? reviewedSha.slice(0, 7) : "?"}</span> (o que este diff mostra).
                  </span>
                  <div className="flex-1" />
                  <Button size="sm" className="h-7 text-xs gap-1" disabled={!canSend} onClick={sendReview}
                    title={blocked || (diffOutdated ? "Recarregue o diff primeiro" : needsText && text.trim() === "" ? "Escreva o comentário" : "")}>
                    {sending && <Loader2 className="w-3 h-3 animate-spin" />}
                    {event === "APPROVE" ? "Aprovar PR" : event === "REQUEST_CHANGES" ? "Solicitar alterações" : "Enviar comentário"}
                  </Button>
                </div>
              </div>
            )}
          </div>
        )}

        {data && (
          <div className="flex-shrink-0 border border-border/40 rounded">
            <button type="button" className="w-full flex items-center gap-2 px-3 py-1.5 text-xs hover:bg-muted/40" onClick={() => setMergeOpen((o) => !o)}>
              <GitMerge className="w-3.5 h-3.5" />
              <span className="font-medium">Merge do PR</span>
              {mergedNow ? (
                <span className="px-1.5 py-0.5 rounded border text-[10px] text-purple-300 border-purple-500/40 bg-purple-500/10">mergeado</span>
              ) : mergeBlocked ? (
                <span className="text-muted-foreground truncate" title={mergeBlocked}>{mergeBlocked}</span>
              ) : (
                <span className="px-1.5 py-0.5 rounded border text-[10px] text-emerald-300 border-emerald-500/40 bg-emerald-500/10">pronto para mergear</span>
              )}
              <div className="flex-1" />
              {mergeOpen ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronUp className="w-3.5 h-3.5" />}
            </button>
            {mergeOpen && (
              <div className="border-t border-border/40 p-3 space-y-2 text-xs">
                {mergedNow ? (
                  <div className="text-purple-300">
                    PR #{data.pr.number} mergeado em {data.pr.base}.
                    {mergedNow.branchDeleted && " A branch de origem foi apagada."}
                    {mergedNow.branchError && <span className="text-amber-300"> A branch não foi apagada: {mergedNow.branchError}</span>}
                  </div>
                ) : (
                  <>
                    {mergeBlocked && (
                      <div className="flex items-center gap-2 rounded border border-amber-500/40 bg-amber-500/10 px-2 py-1.5 text-amber-300">
                        <AlertCircle className="w-3.5 h-3.5 flex-shrink-0" />
                        <span className="flex-1">{diffOutdated ? "O PR recebeu commits novos depois que este diff foi carregado: recarregue e revise antes de mergear." : mergeBlocked}</span>
                        <Button size="sm" variant="outline" className="h-6 text-xs gap-1" onClick={() => setReloadKey((k) => k + 1)}>
                          <RefreshCw className="w-3 h-3" />Atualizar
                        </Button>
                      </div>
                    )}
                    {!mergeBlocked && diffOutdated && (
                      <div className="flex items-center gap-2 rounded border border-amber-500/40 bg-amber-500/10 px-2 py-1.5 text-amber-300">
                        <AlertCircle className="w-3.5 h-3.5 flex-shrink-0" />
                        <span className="flex-1">O PR recebeu commits novos depois que este diff foi carregado: recarregue e revise antes de mergear.</span>
                        <Button size="sm" variant="outline" className="h-6 text-xs gap-1" onClick={() => setReloadKey((k) => k + 1)}>
                          <RefreshCw className="w-3 h-3" />Recarregar
                        </Button>
                      </div>
                    )}
                    <div className="flex items-center gap-3 flex-wrap">
                      <label className="flex items-center gap-1.5">
                        Método
                        <select value={method} onChange={(e) => setMethod(e.target.value as "merge" | "squash" | "rebase")}
                          className="rounded border border-border/60 bg-background px-1.5 py-1 text-xs">
                          <option value="merge">Merge commit</option>
                          <option value="squash">Squash</option>
                          <option value="rebase">Rebase</option>
                        </select>
                      </label>
                      <label className="flex items-center gap-1.5">
                        <input type="checkbox" checked={deleteBranch} onChange={(e) => setDeleteBranch(e.target.checked)} />
                        Apagar a branch de origem depois
                      </label>
                    </div>
                    {mergeError && <div className="text-red-400 whitespace-pre-wrap break-words">{mergeError}</div>}
                    <div className="flex items-center gap-2">
                      <span className="text-muted-foreground">
                        Será mergeado no GitHub {rev?.viewer ? <>como <b>{rev.viewer}</b></> : "em nome do seu usuário"}, com o commit{" "}
                        <span className="font-mono">{reviewedSha ? reviewedSha.slice(0, 7) : "?"}</span> (o que este diff mostra).
                      </span>
                      <div className="flex-1" />
                      <Button size="sm" className="h-7 text-xs gap-1" disabled={!canMerge} onClick={doMerge}
                        title={mergeBlocked || (diffOutdated ? "Recarregue o diff primeiro" : "")}>
                        {merging ? <Loader2 className="w-3 h-3 animate-spin" /> : <GitMerge className="w-3 h-3" />}
                        Mergear PR
                      </Button>
                    </div>
                  </>
                )}
              </div>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
