import { useState, useEffect, useCallback, useRef, useMemo } from "react";
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
  AlertOctagon,
  RefreshCw,
  CheckCircle2,
  XCircle,
  Package,
  Database,
  GitBranch,
  Search,
  Image,
  Download,
  FolderOpen,
  FolderSearch,
  Pencil,
  Save,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import yaml from "js-yaml";
import { apiClient } from "@/lib/api/client";
import type {
  DeploymentManifest,
  DeploymentRevision,
  DeploymentRuntimeInsights,
  HelmRevisionEntry,
  HelmReleaseDetail,
  NexusFlatArtifact,
  RollbackFileEntry,
} from "@/lib/api/types";

// ─── Rollback de Deployment — 5 modos ──────────────────────────────────────────
//
// Pedido explícito do usuário, com o passo a passo `kubectl rollout history/undo/status` como
// referência, mais quatro opções adicionais: Helm (quando o Deployment é gerenciado por Helm — nunca
// usa o caminho cru, que causaria drift), Nexus (manifesto histórico publicado pelo pipeline de
// CI/CD, ver NexusRollbackSection), Imagem (troca só a tag no Deployment ao vivo, sem tocar em
// mais nada do manifesto — item 1 do procedimento interno de rollback manual desta empresa, o
// método mais simples/rápido, só válido quando NENHUM manifesto foi alterado desde a versão-alvo) e
// Arquivos (rollback manual a partir de um YAML já salvo — pasta gerenciada de artefatos baixados
// do Nexus ou qualquer outro diretório do servidor, ver FileRollbackSection).
//
// Detecção de modo: automática, a partir do manifest já carregado pela aba Deployments —
// `meta.helm.sh/release-name` + `app.kubernetes.io/managed-by: Helm` identificam um Deployment
// Helm-gerenciado (oferece Modo Helm e Modo Nexus, nunca os outros 2 — evita drift). Sem esses
// marcadores, oferece Modo K8s nativo e Modo Imagem — com aviso se detectar labels de GitOps
// conhecidos (Flux/ArgoCD), já que um reconcile automático pode reverter o rollback pouco depois.
// Modo Arquivos é o único disponível nos dois casos (Helm-gerenciado ou não) — a escolha do arquivo
// é sempre manual e explícita, com aviso de drift condicional quando Helm-gerenciado.
//
// Segurança comum aos 5 modos: diff obrigatório antes de liberar a confirmação (Modo Imagem usa um
// diff textual simples do campo image, não um YAML completo — não há mais nada mudando), motivo
// obrigatório (vira change-cause/anotação de auditoria), confirmação em 2 cliques (mesmo padrão já
// usado em SreApprovalButton.tsx — nunca um modal empilhado sobre modal), progresso via SSE nunca
// silencioso, nunca reverter pra um estado idêntico ao atual.

type RollbackMode = "helm" | "k8s" | "nexus" | "image" | "files";

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

// formatVersion — mesma lógica de DeploymentsTab.tsx (não exportada de lá, duplicada aqui de
// propósito: função pequena, autocontida, mesmo padrão de pequenos helpers locais já usado em
// vários arquivos deste projeto): "1-0-1-13" (formato de label K8s) → "1.0.1-13" (leitura semver).
function formatVersion(version: string | undefined): string {
  if (!version) return "";
  const parts = version.split("-");
  if (parts.length === 4 && parts.every((p) => /^\d+$/.test(p))) return `${parts[0]}.${parts[1]}.${parts[2]}-${parts[3]}`;
  return version;
}

// revisionWasCreatedByRestart — achado real validando ao vivo contra ms-faturamento-nf-legado: a
// annotation kubectl.kubernetes.io/restartedAt NÃO é limpa pelo K8s quando um deploy normal
// acontece depois de um restart — ela só é SOBRESCRITA por um restart novo, então uma revisão
// criada por um deploy de imagem real pode carregar (e mostrar) o restartedAt de uma revisão
// restart-only anterior, sem nenhuma relação com a criação dela mesma (confirmado: a revisão 4
// desse Deployment real tinha restartedAt herdado da revisão 3, criada ~48 dias antes). Só
// consideramos "esta revisão foi criada por um restart" quando restartedAt e createdAt praticamente
// coincidem — o controller cria o ReplicaSet segundos depois do patch que grava a annotation.
function revisionWasCreatedByRestart(restartedAt?: string, createdAt?: string): boolean {
  if (!restartedAt || !createdAt) return false;
  const diffMs = Math.abs(new Date(restartedAt).getTime() - new Date(createdAt).getTime());
  return diffMs < 60_000;
}

