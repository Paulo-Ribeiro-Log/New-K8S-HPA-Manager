import { useState, useEffect, useCallback, useRef } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Badge } from "@/components/ui/badge";
import { Progress } from "@/components/ui/progress";
import { MonacoYamlEditor } from "@/components/MonacoYamlEditor";
import {
  Loader2,
  RotateCcw,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  Package,
  Database,
  GitBranch,
  Search,
} from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type {
  DeploymentManifest,
  DeploymentRevision,
  HelmRevisionEntry,
  HelmReleaseDetail,
  NexusBrowseItem,
} from "@/lib/api/types";

// ─── Rollback de Deployment — 3 modos ──────────────────────────────────────────
//
// Pedido explícito do usuário, com o passo a passo `kubectl rollout history/undo/status` como
// referência, mais duas opções adicionais: Helm (quando o Deployment é gerenciado por Helm — nunca
// usa o caminho cru, que causaria drift) e Nexus (values histórico publicado pelo pipeline de
// CI/CD, reaproveitando os mesmos endpoints já usados pela aba "Nexus Values").
//
// Detecção de modo: automática, a partir do manifest já carregado pela aba Deployments —
// `meta.helm.sh/release-name` + `app.kubernetes.io/managed-by: Helm` identificam um Deployment
// Helm-gerenciado (oferece Modo Helm e Modo Nexus, nunca o cru). Sem esses marcadores, só o Modo
// K8s nativo é oferecido — com aviso se detectar labels de GitOps conhecidos (Flux/ArgoCD), já que
// um reconcile automático pode reverter o rollback pouco depois.
//
// Segurança comum aos 3 modos: diff obrigatório antes de liberar a confirmação, motivo obrigatório
// (vira change-cause/anotação de auditoria), confirmação em 2 cliques (mesmo padrão já usado em
// SreApprovalButton.tsx — nunca um modal empilhado sobre modal), progresso via SSE nunca silencioso,
// nunca reverter pra um estado idêntico ao atual.

type RollbackMode = "helm" | "k8s" | "nexus";

const GITOPS_LABEL_MARKERS = [
  "kustomize.toolkit.fluxcd.io/name",
  "helm.toolkit.fluxcd.io/name",
  "argocd.argoproj.io/instance",
];

function formatDate(v?: string): string {
  if (!v) return "—";
  try {
    return new Date(v).toLocaleString("pt-BR", { day: "2-digit", month: "2-digit", year: "numeric", hour: "2-digit", minute: "2-digit" });
  } catch {
    return v;
  }
}

// compareVersionsDesc — ordena versões tipo "1.0.0-3"/"0.0.2-7" (formato real observado no Nexus
// desta empresa) da mais recente pra mais antiga comparando os segmentos numéricos, não como
// string pura (sort lexicográfico erraria "1.0.0-10" vindo ANTES de "1.0.0-2").
function compareVersionsDesc(a: string, b: string): number {
  const numsA = a.match(/\d+/g)?.map(Number) || [];
  const numsB = b.match(/\d+/g)?.map(Number) || [];
  const len = Math.max(numsA.length, numsB.length);
  for (let i = 0; i < len; i++) {
    const diff = (numsB[i] ?? 0) - (numsA[i] ?? 0);
    if (diff !== 0) return diff;
  }
  return b.localeCompare(a);
}

interface DeploymentRollbackModalProps {
  open: boolean;
  onClose: () => void;
  cluster: string;
  namespace: string;
  deploymentName: string;
  manifest: DeploymentManifest | null;
  canUpdateDeployment: boolean;
  onRolledBack: () => void; // dispara refresh do manifest/lista na aba Deployments
}

