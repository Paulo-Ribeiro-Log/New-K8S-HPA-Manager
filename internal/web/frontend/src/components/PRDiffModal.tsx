import { useEffect, useMemo, useState } from "react";
import { AlertCircle, Copy, ExternalLink, FileDiff, GitPullRequest, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { apiClient, type CodeEditorPRDiffResponse } from "@/lib/api/client";

// Diff do PR da branch atual (somente leitura): arquivos alterados à esquerda e o patch do
// arquivo selecionado à direita, como na aba "Files changed" do GitHub.

interface PRDiffModalProps {
  repoId: string;
  profileId?: string;
  /** Número do PR; sem ele, usa o PR aberto mais recente da branch atual. */
  prNumber?: number;
  onClose: () => void;
}

const STATUS_BADGE: Record<string, { label: string; cls: string }> = {
  added: { label: "A", cls: "text-emerald-400" },
  modified: { label: "M", cls: "text-amber-300" },
  removed: { label: "D", cls: "text-red-400" },
  renamed: { label: "R", cls: "text-sky-400" },
};

function lineClass(line: string): string {
  if (line.startsWith("@@")) return "text-sky-300 bg-sky-500/10";
  if (line.startsWith("+")) return "text-emerald-300 bg-emerald-500/10";
  if (line.startsWith("-")) return "text-red-300 bg-red-500/10";
  return "text-zinc-300";
}

export function PRDiffModal({ repoId, profileId, prNumber, onClose }: PRDiffModalProps) {
  const [data, setData] = useState<CodeEditorPRDiffResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [idx, setIdx] = useState(0);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    apiClient
      .codeEditorGetPRDiff(repoId, prNumber, profileId)
      .then((r) => { if (!cancelled) { setData(r); setIdx(0); } })
      .catch((e) => { if (!cancelled) setError(e instanceof Error ? e.message : "Erro ao buscar o diff do PR"); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [repoId, prNumber, profileId]);

  const files = data?.files ?? [];
  const file = files[idx];
  const lines = useMemo(() => (file?.patch ? file.patch.split("\n") : []), [file]);
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
                        <div key={i} className={`whitespace-pre px-3 ${lineClass(line)}`}>{line || " "}</div>
                      ))}
                    </pre>
                  )}
                </div>
              </div>
            </>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