// extractDeploymentDoc — o artefato baixado do Nexus (continuousdeploy-history) é o manifesto K8s
// INTEIRO já renderizado (multi-documento: ConfigMap+Service+Deployment+Ingress, confirmado ao
// vivo contra um artefato real desta empresa) — não um values.yaml. Extrai só o documento
// `kind: Deployment`, que é o único escopo deste modal (rollback de UM Deployment, nunca dos
// recursos vizinhos empacotados junto no mesmo snapshot).
function extractDeploymentDoc(multiDocYaml: string): { yaml: string; error?: string } {
  let docs: unknown[];
  try {
    docs = yaml.loadAll(multiDocYaml);
  } catch (err) {
    return { yaml: "", error: err instanceof Error ? err.message : "YAML inválido" };
  }
  const deploymentDoc = docs.find((d) => d && typeof d === "object" && (d as Record<string, unknown>).kind === "Deployment");
  if (!deploymentDoc) {
    return { yaml: "", error: "Este artefato não contém um documento kind: Deployment (só " + docs.map((d) => (d && typeof d === "object" ? (d as Record<string, unknown>).kind : "?")).join(", ") + ")" };
  }
  return { yaml: yaml.dump(deploymentDoc) };
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
  // appVersion — mesma convenção/labels já usados no header do painel de visualização
  // (DeploymentsTab.tsx) — pedido explícito do usuário pra também aparecer aqui no header do
  // modal de Rollback, onde é ainda mais relevante (contexto de "de qual versão estou saindo").
  const appVersion = labels["app.kubernetes.io/version"] || labels["version"] || labels["app.version"];
  const helmReleaseName = annotations["meta.helm.sh/release-name"] || "";
  const helmReleaseNamespace = annotations["meta.helm.sh/release-namespace"] || namespace;
  const isHelmManaged = !!helmReleaseName && labels["app.kubernetes.io/managed-by"] === "Helm";
  const gitopsMarker = GITOPS_LABEL_MARKERS.find((k) => labels[k]);

  const [mode, setMode] = useState<RollbackMode>("k8s");
  useEffect(() => {
    if (open) setMode(isHelmManaged ? "helm" : "k8s");
  }, [open, isHelmManaged]);

  // Insights sob demanda (último kubectl rollout restart + rotas Service/Ingress sem endpoint
  // pronto) — pedido explícito do usuário depois de uma investigação real (ms-faturamento-nf-legado:
  // spec.replicas=0 há anos, Service/Ingress "fachada" ainda apontando pra ele). Independente do
  // modo escolhido — reflete o estado ATUAL do Deployment, relevante nos 3 modos.
  const [insights, setInsights] = useState<DeploymentRuntimeInsights | null>(null);
  useEffect(() => {
    if (!open) { setInsights(null); return; }
    apiClient.getDeploymentInsights(cluster, namespace, deploymentName)
      .then(setInsights)
      .catch(() => setInsights(null)); // best-effort — nunca bloqueia o fluxo de rollback
  }, [open, cluster, namespace, deploymentName]);

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
          <DialogDescription className="font-mono text-xs">
            {namespace} · {cluster}
            {appVersion && <> · versão atual: <span className="text-primary">{formatVersion(appVersion)}</span></>}
          </DialogDescription>
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

        {insights?.danglingRoutes && insights.danglingRoutes.length > 0 && (
          <div className="flex items-start gap-2 rounded-md border border-red-500/30 bg-red-500/10 p-3 text-sm text-red-700 dark:text-red-400 shrink-0">
            <AlertOctagon className="w-4 h-4 mt-0.5 shrink-0" />
            <div>
              <p className="font-medium">Rota sem backend — nenhum pod respondendo atrás deste Deployment</p>
              {insights.danglingRoutes.map((r) => (
                <p key={r.serviceName} className="text-xs font-mono mt-0.5">
                  Service <span className="font-semibold">{r.serviceName}</span>
                  {r.hosts && r.hosts.length > 0 && <> — {r.hosts.join(", ")}</>}
                </p>
              ))}
            </div>
          </div>
        )}

        {insights?.restartedAt && (
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground shrink-0">
            <RefreshCw className="w-3.5 h-3.5" />
            Último restart (kubectl rollout restart): {formatDate(insights.restartedAt)}
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
              <Database className="w-3.5 h-3.5" /> Nexus (manifesto histórico)
            </button>
            <button
              type="button"
              onClick={() => setMode("files")}
              className={`px-3 py-1.5 text-xs font-medium border-b-2 -mb-px flex items-center gap-1.5 ${mode === "files" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}
            >
              <FolderOpen className="w-3.5 h-3.5" /> Arquivos (manual)
            </button>
          </div>
        ) : (
          <div className="flex items-center gap-1 border-b border-border shrink-0">
            <button
              type="button"
              onClick={() => setMode("k8s")}
              className={`px-3 py-1.5 text-xs font-medium border-b-2 -mb-px flex items-center gap-1.5 ${mode === "k8s" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}
            >
              <GitBranch className="w-3.5 h-3.5" /> K8s nativo (revisões)
            </button>
            <button
              type="button"
              onClick={() => setMode("image")}
              className={`px-3 py-1.5 text-xs font-medium border-b-2 -mb-px flex items-center gap-1.5 ${mode === "image" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}
            >
              <Image className="w-3.5 h-3.5" /> Imagem (Harbor)
            </button>
            <button
              type="button"
              onClick={() => setMode("files")}
              className={`px-3 py-1.5 text-xs font-medium border-b-2 -mb-px flex items-center gap-1.5 ${mode === "files" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}
            >
              <FolderOpen className="w-3.5 h-3.5" /> Arquivos (manual)
            </button>
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
              namespace={namespace}
              deploymentName={deploymentName}
              currentYaml={manifest?.yaml || ""}
              suggestedReleaseSearch={labels["app.kubernetes.io/name"] || helmReleaseName}
              canUpdateDeployment={canUpdateDeployment}
              onDone={handleDone}
            />
          )}
          {mode === "image" && !isHelmManaged && (
            <ImageRollbackSection
              cluster={cluster}
              namespace={namespace}
              deploymentName={deploymentName}
              currentYaml={manifest?.yaml || ""}
              canUpdateDeployment={canUpdateDeployment}
              onDone={handleDone}
            />
          )}
          {mode === "files" && (
            <FileRollbackSection
              cluster={cluster}
              namespace={namespace}
              deploymentName={deploymentName}
              currentYaml={manifest?.yaml || ""}
              isHelmManaged={isHelmManaged}
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
              {revisionWasCreatedByRestart(r.restartedAt, r.createdAt) && (
                <div className="flex items-center gap-1 text-xs text-muted-foreground mt-0.5">
                  <RefreshCw className="w-3 h-3" /> Criada por rollout restart
                </div>
              )}
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
// Modo Imagem — troca só a tag da imagem de um ou mais containers (equivalente a `kubectl set
// image`), sem tocar em mais nada do manifesto. Item 1 do procedimento interno de rollback manual
// desta empresa, o método mais simples/rápido dos 4 modos — mas só válido quando NENHUM outro
// manifesto (ConfigMap/Ingress/Deployment/Service) foi alterado desde a versão-alvo. Nunca
// oferecido pra Deployments Helm-gerenciados (mesmo risco de drift já documentado pro Modo K8s
// nativo — a doc interna trata um caso especial pra 1º deploy Helm via `--reuse-values --set
// deployment.image.tag`, mas esse caminho assume o schema de values do chart `convair-helm`
// específico desta empresa, não generalizável a qualquer chart — deliberadamente não implementado).
// ═══════════════════════════════════════════════════════════════════════════

interface ParsedContainer {
  name: string;
  image: string;
}

// parseContainersFromYaml — lê spec.template.spec.containers do YAML do Deployment ATUAL (já
// single-documento, diferente do multi-documento do Modo Nexus) via js-yaml, mesma dependência já
// usada em extractDeploymentDoc.
function parseContainersFromYaml(currentYaml: string): ParsedContainer[] {
  try {
    const doc = yaml.load(currentYaml) as { spec?: { template?: { spec?: { containers?: unknown[] } } } } | undefined;
    const containers = doc?.spec?.template?.spec?.containers;
    if (!Array.isArray(containers)) return [];
    return containers
      .filter((c): c is { name: string; image: string } => !!c && typeof c === "object" && typeof (c as Record<string, unknown>).name === "string" && typeof (c as Record<string, unknown>).image === "string")
      .map((c) => ({ name: c.name, image: c.image }));
  } catch {
    return [];
  }
}

function ImageRollbackSection({
  cluster, namespace, deploymentName, currentYaml, canUpdateDeployment, onDone,
}: {
  cluster: string; namespace: string; deploymentName: string; currentYaml: string; canUpdateDeployment: boolean; onDone: () => void;
}) {
  const containers = useMemo(() => parseContainersFromYaml(currentYaml), [currentYaml]);
  const [newImages, setNewImages] = useState<Record<string, string>>({});
  const [reason, setReason] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [applying, setApplying] = useState(false);

  const progress = useRollbackProgress();

  // Inicializa os campos com a imagem atual — reseta se o manifesto mudar (troca de Deployment).
  useEffect(() => {
    const initial: Record<string, string> = {};
    containers.forEach((c) => { initial[c.name] = c.image; });
    setNewImages(initial);
  }, [containers]);

  const changedImages = useMemo(() => {
    const changed: Record<string, string> = {};
    containers.forEach((c) => {
      const v = (newImages[c.name] ?? "").trim();
      if (v && v !== c.image) changed[c.name] = v;
    });
    return changed;
  }, [containers, newImages]);

  const canConfirm = Object.keys(changedImages).length > 0 && reason.trim().length > 0;

  const handleConfirm = async () => {
    setApplying(true);
    setConfirming(false);
    try {
      const { sessionId } = await apiClient.setDeploymentImage(cluster, namespace, deploymentName, changedImages, reason.trim());
      toast.success("Imagem revertida — acompanhando o rollout...");
      progress.start(apiClient.getDeploymentRollbackStreamURL(sessionId), onDone);
    } catch (err) {
      toast.error("Falha ao trocar a imagem", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setApplying(false);
    }
  };

  if (progress.active || progress.done) {
    return <RolloutProgressPanel progress={progress} />;
  }

  if (containers.length === 0) {
    return <div className="text-sm text-destructive py-8 text-center">Não foi possível ler os containers do manifesto atual.</div>;
  }

  return (
    <div className="space-y-4 py-2">
      <div className="flex items-start gap-2 rounded-md border border-blue-500/30 bg-blue-500/10 p-3 text-xs text-blue-700 dark:text-blue-400">
        <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
        <span>
          Este modo troca <strong>só a imagem</strong> do(s) container(s) abaixo — equivalente a <span className="font-mono">kubectl set image</span>.
          Só use quando tiver certeza de que <strong>nenhum outro manifesto</strong> (ConfigMap/Ingress/Deployment/Service) mudou desde a
          versão pra qual está revertendo — caso contrário, use o Modo K8s nativo (revisões completas).
        </span>
      </div>

      <div className="space-y-3">
        {containers.map((c) => (
          <div key={c.name} className="space-y-1">
            <Label htmlFor={`img-${c.name}`} className="text-xs text-muted-foreground block">
              Container <span className="font-mono font-semibold text-foreground">{c.name}</span>
            </Label>
            <div className="text-[11px] font-mono text-muted-foreground truncate">Atual: {c.image}</div>
            <Input
              id={`img-${c.name}`}
              value={newImages[c.name] ?? ""}
              onChange={(e) => setNewImages((prev) => ({ ...prev, [c.name]: e.target.value }))}
              placeholder={c.image}
              className="font-mono text-xs"
            />
          </div>
        ))}
      </div>

      {Object.keys(changedImages).length > 0 && (
        <div>
          <Label className="text-xs text-muted-foreground mb-1 block">Mudanças a aplicar</Label>
          <div className="space-y-1 rounded-md border p-2.5 text-xs">
            {Object.entries(changedImages).map(([name, image]) => {
              const original = containers.find((c) => c.name === name)?.image ?? "";
              return (
                <div key={name} className="font-mono">
                  <span className="text-muted-foreground">{name}:</span>{" "}
                  <span className="text-red-500 line-through">{original}</span>{" "}
                  → <span className="text-green-600 dark:text-green-400">{image}</span>
                </div>
              );
            })}
          </div>
        </div>
      )}

      <div>
        <Label htmlFor="image-reason" className="text-xs text-muted-foreground mb-1 block">Motivo do rollback (obrigatório — vira annotation change-cause)</Label>
        <Textarea id="image-reason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Ex: revertendo para a tag estável anterior via Harbor" rows={2} />
      </div>

      <div className="flex items-center gap-2 pt-2 border-t">
        {!confirming ? (
          <Button variant="default" disabled={!canConfirm || !canUpdateDeployment || applying} onClick={() => setConfirming(true)} title={!canUpdateDeployment ? "Sem permissão de escrita neste namespace (K8s RBAC)" : undefined}>
            <RotateCcw className="w-4 h-4 mr-2" /> Aplicar troca de imagem
          </Button>
        ) : (
          <>
            <span className="text-sm text-amber-600 dark:text-amber-400 flex items-center gap-1"><AlertTriangle className="w-4 h-4" /> Confirmar troca de imagem?</span>
            <Button size="sm" onClick={handleConfirm} disabled={applying} className="bg-amber-600 hover:bg-amber-700">
              {applying ? <Loader2 className="w-4 h-4 mr-1 animate-spin" /> : <CheckCircle2 className="w-4 h-4 mr-1" />} Sim, aplicar
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setConfirming(false)} disabled={applying}>
              <XCircle className="w-4 h-4 mr-1" /> Cancelar
            </Button>
          </>
        )}
      </div>
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
  // wait/recreatePods — pedido explícito do usuário depois de analisar a documentação interna de
  // rollback manual desta empresa, que recomenda sempre `helm rollback ... --wait --force
  // --recreate-pods` no cenário de emergência (rollback automático indisponível/falhou). Nenhum
  // dos dois era passado antes — `buildRollbackArgs` (internal/pkg/helm/cli_client.go) só
  // suportava --force. Desligados por padrão (mesmo padrão de `force`, opt-in explícito) — o
  // usuário liga quando está de fato no cenário de emergência descrito na documentação.
  const [wait, setWait] = useState(false);
  const [recreatePods, setRecreatePods] = useState(false);
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
      const { operationId } = await apiClient.helmRollback(cluster, release, releaseNamespace, target, force, wait, recreatePods);
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
          <div className="space-y-1.5">
            <div className="flex items-center gap-2">
              <input type="checkbox" id="helm-force" checked={force} onChange={(e) => setForce(e.target.checked)} className="rounded" />
              <Label htmlFor="helm-force" className="text-xs cursor-pointer">Forçar (recria recursos se necessário — <span className="font-mono">--force</span>)</Label>
            </div>
            <div className="flex items-center gap-2">
              <input type="checkbox" id="helm-wait" checked={wait} onChange={(e) => setWait(e.target.checked)} className="rounded" />
              <Label htmlFor="helm-wait" className="text-xs cursor-pointer">Aguardar pods ficarem prontos antes de reportar sucesso (<span className="font-mono">--wait</span>)</Label>
            </div>
            <div className="flex items-center gap-2">
              <input type="checkbox" id="helm-recreate-pods" checked={recreatePods} onChange={(e) => setRecreatePods(e.target.checked)} className="rounded" />
              <Label htmlFor="helm-recreate-pods" className="text-xs cursor-pointer">Recriar pods mesmo sem mudança de template (<span className="font-mono">--recreate-pods</span>)</Label>
            </div>
            <p className="text-[11px] text-muted-foreground">
              As 3 opções acima juntas são a recomendação do procedimento interno de rollback manual desta empresa pro cenário de emergência (rollback automático indisponível/falhou).
            </p>
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
// Modo Nexus — manifesto histórico publicado pelo pipeline de CI/CD, aplicado via kubectl apply
// (reaproveita apiClient.applyDeployment, o mesmo endpoint genérico usado pelo botão "Aplicar" do
// editor YAML normal da aba Deployments).
//
// Achado real, crítico, que mudou o desenho original deste modo (o usuário compartilhou o trecho
// real do pipeline Jenkins desta empresa que publica esses artefatos): o arquivo publicado em
// "continuousdeploy-history" NÃO é um values.yaml — é o manifesto K8s INTEIRO já renderizado
// (multi-documento: ConfigMap+Service+Deployment+Ingress, confirmado baixando um artefato real —
// é literalmente o `deploy.yaml` que o pipeline aplicou naquele deploy). A 1ª versão deste modo
// tratava o conteúdo como values pra um `helm upgrade`, o que teria aplicado um manifesto K8s
// inteiro como se fosse a seção `values:` de um chart — falharia ou corromperia o release.
// Corrigido: extrai só o documento `kind: Deployment` (extractDeploymentDoc) e aplica via
// apiClient.applyDeployment — mesmo mecanismo do Modo K8s nativo, mas usando o Nexus como fonte de
// manifesto histórico em vez do ReplicaSet ao vivo (útil quando revisionHistoryLimit já podou a
// revisão desejada do próprio cluster). Nunca reaplica ConfigMap/Service/Ingress do mesmo
// snapshot — fora do escopo deste modal, que é rollback de UM Deployment.
//
// O repositório em si TAMBÉM é diferente do que BrowseRepository (aba Nexus Values) espera:
// "continuousdeploy-history" é achatado — cada componente é um arquivo solto na raiz (nome
// sanitizado com timestamp+versão embutidos), sem hierarquia release/version/arquivo nenhuma.
// BrowseRepository descartava esses componentes em silêncio — corrigido com SearchFlatArtifacts.
//
// Sem streaming de progresso de rollout (diferente do Modo K8s nativo): apiClient.applyDeployment
// é síncrono e não abre sessão SSE — mesmo comportamento do botão "Aplicar" normal da aba.
// ═══════════════════════════════════════════════════════════════════════════

// NEXUS_ROLLBACK_REPOSITORY — repositório Nexus DEDICADO a histórico de deploy nesta empresa,
// pedido explícito do usuário depois de relatar que a busca genérica (sem filtro de repositório)
// ficava confusa — sem esse filtro, a busca cruza TODOS os repositórios que as credenciais
// alcançam, misturando resultados sem relação nenhuma com rollback. Diferente do repositório
// default configurado no Perfil do Usuário (usado pela aba "Nexus Values" pra comparação livre de
// values entre versões/ambientes) — este é fixo, só pra este fluxo.
const NEXUS_ROLLBACK_REPOSITORY = "continuousdeploy-history";

function NexusRollbackSection({
  cluster, namespace, deploymentName, currentYaml, suggestedReleaseSearch, canUpdateDeployment, onDone,
}: {
  cluster: string; namespace: string; deploymentName: string; currentYaml: string; suggestedReleaseSearch: string; canUpdateDeployment: boolean; onDone: () => void;
}) {
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [artifacts, setArtifacts] = useState<NexusFlatArtifact[]>([]);
  const [selectedArtifact, setSelectedArtifact] = useState<NexusFlatArtifact | null>(null);

  const [manualSearchOpen, setManualSearchOpen] = useState(false);
  const [manualSearchTerm, setManualSearchTerm] = useState(suggestedReleaseSearch);
  // Busca automática (ao abrir o modal) continua restrita a NEXUS_ROLLBACK_REPOSITORY — pedido
  // explícito anterior do usuário, pra não misturar resultados de repositórios sem relação com
  // rollback. Busca MANUAL ganha este toggle — pedido explícito do usuário: "habilite a busca
  // também para o search, pois agora está apenas para o asset continuousdeploy-history".
  const [manualSearchAllRepos, setManualSearchAllRepos] = useState(false);
  // Escopo da última busca disparada (automática ou manual) — só pra exibir a mensagem de
  // loading/erro com o escopo certo, nunca usado como decisão de fluxo.
  const [lastSearchScopeLabel, setLastSearchScopeLabel] = useState(`repositório "${NEXUS_ROLLBACK_REPOSITORY}"`);

  const [nexusManifest, setNexusManifest] = useState("");
  const [extractError, setExtractError] = useState("");
  const [fetchingContent, setFetchingContent] = useState(false);
  // Pedido explícito do usuário: "habilite a opção de download dos itens e crie uma pasta para
  // guarda-los" — persiste o artefato na pasta gerenciada ~/.k8s-hpa-manager/rollback-artifacts/
  // (ver internal/rollbackfiles/store.go), reaproveitável depois pelo Modo Arquivos sem precisar
  // buscar no Nexus de novo. `downloadingName` rastreia qual linha está baixando (nunca mais de
  // uma por vez, mas por nome — permite feedback visual só no botão certo).
  const [downloadingName, setDownloadingName] = useState<string | null>(null);
  const [downloadedNames, setDownloadedNames] = useState<Set<string>>(new Set());

  const handleDownloadArtifact = async (a: NexusFlatArtifact) => {
    setDownloadingName(a.name);
    try {
      const res = await apiClient.nexusDownloadValues({ repository: a.repository, filePath: a.name });
      if (res.error) throw new Error(res.error);
      await apiClient.saveRollbackFile(a.name, res.content || "");
      setDownloadedNames((prev) => new Set(prev).add(a.name));
      toast.success(`${a.name} salvo na pasta de rollback do servidor`);
    } catch (err) {
      toast.error("Falha ao baixar artefato", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setDownloadingName(null);
    }
  };

  const [force, setForce] = useState(false);
  const [reason, setReason] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [applying, setApplying] = useState(false);

  const progress = useRollbackProgress();

  const runSearch = useCallback((term: string, allRepos = false) => {
    if (!term.trim()) return;
    setLoading(true);
    setLoadError("");
    setArtifacts([]);
    setSelectedArtifact(null);
    setNexusManifest("");
    setExtractError("");
    const scopeLabel = allRepos ? "todos os repositórios" : `repositório "${NEXUS_ROLLBACK_REPOSITORY}"`;
    setLastSearchScopeLabel(scopeLabel);
    apiClient.nexusSearchFlat(NEXUS_ROLLBACK_REPOSITORY, term.trim(), allRepos)
      .then((res) => {
        if (res.length === 0) {
          setLoadError(`Nenhum artefato encontrado no Nexus (${scopeLabel}) para "${term.trim()}".`);
          return;
        }
        setArtifacts(res);
      })
      .catch((err) => setLoadError(err instanceof Error ? err.message : "Erro ao buscar no Nexus (verifique se está configurado no Perfil do Usuário)"))
      .finally(() => setLoading(false));
  }, []);

  // Busca automática ao abrir o modal — pedido explícito do usuário: listar direto o histórico,
  // sem exigir buscar manualmente primeiro. Usa o nome sugerido (labels do Deployment, ver
  // DeploymentRollbackModal) como termo inicial — na convenção real desta empresa, o nome do
  // projeto aparece embutido no nome do arquivo (ex: "...faturamento-gateway-adc...").
  useEffect(() => { runSearch(suggestedReleaseSearch); }, [suggestedReleaseSearch, runSearch]);

  // Ao selecionar um artefato: baixa o conteúdo bruto (multi-documento) e extrai só o Deployment.
  useEffect(() => {
    if (!selectedArtifact) { setNexusManifest(""); setExtractError(""); return; }
    setFetchingContent(true);
    setExtractError("");
    apiClient.nexusDownloadValues({
      repository: selectedArtifact.repository,
      filePath: selectedArtifact.name,
    })
      .then((res) => {
        if (res.error) { toast.error("Nexus retornou erro", { description: res.error }); setNexusManifest(""); return; }
        const extracted = extractDeploymentDoc(res.content || "");
        if (extracted.error) { setExtractError(extracted.error); setNexusManifest(""); return; }
        setNexusManifest(extracted.yaml);
      })
      .catch((err) => toast.error("Erro ao baixar artefato do Nexus", { description: err instanceof Error ? err.message : "Erro" }))
      .finally(() => setFetchingContent(false));
  }, [selectedArtifact]);

  const canConfirm = !!nexusManifest && reason.trim().length > 0;

  const handleConfirm = async () => {
    setApplying(true);
    setConfirming(false);
    try {
      const { sessionId } = await apiClient.applyDeploymentManifest(cluster, namespace, deploymentName, nexusManifest, reason.trim(), force);
      toast.success("Manifesto histórico do Nexus aplicado — acompanhando o rollout...");
      progress.start(apiClient.getDeploymentRollbackStreamURL(sessionId), onDone);
    } catch (err) {
      toast.error("Falha ao aplicar manifesto do Nexus", { description: err instanceof Error ? err.message : "Erro" });
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
          Este modo aplica (<span className="font-mono">kubectl apply</span>) um manifesto Deployment histórico publicado pelo pipeline de
          CI/CD no Nexus — útil quando o histórico de revisões do próprio cluster (Modo K8s nativo) já foi podado
          (<span className="font-mono">revisionHistoryLimit</span>). Revise o diff com atenção: este manifesto pode ter divergido do chart
          atual desde então.
        </span>
      </div>

      {loading && (
        <div className="flex items-center justify-center py-12 text-muted-foreground text-sm">
          <Loader2 className="w-5 h-5 animate-spin mr-2" /> Buscando artefatos no Nexus ({lastSearchScopeLabel})...
        </div>
      )}

      {!loading && loadError && (
        <div className="text-sm text-destructive py-2 text-center">{loadError}</div>
      )}

      {!loading && artifacts.length > 1 && (
        <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-2.5 text-[11px] text-amber-700 dark:text-amber-400">
          <AlertTriangle className="w-3.5 h-3.5 mt-0.5 shrink-0" />
          <span>
            Segundo o procedimento interno de rollback manual: se este deploy foi feito <strong>por artefato</strong> (Spinnaker/GitHub CD
            via render-helm ou render-kustomize), o artefato <strong>mais recente</strong> costuma ser justamente a versão com problema —
            prefira o 2º item da lista (marcado abaixo). Se este foi o <strong>1º deploy via Helm</strong> em produção, use o mais recente
            (1º item) mesmo. Reveja o diff antes de confirmar, de qualquer forma.
          </span>
        </div>
      )}

      {!loading && artifacts.length > 0 && (
        <div>
          <div className="text-xs text-muted-foreground mb-1.5">
            {artifacts.length} artefato(s) encontrado(s) — mais recente primeiro
          </div>
          <RadioGroup
            value={selectedArtifact?.name ?? ""}
            onValueChange={(v) => setSelectedArtifact(artifacts.find((a) => a.name === v) ?? null)}
            className="space-y-1.5 max-h-64 overflow-y-auto pr-1"
          >
            {artifacts.map((a, idx) => (
              <div key={a.name} className="flex items-start gap-3 p-2.5 border rounded-lg hover:bg-accent">
                <RadioGroupItem value={a.name} id={`nexusart-${a.name}`} className="mt-1" />
                <Label htmlFor={`nexusart-${a.name}`} className="flex-1 cursor-pointer">
                  <div className="font-mono text-xs break-all">{a.name}</div>
                  <div className="flex items-center gap-2 text-[11px] text-muted-foreground mt-0.5">
                    <span>{formatDate(a.lastModified)}</span>
                    {a.uploader && <Badge variant="outline" className="text-[10px]">{a.uploader}</Badge>}
                    {a.repository !== NEXUS_ROLLBACK_REPOSITORY && <Badge variant="outline" className="text-[10px]">{a.repository}</Badge>}
                    {idx === 1 && <Badge variant="outline" className="text-[10px] border-amber-500/50 text-amber-600 dark:text-amber-400">2º mais recente</Badge>}
                    {downloadedNames.has(a.name) && <Badge variant="outline" className="text-[10px] border-green-500/50 text-green-600 dark:text-green-400">baixado</Badge>}
                  </div>
                </Label>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="mt-0.5 h-7 px-2 shrink-0"
                  title="Baixar (salva na pasta de rollback do servidor, pro Modo Arquivos)"
                  disabled={downloadingName === a.name}
                  onClick={(e) => { e.preventDefault(); e.stopPropagation(); handleDownloadArtifact(a); }}
                >
                  {downloadingName === a.name ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Download className="w-3.5 h-3.5" />}
                </Button>
              </div>
            ))}
          </RadioGroup>
        </div>
      )}

      {!loading && !manualSearchOpen && (
        <button type="button" className="text-xs text-primary underline underline-offset-2" onClick={() => setManualSearchOpen(true)}>
          Buscar por outro nome de aplicação
        </button>
      )}

      {manualSearchOpen && (
        <div className="space-y-1.5">
          <div className="flex items-end gap-2">
            <div className="flex-1">
              <Label htmlFor="nexus-manual-search" className="text-xs text-muted-foreground mb-1 block">Nome da aplicação no Nexus</Label>
              <Input id="nexus-manual-search" value={manualSearchTerm} onChange={(e) => setManualSearchTerm(e.target.value)} onKeyDown={(e) => e.key === "Enter" && runSearch(manualSearchTerm, manualSearchAllRepos)} />
            </div>
            <Button variant="outline" onClick={() => runSearch(manualSearchTerm, manualSearchAllRepos)} disabled={loading || !manualSearchTerm.trim()}>
              {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Search className="w-4 h-4" />}
            </Button>
          </div>
          <div className="flex items-center gap-2">
            <input type="checkbox" id="nexus-manual-search-allrepos" checked={manualSearchAllRepos} onChange={(e) => setManualSearchAllRepos(e.target.checked)} className="rounded" />
            <Label htmlFor="nexus-manual-search-allrepos" className="text-xs text-muted-foreground cursor-pointer">
              Buscar em todos os repositórios do Nexus (não só "{NEXUS_ROLLBACK_REPOSITORY}")
            </Label>
          </div>
        </div>
      )}

      {fetchingContent && <div className="flex items-center justify-center py-6 text-muted-foreground text-sm"><Loader2 className="w-4 h-4 animate-spin mr-2" /> Baixando manifesto do Nexus...</div>}

      {extractError && (
        <div className="text-sm text-destructive py-2 text-center">{extractError}</div>
      )}

      {nexusManifest && (
        <>
          <div>
            <Label className="text-xs text-muted-foreground mb-1 block">Diff — Deployment atual vs. manifesto histórico do Nexus (revise antes de confirmar)</Label>
            <MonacoYamlEditor mode="diff" originalValue={currentYaml} value={nexusManifest} height={280} readOnly />
          </div>

          <div className="flex items-center gap-2">
            <input type="checkbox" id="nexus-force" checked={force} onChange={(e) => setForce(e.target.checked)} className="rounded" />
            <Label htmlFor="nexus-force" className="text-xs cursor-pointer">Forçar (recria recursos se necessário)</Label>
          </div>

          <div>
            <Label htmlFor="nexus-reason" className="text-xs text-muted-foreground mb-1 block">Motivo do rollback (obrigatório)</Label>
            <Textarea id="nexus-reason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Ex: manifesto de DD/MM é o último confirmado estável" rows={2} />
          </div>

          <div className="flex items-center gap-2 pt-2 border-t">
            {!confirming ? (
              <Button variant="default" disabled={!canConfirm || !canUpdateDeployment || applying} onClick={() => setConfirming(true)} title={!canUpdateDeployment ? "Sem permissão de escrita neste namespace (K8s RBAC)" : undefined}>
                <RotateCcw className="w-4 h-4 mr-2" /> Aplicar manifesto do Nexus
              </Button>
            ) : (
              <>
                <span className="text-sm text-amber-600 dark:text-amber-400 flex items-center gap-1"><AlertTriangle className="w-4 h-4" /> Confirmar aplicação do manifesto histórico?</span>
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
// Modo Arquivos — rollback manual a partir de um YAML já salvo: seja na pasta gerenciada de
// artefatos de rollback (~/.k8s-hpa-manager/rollback-artifacts/, alimentada pelo botão "Baixar" do
// Modo Nexus) ou em qualquer outro diretório do servidor onde o usuário já tenha arquivos salvos de
// ações de rollback anteriores. Pedido explícito do usuário: "inclua uma função de rollback manual
// onde poderemos selecionar um ou mais itens dentro desse diretório, ou busca em outro diretório...
// inclua a opção de editar os yamls desses diretórios usando nossa solução do monaco editor".
//
// Disponível independente de o Deployment ser Helm-gerenciado ou não — ao contrário do Modo Imagem
// (restrito a não-Helm), aqui é o próprio usuário quem escolhe manualmente o arquivo a aplicar, e
// pode ser exatamente o tipo de emergência que o procedimento interno de rollback descreve (rollback
// automático indisponível); por isso mostra o mesmo aviso de risco de drift quando Helm-gerenciado,
// mas nunca bloqueia a ação — a decisão final é do usuário.
// ═══════════════════════════════════════════════════════════════════════════

type FileSource = "default" | "external";

function FileRollbackSection({
  cluster, namespace, deploymentName, currentYaml, isHelmManaged, canUpdateDeployment, onDone,
}: {
  cluster: string; namespace: string; deploymentName: string; currentYaml: string; isHelmManaged: boolean; canUpdateDeployment: boolean; onDone: () => void;
}) {
  const [source, setSource] = useState<FileSource>("default");
  const [files, setFiles] = useState<RollbackFileEntry[]>([]);
  const [baseDir, setBaseDir] = useState("");
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState("");

  const [externalDir, setExternalDir] = useState("");
  const [externalSearched, setExternalSearched] = useState(false);

  const [selectedNames, setSelectedNames] = useState<Set<string>>(new Set());
  const [deletingName, setDeletingName] = useState<string | null>(null);
  const [bulkDeleting, setBulkDeleting] = useState(false);

  const [activeFile, setActiveFile] = useState<RollbackFileEntry | null>(null);
  const [activeContent, setActiveContent] = useState("");
  const [loadingActiveContent, setLoadingActiveContent] = useState(false);
  const [savingActiveContent, setSavingActiveContent] = useState(false);

  const [force, setForce] = useState(false);
  const [reason, setReason] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [applying, setApplying] = useState(false);

  const progress = useRollbackProgress();

  const loadDefaultFiles = useCallback(() => {
    setLoading(true);
    setLoadError("");
    apiClient.listRollbackFiles()
      .then((res) => { setFiles(res.files); setBaseDir(res.baseDir); })
      .catch((err) => setLoadError(err instanceof Error ? err.message : "Erro ao listar arquivos"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    if (source === "default") loadDefaultFiles();
  }, [source, loadDefaultFiles]);

  const handleBrowseExternal = () => {
    if (!externalDir.trim()) return;
    setLoading(true);
    setLoadError("");
    setExternalSearched(true);
    apiClient.browseRollbackDirectory(externalDir.trim())
      .then(setFiles)
      .catch((err) => { setFiles([]); setLoadError(err instanceof Error ? err.message : "Erro ao navegar no diretório"); })
      .finally(() => setLoading(false));
  };

  const toggleSelect = (name: string) => {
    setSelectedNames((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name); else next.add(name);
      return next;
    });
  };

  const handleBulkDelete = async () => {
    setBulkDeleting(true);
    try {
      for (const name of selectedNames) {
        await apiClient.deleteRollbackFile(name);
      }
      toast.success(`${selectedNames.size} arquivo(s) excluído(s)`);
      setSelectedNames(new Set());
      if (activeFile && selectedNames.has(activeFile.name)) { setActiveFile(null); setActiveContent(""); }
      loadDefaultFiles();
    } catch (err) {
      toast.error("Falha ao excluir alguns arquivos", { description: err instanceof Error ? err.message : "Erro" });
      loadDefaultFiles();
    } finally {
      setBulkDeleting(false);
    }
  };

  const handleDeleteFile = async (file: RollbackFileEntry) => {
    setDeletingName(file.name);
    try {
      await apiClient.deleteRollbackFile(file.name);
      toast.success(`${file.name} excluído`);
      if (activeFile?.name === file.name) { setActiveFile(null); setActiveContent(""); }
      loadDefaultFiles();
    } catch (err) {
      toast.error("Falha ao excluir arquivo", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setDeletingName(null);
    }
  };

  const handleSelectFile = (file: RollbackFileEntry) => {
    setActiveFile(file);
    setActiveContent("");
    setReason("");
    setConfirming(false);
    setLoadingActiveContent(true);
    const read = source === "default" ? apiClient.readRollbackFile(file.name) : apiClient.readExternalRollbackFile(file.path);
    read
      .then(setActiveContent)
      .catch((err) => toast.error("Erro ao ler arquivo", { description: err instanceof Error ? err.message : "Erro" }))
      .finally(() => setLoadingActiveContent(false));
  };

  const handleSaveActiveContent = async () => {
    if (!activeFile) return;
    setSavingActiveContent(true);
    try {
      if (source === "default") await apiClient.writeRollbackFile(activeFile.name, activeContent);
      else await apiClient.writeExternalRollbackFile(activeFile.path, activeContent);
      toast.success("Arquivo salvo");
      if (source === "default") loadDefaultFiles();
    } catch (err) {
      toast.error("Falha ao salvar arquivo", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setSavingActiveContent(false);
    }
  };

  // extractDeploymentDoc é puro e barato — recalcular a cada tecla digitada no editor é seguro,
  // sem chamada de rede nenhuma (mesma função já usada pelo Modo Nexus).
  const extracted = useMemo(() => extractDeploymentDoc(activeContent), [activeContent]);
  const canConfirm = !!extracted.yaml && !extracted.error && reason.trim().length > 0;

  const handleConfirm = async () => {
    setApplying(true);
    setConfirming(false);
    try {
      const { sessionId } = await apiClient.applyDeploymentManifest(cluster, namespace, deploymentName, extracted.yaml, reason.trim(), force);
      toast.success("Manifesto do arquivo aplicado — acompanhando o rollout...");
      progress.start(apiClient.getDeploymentRollbackStreamURL(sessionId), onDone);
    } catch (err) {
      toast.error("Falha ao aplicar manifesto", { description: err instanceof Error ? err.message : "Erro" });
    } finally {
      setApplying(false);
    }
  };

  if (progress.active || progress.done) {
    return <RolloutProgressPanel progress={progress} />;
  }

  return (
    <div className="space-y-4 py-2">
      <div className="flex items-center gap-1 border-b border-border">
        <button
          type="button"
          onClick={() => { setSource("default"); setActiveFile(null); setActiveContent(""); setSelectedNames(new Set()); }}
          className={`px-3 py-1.5 text-xs font-medium border-b-2 -mb-px flex items-center gap-1.5 ${source === "default" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}
        >
          <FolderOpen className="w-3.5 h-3.5" /> Pasta padrão
        </button>
        <button
          type="button"
          onClick={() => { setSource("external"); setActiveFile(null); setActiveContent(""); setSelectedNames(new Set()); setFiles([]); setExternalSearched(false); }}
          className={`px-3 py-1.5 text-xs font-medium border-b-2 -mb-px flex items-center gap-1.5 ${source === "external" ? "border-primary text-foreground" : "border-transparent text-muted-foreground"}`}
        >
          <FolderSearch className="w-3.5 h-3.5" /> Outro diretório
        </button>
      </div>

      {source === "default" && baseDir && (
        <div className="text-[11px] text-muted-foreground font-mono">Pasta no servidor: {baseDir}</div>
      )}

      {source === "external" && (
        <div className="flex items-end gap-2">
          <div className="flex-1">
            <Label htmlFor="files-external-dir" className="text-xs text-muted-foreground mb-1 block">Caminho absoluto no servidor</Label>
            <Input id="files-external-dir" value={externalDir} onChange={(e) => setExternalDir(e.target.value)} placeholder="/caminho/absoluto/da/pasta" className="font-mono text-xs" onKeyDown={(e) => e.key === "Enter" && handleBrowseExternal()} />
          </div>
          <Button variant="outline" onClick={handleBrowseExternal} disabled={loading || !externalDir.trim()}>
            {loading ? <Loader2 className="w-4 h-4 animate-spin" /> : <Search className="w-4 h-4" />}
          </Button>
        </div>
      )}

      {loading && (
        <div className="flex items-center justify-center py-8 text-muted-foreground text-sm"><Loader2 className="w-5 h-5 animate-spin mr-2" /> Carregando...</div>
      )}

      {!loading && loadError && (
        <div className="text-sm text-destructive py-2 text-center">{loadError}</div>
      )}

      {!loading && !loadError && source === "external" && externalSearched && files.length === 0 && (
        <div className="text-sm text-muted-foreground py-4 text-center">Nenhum arquivo .yaml/.yml encontrado nesse diretório.</div>
      )}

      {!loading && !loadError && source === "default" && files.length === 0 && (
        <div className="text-sm text-muted-foreground py-4 text-center">
          Nenhum arquivo salvo ainda — use o botão de download na lista do Modo Nexus, ou salve manualmente em {baseDir || "~/.k8s-hpa-manager/rollback-artifacts"}.
        </div>
      )}

      {!loading && files.length > 0 && (
        <div>
          <div className="flex items-center justify-between mb-1.5">
            <div className="text-xs text-muted-foreground">{files.length} arquivo(s) — mais recente primeiro</div>
            {source === "default" && selectedNames.size > 0 && (
              <Button size="sm" variant="ghost" className="h-6 px-2 text-xs text-destructive hover:text-destructive" onClick={handleBulkDelete} disabled={bulkDeleting}>
                {bulkDeleting ? <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" /> : <Trash2 className="w-3.5 h-3.5 mr-1" />} Excluir {selectedNames.size} selecionado(s)
              </Button>
            )}
          </div>
          <RadioGroup value={activeFile?.name ?? ""} className="space-y-1.5 max-h-56 overflow-y-auto pr-1">
            {files.map((f) => (
              <div key={f.path} className="flex items-center gap-2 p-2.5 border rounded-lg hover:bg-accent">
                {source === "default" && (
                  <input type="checkbox" checked={selectedNames.has(f.name)} onChange={() => toggleSelect(f.name)} onClick={(e) => e.stopPropagation()} className="rounded" />
                )}
                <RadioGroupItem value={f.name} id={`file-${f.path}`} onClick={() => handleSelectFile(f)} />
                <Label htmlFor={`file-${f.path}`} className="flex-1 cursor-pointer min-w-0" onClick={() => handleSelectFile(f)}>
                  <div className="font-mono text-xs break-all">{f.name}</div>
                  <div className="text-[11px] text-muted-foreground mt-0.5">{formatDate(f.modifiedAt)} · {(f.size / 1024).toFixed(1)} KB</div>
                </Label>
                {source === "default" && (
                  <Button type="button" variant="ghost" size="sm" className="h-7 px-2 shrink-0 text-destructive hover:text-destructive" title="Excluir" disabled={deletingName === f.name} onClick={(e) => { e.preventDefault(); e.stopPropagation(); handleDeleteFile(f); }}>
                    {deletingName === f.name ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Trash2 className="w-3.5 h-3.5" />}
                  </Button>
                )}
              </div>
            ))}
          </RadioGroup>
        </div>
      )}

      {activeFile && (
        <>
          <div className="flex items-center justify-between">
            <Label className="text-xs text-muted-foreground flex items-center gap-1.5"><Pencil className="w-3.5 h-3.5" /> Editando: <span className="font-mono text-foreground">{activeFile.name}</span></Label>
            <Button size="sm" variant="outline" onClick={handleSaveActiveContent} disabled={savingActiveContent || loadingActiveContent}>
              {savingActiveContent ? <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" /> : <Save className="w-3.5 h-3.5 mr-1.5" />} Salvar alterações
            </Button>
          </div>
          {loadingActiveContent ? (
            <div className="flex items-center justify-center h-40 border rounded-md text-muted-foreground text-sm"><Loader2 className="w-4 h-4 animate-spin mr-2" /> Carregando conteúdo...</div>
          ) : (
            <MonacoYamlEditor mode="editor" value={activeContent} onChange={setActiveContent} height={260} />
          )}

          {isHelmManaged && (
            <div className="flex items-start gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 p-3 text-xs text-amber-700 dark:text-amber-400">
              <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
              <span>
                Este Deployment é gerenciado por Helm — aplicar um manifesto manual aqui causa <strong>drift</strong> (o Helm nunca fica
                sabendo dessa mudança). Prefira o Modo Helm/Nexus quando possível; use este modo só se estiver de fato no cenário de
                emergência do procedimento de rollback manual.
              </span>
            </div>
          )}

          {extracted.error && (
            <div className="text-sm text-destructive py-2 text-center">{extracted.error}</div>
          )}

          {extracted.yaml && (
            <>
              <div>
                <Label className="text-xs text-muted-foreground mb-1 block">Diff — Deployment atual vs. arquivo selecionado (revise antes de confirmar)</Label>
                <MonacoYamlEditor mode="diff" originalValue={currentYaml} value={extracted.yaml} height={240} readOnly />
              </div>

              <div className="flex items-center gap-2">
                <input type="checkbox" id="files-force" checked={force} onChange={(e) => setForce(e.target.checked)} className="rounded" />
                <Label htmlFor="files-force" className="text-xs cursor-pointer">Forçar (recria recursos se necessário)</Label>
              </div>

              <div>
                <Label htmlFor="files-reason" className="text-xs text-muted-foreground mb-1 block">Motivo do rollback (obrigatório — vira annotation change-cause)</Label>
                <Textarea id="files-reason" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Ex: aplicando manifesto salvo manualmente de rollback anterior" rows={2} />
              </div>

              <div className="flex items-center gap-2 pt-2 border-t">
                {!confirming ? (
                  <Button variant="default" disabled={!canConfirm || !canUpdateDeployment || applying} onClick={() => setConfirming(true)} title={!canUpdateDeployment ? "Sem permissão de escrita neste namespace (K8s RBAC)" : undefined}>
                    <RotateCcw className="w-4 h-4 mr-2" /> Aplicar arquivo selecionado
                  </Button>
                ) : (
                  <>
                    <span className="text-sm text-amber-600 dark:text-amber-400 flex items-center gap-1"><AlertTriangle className="w-4 h-4" /> Confirmar aplicação deste arquivo?</span>
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
        </>
      )}
    </div>
  );
}

// ═══════════════════════════════════════════════════════════════════════════
// Progresso via SSE — compartilhado pelos 4 modos (fontes de evento diferentes: stream próprio de
// rollout do Modo K8s nativo/Imagem vs. stream de operação Helm dos Modos Helm/Nexus — mesma UI;
// Nexus/Arquivos aplicam via kubectl apply síncrono, sem streaming de rollout — ver comentário no
// topo de cada seção).
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