export function DeploymentRollbackModal({
  open, onClose, cluster, namespace, deploymentName, manifest, canUpdateDeployment, onRolledBack,
}: DeploymentRollbackModalProps) {
  const annotations = manifest?.metadata.annotations || {};
  const labels = manifest?.metadata.labels || {};
  const helmReleaseName = annotations["meta.helm.sh/release-name"] || "";
  const helmReleaseNamespace = annotations["meta.helm.sh/release-namespace"] || namespace;
  const isHelmManaged = !!helmReleaseName && labels["app.kubernetes.io/managed-by"] === "Helm";
  const gitopsMarker = GITOPS_LABEL_MARKERS.find((k) => labels[k]);

  const [mode, setMode] = useState<RollbackMode>("k8s");
  useEffect(() => {
    if (open) setMode(isHelmManaged ? "helm" : "k8s");
  }, [open, isHelmManaged]);

  const handleDone = useCallback(() => {
    onRolledBack();
    onClose();
  }, [onRolledBack, onClose]);

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <RotateCcw className="w-5 h-5" />
            Rollback: {deploymentName}
          </DialogTitle>
          <DialogDescription className="font-mono text-xs">{namespace} · {cluster}</DialogDescription>
        </DialogHeader>

        {gitopsMarker && (
          <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-sm text-amber-700 dark:text-amber-400 shrink-0">
            <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
            <span>
              Este Deployment parece ser gerenciado por GitOps (<span className="font-mono">{gitopsMarker}</span>) —
              um reconcile automático pode reverter este rollback pouco depois de aplicado. Considere pausar o
              controller antes de continuar, se possível.
            </span>
          </div>
        )}

        {isHelmManaged ? (
          <div className="flex items-center gap-1 border-b border-border shrink-0">
            <button
              type="button"
              onClick={() => setMode("helm")}
              className={`px-3 py-1.5 text-xs font-medium border-b-2 -mb-px flex items-center gap-1.5 ${mode === "helm" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}
            >
              <Package className="w-3.5 h-3.5" /> Helm (nativo)
            </button>
            <button
              type="button"
              onClick={() => setMode("nexus")}
              className={`px-3 py-1.5 text-xs font-medium border-b-2 -mb-px flex items-center gap-1.5 ${mode === "nexus" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}
            >
              <Database className="w-3.5 h-3.5" /> Nexus (values histórico)
            </button>
          </div>
        ) : (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground shrink-0">
            <GitBranch className="w-3.5 h-3.5" />
            Deployment não gerenciado pelo Helm — rollback via histórico de revisões do Kubernetes.
          </div>
        )}

        <div className="flex-1 overflow-auto min-h-0 -mx-1 px-1">
          {mode === "helm" && isHelmManaged && (
            <HelmRollbackSection
              cluster={cluster}
              release={helmReleaseName}
              releaseNamespace={helmReleaseNamespace}
              canUpdateDeployment={canUpdateDeployment}
              onDone={handleDone}
            />
          )}
          {mode === "k8s" && !isHelmManaged && (
            <NativeRollbackSection
              cluster={cluster}
              namespace={namespace}
              name={deploymentName}
              currentYaml={manifest?.yaml || ""}
              canUpdateDeployment={canUpdateDeployment}
              onDone={handleDone}
            />
          )}
          {mode === "nexus" && isHelmManaged && (
            <NexusRollbackSection
              cluster={cluster}
              release={helmReleaseName}
              releaseNamespace={helmReleaseNamespace}
              suggestedReleaseSearch={labels["app.kubernetes.io/name"] || helmReleaseName}
              canUpdateDeployment={canUpdateDeployment}
              onDone={handleDone}
            />
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Modo K8s nativo — equivalente a `kubectl rollout history/undo/status`
// ═══════════════════════════════════════════════════════════════════════════

function NativeRollbackSection({
  cluster, namespace, name, currentYaml, canUpdateDeployment, onDone,
}: {
  cluster: string; namespace: string; name: string; currentYaml: string; canUpdateDeployment: boolean; onDone: () => void;
}) {
  const [revisions, setRevisions] = useState<DeploymentRevision[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [target, setTarget] = useState<number | null>(null);
  const [preview, setPreview] = useState("");
  const [previewLoading, setPreviewLoading] = useState(false);
  const [reason, setReason] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [applying, setApplying] = useState(false);

  const progress = useRollbackProgress();

  const load = useCallback(() => {
    setLoading(true);
    setLoadError("");
    apiClient.listDeploymentRevisions(cluster, namespace, name)
      .then(setRevisions)
      .catch((err) => setLoadError(err instanceof Error ? err.message : "Erro ao carregar revisões"))
      .finally(() => setLoading(false));
  }, [cluster, namespace, name]);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    if (target == null) { setPreview(""); return; }
    setPreviewLoading(true);
    apiClient.previewDeploymentRevision(cluster, namespace, name, target)
      .then(setPreview)
      .catch((err) => toast.error("Erro ao gerar preview", { description: err instanceof Error ? err.message : "Erro" }))
      .finally(() => setPreviewLoading(false));
  }, [cluster, namespace, name, target]);

  const targetEntry = revisions.find((r) => r.revision === target);
  const canConfirm = target != null && reason.trim().length > 0 && !targetEntry?.isCurrent;

  const handleConfirm = async () => {
    if (target == null) return;
    setApplying(true);
    setConfirming(false);
    try {
      const { sessionId } = await apiClient.rollbackDeploymentNative(cluster, namespace, name, target, reason.trim());
      toast.success(`Revertido para a revisão ${target} — acompanhando o rollout...`);
      progress.start(apiClient.getDeploymentRollbackStreamURL(sessionId), onDone);
    } catch (err) {
      toast.error("Falha ao reverter", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setApplying(false);
    }
  };

  if (progress.active || progress.done) {
    return <RolloutProgressPanel progress={progress} />;
  }

  if (loading) {
    return <div className="flex items-center justify-center py-12 text-muted-foreground"><Loader2 className="w-5 h-5 animate-spin mr-2" /> Carregando histórico de revisões...</div>;
  }
  if (loadError) {
    return <div className="text-sm text-destructive py-8 text-center">{loadError}</div>;
  }
  if (revisions.length === 0) {
    return <div className="text-sm text-muted-foreground py-8 text-center">Nenhuma revisão anterior disponível (revisionHistoryLimit pode estar zerado, ou o Deployment nunca teve rollout).</div>;
  }

  return (
    <div className="space-y-4 py-2">
      <RadioGroup value={target?.toString() ?? ""} onValueChange={(v) => setTarget(parseInt(v, 10))} className="space-y-2">
        {revisions.map((r) => (
          <div key={r.revision} className={`flex items-start gap-3 p-3 border rounded-lg ${r.isCurrent ? "opacity-50" : "hover:bg-accent"}`}>
            <RadioGroupItem value={r.revision.toString()} id={`rev-${r.revision}`} disabled={r.isCurrent} className="mt-1" />
            <Label htmlFor={`rev-${r.revision}`} className={r.isCurrent ? "flex-1" : "flex-1 cursor-pointer"}>
              <div className="flex items-center justify-between mb-1">
                <span className="font-semibold">Revisão {r.revision} {r.isCurrent && <Badge variant="outline" className="ml-1 text-[10px]">atual</Badge>}</span>
                <span className="text-xs text-muted-foreground">{formatDate(r.createdAt)}</span>
              </div>
              <div className="text-xs font-mono text-muted-foreground truncate">{r.images.join(", ")}</div>
              {r.changeCause && <div className="text-xs text-muted-foreground mt-1">{r.changeCause}</div>}
              <div className="text-xs text-muted-foreground mt-0.5">{r.replicas} réplica(s) desejadas nessa revisão</div>
            </Label>
          </div>
        ))}
      </RadioGroup>

      {target != null && !targetEntry?.isCurrent && (
        <>
          <div>
            <Label className="text-xs text-muted-foreground mb-1 block">Diff — atual vs. revisão {target} (revise antes de confirmar)</Label>
            {previewLoading ? (
              <div className="flex items-center justify-center h-40 border rounded-md text-muted-foreground text-sm"><Loader2 className="w-4 h-4 animate-spin mr-2" /> Gerando preview...</div>
            ) : (
              <MonacoYamlEditor mode="diff" originalValue={currentYaml} value={preview} height={280} readOnly />
            )}
          </div>

          <div>
            <Label htmlFor="native-reason" className="text-xs text-muted-foreground mb-1 block">Motivo do rollback (obrigatório — vira annotation change-cause)</Label>
            <Textarea id="native-reason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Ex: instabilidade após deploy da revisão atual" rows={2} />
          </div>

          <div className="flex items-center gap-2 pt-2 border-t">
            {!confirming ? (
              <Button variant="default" disabled={!canConfirm || !canUpdateDeployment || applying} onClick={() => setConfirming(true)} title={!canUpdateDeployment ? "Sem permissão de escrita neste namespace (K8s RBAC)" : undefined}>
                <RotateCcw className="w-4 h-4 mr-2" /> Reverter para revisão {target}
              </Button>
            ) : (
              <>
                <span className="text-sm text-amber-600 dark:text-amber-400 flex items-center gap-1"><AlertTriangle className="w-4 h-4" /> Confirmar reversão para a revisão {target}?</span>
                <Button size="sm" onClick={handleConfirm} disabled={applying} className="bg-amber-600 hover:bg-amber-700">
                  {applying ? <Loader2 className="w-4 h-4 mr-1 animate-spin" /> : <CheckCircle2 className="w-4 h-4 mr-1" />} Sim, reverter
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirming(false)} disabled={applying}>
                  <XCircle className="w-4 h-4 mr-1" /> Cancelar
                </Button>
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Modo Helm — `helm rollback` nativo, reaproveita os mesmos endpoints já usados pela aba Helm
// ═══════════════════════════════════════════════════════════════════════════

function HelmRollbackSection({
  cluster, release, releaseNamespace, canUpdateDeployment, onDone,
}: {
  cluster: string; release: string; releaseNamespace: string; canUpdateDeployment: boolean; onDone: () => void;
}) {
  const [detail, setDetail] = useState<HelmReleaseDetail | null>(null);
  const [revisions, setRevisions] = useState<HelmRevisionEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [target, setTarget] = useState<number | null>(null);
  const [force, setForce] = useState(false);
  const [reason, setReason] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [applying, setApplying] = useState(false);

  const progress = useRollbackProgress();

  const load = useCallback(() => {
    setLoading(true);
    setLoadError("");
    Promise.all([
      apiClient.getHelmRelease(cluster, release, releaseNamespace),
      apiClient.getHelmHistory(cluster, release, releaseNamespace),
    ])
      .then(([d, h]) => { setDetail(d); setRevisions(h); })
      .catch((err) => setLoadError(err instanceof Error ? err.message : "Erro ao carregar histórico Helm"))
      .finally(() => setLoading(false));
  }, [cluster, release, releaseNamespace]);

  useEffect(() => { load(); }, [load]);

  const currentRevision = detail?.revision ?? 0;
  const availableRevisions = revisions.filter((r) => r.revision !== currentRevision).sort((a, b) => b.revision - a.revision);
  const canConfirm = target != null && reason.trim().length > 0;

  const handleConfirm = async () => {
    if (target == null) return;
    setApplying(true);
    setConfirming(false);
    try {
      const { operationId } = await apiClient.helmRollback(cluster, release, releaseNamespace, target, force);
      toast.success(`helm rollback iniciado para a revisão ${target} — acompanhando...`);
      progress.startHelm(operationId, onDone);
    } catch (err) {
      toast.error("Falha ao reverter release Helm", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setApplying(false);
    }
  };

  if (progress.active || progress.done) {
    return <RolloutProgressPanel progress={progress} />;
  }

  if (loading) {
    return <div className="flex items-center justify-center py-12 text-muted-foreground"><Loader2 className="w-5 h-5 animate-spin mr-2" /> Carregando histórico Helm...</div>;
  }
  if (loadError) {
    return <div className="text-sm text-destructive py-8 text-center">{loadError}</div>;
  }

  return (
    <div className="space-y-4 py-2">
      <div className="text-xs text-muted-foreground">
        Release <span className="font-mono">{release}</span> — revisão atual <span className="font-semibold">{currentRevision}</span>, chart <span className="font-mono">{detail?.chart}</span>
      </div>

      {availableRevisions.length === 0 ? (
        <div className="text-sm text-muted-foreground py-8 text-center">Nenhuma revisão anterior disponível no `helm history` deste release.</div>
      ) : (
        <RadioGroup value={target?.toString() ?? ""} onValueChange={(v) => setTarget(parseInt(v, 10))} className="space-y-2">
          {availableRevisions.map((r) => (
            <div key={r.revision} className="flex items-start gap-3 p-3 border rounded-lg hover:bg-accent">
              <RadioGroupItem value={r.revision.toString()} id={`helmrev-${r.revision}`} className="mt-1" />
              <Label htmlFor={`helmrev-${r.revision}`} className="flex-1 cursor-pointer">
                <div className="flex items-center justify-between mb-1">
                  <span className="font-semibold">Revisão {r.revision}</span>
                  <span className="text-xs text-muted-foreground">{formatDate(r.updatedAt)}</span>
                </div>
                <div className="text-xs text-muted-foreground">{r.description || "sem descrição"}</div>
                <div className="text-xs text-muted-foreground mt-0.5">Status: {r.status}</div>
              </Label>
            </div>
          ))}
        </RadioGroup>
      )}

      {target != null && (
        <>
          <div className="flex items-center gap-2">
            <input type="checkbox" id="helm-force" checked={force} onChange={(e) => setForce(e.target.checked)} className="rounded" />
            <Label htmlFor="helm-force" className="text-xs cursor-pointer">Forçar (recria recursos se necessário — mesma opção do `helm rollback --force`)</Label>
          </div>

          <div>
            <Label htmlFor="helm-reason" className="text-xs text-muted-foreground mb-1 block">Motivo do rollback (obrigatório)</Label>
            <Textarea id="helm-reason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Ex: instabilidade após deploy da revisão atual" rows={2} />
          </div>

          <div className="flex items-center gap-2 pt-2 border-t">
            {!confirming ? (
              <Button variant="default" disabled={!canConfirm || !canUpdateDeployment || applying} onClick={() => setConfirming(true)} title={!canUpdateDeployment ? "Sem permissão de escrita neste namespace (K8s RBAC)" : undefined}>
                <RotateCcw className="w-4 h-4 mr-2" /> helm rollback para revisão {target}
              </Button>
            ) : (
              <>
                <span className="text-sm text-amber-600 dark:text-amber-400 flex items-center gap-1"><AlertTriangle className="w-4 h-4" /> Confirmar `helm rollback {release} {target}`?</span>
                <Button size="sm" onClick={handleConfirm} disabled={applying} className="bg-amber-600 hover:bg-amber-700">
                  {applying ? <Loader2 className="w-4 h-4 mr-1 animate-spin" /> : <CheckCircle2 className="w-4 h-4 mr-1" />} Sim, reverter
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirming(false)} disabled={applying}>
                  <XCircle className="w-4 h-4 mr-1" /> Cancelar
                </Button>
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Modo Nexus — values histórico publicado pelo pipeline de CI/CD, aplicado via `helm upgrade`
// (mesmos endpoints da aba "Nexus Values" pra navegar/baixar; endpoint Helm Upgrade já existente
// pra aplicar). NÃO é uma reversão nativa do Helm — é um upgrade com valores antigos, por isso o
// aviso fixo e o diff obrigatório são ainda mais enfatizados aqui que nos outros 2 modos.
// ═══════════════════════════════════════════════════════════════════════════

// NEXUS_ROLLBACK_REPOSITORY — repositório Nexus DEDICADO a histórico de deploy nesta empresa,
// pedido explícito do usuário depois de relatar que a busca genérica (sem filtro de repositório)
// ficava confusa — sem esse filtro, a busca cruza TODOS os repositórios que as credenciais
// alcançam, misturando resultados sem relação nenhuma com rollback. Diferente do repositório
// default configurado no Perfil do Usuário (usado pela aba "Nexus Values" pra comparação livre de
// values entre versões/ambientes) — este é fixo, só pra este fluxo.
const NEXUS_ROLLBACK_REPOSITORY = "continuousdeploy-history";

function NexusRollbackSection({
  cluster, release, releaseNamespace, suggestedReleaseSearch, canUpdateDeployment, onDone,
}: {
  cluster: string; release: string; releaseNamespace: string; suggestedReleaseSearch: string; canUpdateDeployment: boolean; onDone: () => void;
}) {
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [item, setItem] = useState<NexusBrowseItem | null>(null);
  // candidates — só populado no caso raro de a busca automática achar MAIS de um nome batendo
  // (a busca do Nexus é por substring, não por igualdade exata).
  const [candidates, setCandidates] = useState<NexusBrowseItem[]>([]);
  const [manualSearchOpen, setManualSearchOpen] = useState(false);
  const [manualSearchTerm, setManualSearchTerm] = useState(suggestedReleaseSearch);

  const [selectedVersion, setSelectedVersion] = useState("");
  const [selectedFile, setSelectedFile] = useState("");

  const [currentValues, setCurrentValues] = useState("");
  const [nexusValues, setNexusValues] = useState("");
  const [fetchingValues, setFetchingValues] = useState(false);

  const [chartRef, setChartRef] = useState("");
  const [chartVersion, setChartVersion] = useState("");
  const [force, setForce] = useState(false);
  const [reason, setReason] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [applying, setApplying] = useState(false);

  const progress = useRollbackProgress();

  // Carrega valores/chart atuais do release Helm (pra diff + sugestão de chartRef/version).
  useEffect(() => {
    apiClient.getHelmRelease(cluster, release, releaseNamespace)
      .then((d) => {
        setCurrentValues(d.valuesRaw || "");
        if (d.chartMetadata?.name) setChartRef((v) => v || d.chartMetadata!.name);
        if (d.chartMetadata?.version) setChartVersion((v) => v || d.chartMetadata!.version);
      })
      .catch(() => { /* best-effort — diff/sugestão ficam vazios, não bloqueia o fluxo */ });
  }, [cluster, release, releaseNamespace]);

  const runSearch = useCallback((term: string) => {
    if (!term.trim()) return;
    setLoading(true);
    setLoadError("");
    setItem(null);
    setCandidates([]);
    setSelectedVersion("");
    setSelectedFile("");
    apiClient.nexusBrowse("", term.trim(), NEXUS_ROLLBACK_REPOSITORY)
      .then((res) => {
        const items = res.items || [];
        if (items.length === 0) {
          setLoadError(`Nenhuma versão encontrada no Nexus (repositório "${NEXUS_ROLLBACK_REPOSITORY}") para "${term.trim()}".`);
          return;
        }
        // Prioriza um match EXATO pelo nome — evita ambiguidade quando a busca (substring, via
        // Nexus /search) casa com nomes parecidos de outras aplicações.
        const exact = items.find((i) => i.name.toLowerCase() === term.trim().toLowerCase());
        if (exact) setItem(exact);
        else if (items.length === 1) setItem(items[0]);
        else setCandidates(items);
      })
      .catch((err) => setLoadError(err instanceof Error ? err.message : "Erro ao buscar no Nexus (verifique se está configurado no Perfil do Usuário)"))
      .finally(() => setLoading(false));
  }, []);

  // Busca automática ao abrir o modal — pedido explícito do usuário: listar direto as versões
  // possíveis, sem exigir buscar manualmente primeiro. Usa o nome do release Helm (identidade
  // mais confiável que já temos) como termo inicial.
  useEffect(() => { runSearch(release); }, [release, runSearch]);

  const sortedVersions = [...(item?.versions || [])].sort(compareVersionsDesc);
  const filesForVersion = item && selectedVersion ? (item.files?.[selectedVersion] || []) : [];

  // Auto-seleciona o arquivo quando só há UM pra essa versão — só pede escolha explícita quando
  // há ambiguidade real (ex: mais de um ambiente publicado na mesma versão).
  useEffect(() => {
    setSelectedFile(filesForVersion.length === 1 ? filesForVersion[0] : "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item, selectedVersion]);

  useEffect(() => {
    if (!item || !selectedVersion || !selectedFile) { setNexusValues(""); return; }
    setFetchingValues(true);
    apiClient.nexusDownloadValues({
      release: item.name,
      version: selectedVersion,
      repository: item.repository || NEXUS_ROLLBACK_REPOSITORY,
      filePath: `${item.name}/${selectedVersion}/${selectedFile}`,
    })
      .then((res) => {
        if (res.error) { toast.error("Nexus retornou erro", { description: res.error }); setNexusValues(""); return; }
        setNexusValues(res.content || "");
      })
      .catch((err) => toast.error("Erro ao baixar values do Nexus", { description: err instanceof Error ? err.message : "Erro" }))
      .finally(() => setFetchingValues(false));
  }, [item, selectedVersion, selectedFile]);

  const canConfirm = !!nexusValues && !!chartRef.trim() && reason.trim().length > 0;

  const handleConfirm = async () => {
    setApplying(true);
    setConfirming(false);
    try {
      const { operationId } = await apiClient.helmUpgrade(cluster, release, releaseNamespace, chartRef.trim(), chartVersion.trim(), nexusValues, force);
      toast.success("helm upgrade com values do Nexus iniciado — acompanhando...");
      progress.startHelm(operationId, onDone);
    } catch (err) {
      toast.error("Falha ao aplicar values do Nexus", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setApplying(false);
    }
  };

  if (progress.active || progress.done) {
    return <RolloutProgressPanel progress={progress} />;
  }

  return (
    <div className="space-y-4 py-2">
      <div className="flex items-start gap-2 rounded-md border border-blue-500/30 bg-blue-500/10 p-3 text-xs text-blue-700 dark:text-blue-400">
        <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
        <span>
          Este modo faz um <span className="font-mono">helm upgrade</span> usando um values.yaml histórico do Nexus — <strong>não</strong> é
          uma reversão nativa do Helm (<span className="font-mono">helm rollback</span>). Pode haver incompatibilidade entre o values antigo e o
          chart atual (chaves renomeadas/novas obrigatórias). Use quando o `helm history` já não tiver mais a revisão desejada.
        </span>
      </div>

      {loading && (
        <div className="flex items-center justify-center py-12 text-muted-foreground text-sm">
          <Loader2 className="w-5 h-5 animate-spin mr-2" /> Buscando versões no Nexus ({NEXUS_ROLLBACK_REPOSITORY})...
        </div>
      )}

      {!loading && loadError && (
        <div className="text-sm text-destructive py-2 text-center">{loadError}</div>
      )}

      {!loading && candidates.length > 0 && (
        <div>
          <Label className="text-xs text-muted-foreground mb-1 block">Mais de uma aplicação encontrada — selecione a correta</Label>
          <div className="flex flex-wrap gap-1.5">
            {candidates.map((c) => (
              <Badge key={c.path} variant="outline" className="cursor-pointer font-mono text-xs" onClick={() => { setItem(c); setCandidates([]); }}>
                {c.name}
              </Badge>
            ))}
          </div>
        </div>
      )}

      {!loading && item && sortedVersions.length > 0 && (
        <>
          <div className="text-xs text-muted-foreground">
            Aplicação <span className="font-mono font-semibold">{item.name}</span> — {sortedVersions.length} versão(ões) disponível(is)
          </div>
          <RadioGroup value={selectedVersion} onValueChange={setSelectedVersion} className="space-y-1.5 max-h-64 overflow-y-auto pr-1">
            {sortedVersions.map((v) => (
              <div key={v} className="flex items-center gap-3 p-2.5 border rounded-lg hover:bg-accent">
                <RadioGroupItem value={v} id={`nexusver-${v}`} />
                <Label htmlFor={`nexusver-${v}`} className="flex-1 cursor-pointer font-mono text-sm">{v}</Label>
              </div>
            ))}
          </RadioGroup>

          {selectedVersion && filesForVersion.length > 1 && (
            <div>
              <Label className="text-xs text-muted-foreground mb-1 block">Mais de um arquivo nessa versão — selecione qual usar</Label>
              <div className="flex flex-wrap gap-1.5">
                {filesForVersion.map((f) => (
                  <Badge key={f} variant={selectedFile === f ? "default" : "outline"} className="cursor-pointer font-mono text-xs" onClick={() => setSelectedFile(f)}>
                    {f}
                  </Badge>
                ))}
              </div>
            </div>
          )}
        </>
      )}

      {!loading && !manualSearchOpen && (
        <button type="button" className="text-xs text-primary underline underline-offset-2" onClick={() => setManualSearchOpen(true)}>
          Buscar por outro nome de aplicação
        </button>
      )}

      {manualSearchOpen && (
        <div className="flex items-end gap-2">
          <div className="flex-1">
            <Label htmlFor="nexus-manual-search" className="text-xs text-muted-foreground mb-1 block">Nome da aplicação no Nexus</Label>
            <Input id="nexus-manual-search" value={manualSearchTerm} onChange={(e) => setManualSearchTerm(e.target.value)} onKeyDown={(e) => e.key === "Enter" && runSearch(manualSearchTerm)} />
          </div>
          <Button variant="outline" onClick={() => runSearch(manualSearchTerm)} disabled={loading || !manualSearchTerm.trim()}>
            {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Search className="w-4 h-4" />}
          </Button>
        </div>
      )}

      {fetchingValues && <div className="flex items-center justify-center py-6 text-muted-foreground text-sm"><Loader2 className="w-4 h-4 animate-spin mr-2" /> Baixando values do Nexus...</div>}

      {nexusValues && (
        <>
          <div>
            <Label className="text-xs text-muted-foreground mb-1 block">Diff — values atual do release vs. values do Nexus (revise antes de confirmar)</Label>
            <MonacoYamlEditor mode="diff" originalValue={currentValues} value={nexusValues} height={240} readOnly />
          </div>

          <div className="grid grid-cols-2 gap-2">
            <div>
              <Label htmlFor="nexus-chartref" className="text-xs text-muted-foreground mb-1 block">Referência do chart (mesmo formato da aba Helm → Instalar/Atualizar)</Label>
              <Input id="nexus-chartref" value={chartRef} onChange={(e) => setChartRef(e.target.value)} placeholder="repo/nome-do-chart" className="font-mono text-xs" />
            </div>
            <div>
              <Label htmlFor="nexus-chartversion" className="text-xs text-muted-foreground mb-1 block">Versão do chart (opcional — vazio usa a mais recente)</Label>
              <Input id="nexus-chartversion" value={chartVersion} onChange={(e) => setChartVersion(e.target.value)} className="font-mono text-xs" />
            </div>
          </div>
          <p className="text-[11px] text-muted-foreground -mt-2">
            Pré-preenchido com o chart/versão atuais do release, quando disponíveis — o Helm não guarda de onde o chart original veio (repo/URL), então confirme antes de aplicar.
          </p>

          <div className="flex items-center gap-2">
            <input type="checkbox" id="nexus-force" checked={force} onChange={(e) => setForce(e.target.checked)} className="rounded" />
            <Label htmlFor="nexus-force" className="text-xs cursor-pointer">Forçar (recria recursos se necessário)</Label>
          </div>

          <div>
            <Label htmlFor="nexus-reason" className="text-xs text-muted-foreground mb-1 block">Motivo do rollback (obrigatório)</Label>
            <Textarea id="nexus-reason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Ex: versão X.Y do Nexus é a última confirmada estável" rows={2} />
          </div>

          <div className="flex items-center gap-2 pt-2 border-t">
            {!confirming ? (
              <Button variant="default" disabled={!canConfirm || !canUpdateDeployment || applying} onClick={() => setConfirming(true)} title={!canUpdateDeployment ? "Sem permissão de escrita neste namespace (K8s RBAC)" : undefined}>
                <RotateCcw className="w-4 h-4 mr-2" /> Aplicar values do Nexus
              </Button>
            ) : (
              <>
                <span className="text-sm text-amber-600 dark:text-amber-400 flex items-center gap-1"><AlertTriangle className="w-4 h-4" /> Confirmar helm upgrade com values antigos?</span>
                <Button size="sm" onClick={handleConfirm} disabled={applying} className="bg-amber-600 hover:bg-amber-700">
                  {applying ? <Loader2 className="w-4 h-4 mr-1 animate-spin" /> : <CheckCircle2 className="w-4 h-4 mr-1" />} Sim, aplicar
                </Button>
                <Button size="sm" variant="ghost" onClick={() => setConfirming(false)} disabled={applying}>
                  <XCircle className="w-4 h-4 mr-1" /> Cancelar
                </Button>
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Progresso via SSE — compartilhado pelos 3 modos (fontes de evento diferentes: stream próprio de
// rollout do Modo K8s nativo vs. stream de operação Helm dos Modos Helm/Nexus — mesma UI).
// ═══════════════════════════════════════════════════════════════════════════

interface RollbackProgressState {
  active: boolean;
  done: boolean;
  failed: boolean;
  percent: number;
  message: string;
}

function useRollbackProgress() {
  const [state, setState] = useState<RollbackProgressState>({ active: false, done: false, failed: false, percent: 0, message: "" });
  const esRef = useRef<EventSource | null>(null);

  const start = useCallback((url: string, onSuccess: () => void) => {
    esRef.current?.close();
    setState({ active: true, done: false, failed: false, percent: 5, message: "Iniciando..." });
    const es = new EventSource(url);
    esRef.current = es;
    es.onmessage = (e) => {
      try {
        const evt = JSON.parse(e.data);
        setState({ active: evt.type !== "complete" && evt.type !== "error", done: evt.type === "complete", failed: evt.type === "error", percent: Math.round((evt.progress || 0) * 100), message: evt.message || "" });
        if (evt.type === "complete") { es.close(); onSuccess(); }
        if (evt.type === "error") es.close();
      } catch { /* ignora frame malformado */ }
    };
    es.onerror = () => { es.close(); };
  }, []);

  const startHelm = useCallback((operationId: string, onSuccess: () => void) => {
    esRef.current?.close();
    setState({ active: true, done: false, failed: false, percent: 5, message: "Iniciando..." });
    const es = new EventSource(apiClient.getHelmOperationStreamURL(operationId));
    esRef.current = es;
    es.addEventListener("helm-operation", (e) => {
      try {
        const evt = JSON.parse((e as MessageEvent).data);
        const failed = evt.phase === "failed" || !!evt.error;
        const done = evt.phase === "succeeded";
        setState({ active: !done && !failed, done, failed, percent: done ? 100 : failed ? 100 : 60, message: evt.message || evt.error || "" });
        if (done) { es.close(); onSuccess(); }
        if (failed) es.close();
      } catch { /* ignora frame malformado */ }
    });
    es.onerror = () => { es.close(); };
  }, []);

  useEffect(() => () => { esRef.current?.close(); }, []);

  return { ...state, start, startHelm };
}

function RolloutProgressPanel({ progress }: { progress: RollbackProgressState }) {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-16">
      {progress.failed ? (
        <XCircle className="w-10 h-10 text-destructive" />
      ) : progress.done ? (
        <CheckCircle2 className="w-10 h-10 text-green-500" />
      ) : (
        <Loader2 className="w-10 h-10 animate-spin text-primary" />
      )}
      <div className="w-full max-w-sm">
        <Progress value={progress.percent} className={progress.failed ? "[&>div]:bg-destructive" : progress.done ? "[&>div]:bg-green-500" : undefined} />
      </div>
      <p className={`text-sm text-center max-w-md ${progress.failed ? "text-destructive" : "text-muted-foreground"}`}>{progress.message || "Aguardando..."}</p>
    </div>
  );
}
