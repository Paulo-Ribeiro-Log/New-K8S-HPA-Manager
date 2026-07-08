import { useState } from "react";
import { ClusterSelectorForTab } from "@/components/ClusterSelectorForTab";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Loader2, ShieldCheck, ShieldAlert, ShieldQuestion, AlertTriangle, Copy } from "lucide-react";
import { useClusters } from "@/hooks/useAPI";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { toast } from "sonner";
import type {
  AccessCheckRulesResult,
  AccessCheckCanIResult,
  AccessCheckResourceRule,
  AccessCheckFleetScanResult,
} from "@/lib/api/types";

// Entrada de histórico relevante para "access_check" — só os campos usados aqui
// (o tipo completo é privado a HistoryViewer.tsx, não reexportado).
interface AccessCheckHistoryEntry {
  id: string;
  timestamp: string;
  user_email?: string;
  cluster: string;
  status: string;
  after?: { email_analisado?: string; verb?: string; resource?: string };
}

const VERBS = ["get", "list", "watch", "create", "update", "patch", "delete", "deletecollection"];

const WRITE_VERBS = new Set(["create", "update", "patch"]);
const DELETE_VERBS = new Set(["delete", "deletecollection"]);
const READ_VERBS = new Set(["get", "list", "watch"]);
const WRITE_OR_DELETE_VERBS = new Set(["create", "update", "patch", "delete", "deletecollection"]);

function verbBadgeClass(verb: string): string {
  if (DELETE_VERBS.has(verb)) return "bg-red-500/10 text-red-500 border-red-500/30";
  if (WRITE_VERBS.has(verb)) return "bg-amber-500/10 text-amber-500 border-amber-500/30";
  return "bg-emerald-500/10 text-emerald-500 border-emerald-500/30";
}

// Categorias de recursos mais perguntadas por SREs — traduz as resourceRules brutas
// (que exigem interpretação) num veredito direto de "tem/não tem acesso" por recurso.
interface ResourceCategory {
  label: string;
  apiGroups: string[];
  resources: string[];
}

const RESOURCE_CATEGORIES: ResourceCategory[] = [
  { label: "Secrets", apiGroups: [""], resources: ["secrets"] },
  { label: "ConfigMaps", apiGroups: [""], resources: ["configmaps"] },
  { label: "Pods", apiGroups: [""], resources: ["pods"] },
  { label: "Logs de Pods", apiGroups: [""], resources: ["pods/log"] },
  { label: "Exec em Pods (terminal)", apiGroups: [""], resources: ["pods/exec"] },
  { label: "Services", apiGroups: [""], resources: ["services"] },
  { label: "Deployments", apiGroups: ["apps"], resources: ["deployments"] },
  { label: "StatefulSets", apiGroups: ["apps"], resources: ["statefulsets"] },
  { label: "DaemonSets", apiGroups: ["apps"], resources: ["daemonsets"] },
  { label: "CronJobs / Jobs", apiGroups: ["batch"], resources: ["cronjobs", "jobs"] },
  { label: "Ingress", apiGroups: ["networking.k8s.io", "extensions"], resources: ["ingresses"] },
  { label: "HPA (autoscaling)", apiGroups: ["autoscaling"], resources: ["horizontalpodautoscalers"] },
  { label: "Events", apiGroups: [""], resources: ["events"] },
];

interface CategoryAccess {
  label: string;
  read: boolean;
  write: boolean;
}

function computeCategoryAccess(rules: AccessCheckResourceRule[], cat: ResourceCategory): CategoryAccess {
  let read = false;
  let write = false;
  for (const rule of rules) {
    const groups = rule.apiGroups?.length ? rule.apiGroups : [""];
    if (!groups.some((g) => g === "*" || cat.apiGroups.includes(g))) continue;
    const resources = rule.resources ?? [];
    if (!resources.some((r) => r === "*" || cat.resources.includes(r))) continue;
    if (rule.verbs.includes("*")) {
      read = true;
      write = true;
      continue;
    }
    if (rule.verbs.some((v) => READ_VERBS.has(v))) read = true;
    if (rule.verbs.some((v) => WRITE_OR_DELETE_VERBS.has(v))) write = true;
  }
  return { label: cat.label, read, write };
}

function ResourceRuleRow({ rule }: { rule: AccessCheckResourceRule }) {
  const resources = rule.resources?.length ? rule.resources : ["*"];
  const group = rule.apiGroups?.join(", ") || "core";
  return (
    <tr className="border-b border-border last:border-0">
      <td className="py-1.5 pr-4 align-top">
        {resources.map((r) => (
          <div key={r} className="text-sm font-mono">
            {r === "*" ? <Badge variant="outline" className="border-primary/50 text-primary">Acesso total</Badge> : r}
          </div>
        ))}
      </td>
      <td className="py-1.5 pr-4 align-top text-sm text-muted-foreground">{group}</td>
      <td className="py-1.5 align-top">
        <div className="flex flex-wrap gap-1">
          {rule.verbs.map((v) => (
            <Badge key={v} variant="outline" className={verbBadgeClass(v)}>
              {v}
            </Badge>
          ))}
        </div>
      </td>
    </tr>
  );
}

export default function AccessCheckTab() {
  const { clusters } = useClusters();
  const [cluster, setCluster] = useState("");
  const [namespace, setNamespace] = useState("");
  const [email, setEmail] = useState("");
  const [section, setSection] = useState<"overview" | "pointcheck" | "allgroups" | "fleet" | "history">("overview");
  const [groupsSearch, setGroupsSearch] = useState("");

  const [fleetResult, setFleetResult] = useState<AccessCheckFleetScanResult | null>(null);
  const [fleetError, setFleetError] = useState<string | null>(null);
  const [fleetLoading, setFleetLoading] = useState(false);

  const [rulesResult, setRulesResult] = useState<AccessCheckRulesResult | null>(null);
  const [rulesError, setRulesError] = useState<string | null>(null);
  const [rulesLoading, setRulesLoading] = useState(false);

  const [verb, setVerb] = useState("get");
  const [resource, setResource] = useState("secrets");
  const [apiGroup, setApiGroup] = useState("");
  const [canIResult, setCanIResult] = useState<AccessCheckCanIResult | null>(null);
  const [canIError, setCanIError] = useState<string | null>(null);
  const [canILoading, setCanILoading] = useState(false);

  // E-mail associado ao rulesResult/canIResult atual — usado só pra decidir se o banner
  // compartilhado (IAM admin/grupos AAD, fora do switch de `section`) pode ser exibido sem risco
  // de mostrar dado de um analista antigo rotulado com o e-mail novo digitado no campo (ver
  // Revisão 7 no ACCESS-CHECK-PLAN.md). NÃO limpa mais rulesResult/canIResult/fleetResult ao
  // rodar um scan diferente — cada aba (Visão Geral/Verificação Pontual/Todos os Clusters)
  // mantém seu próprio resultado até você rodar de novo, só o banner compartilhado é que precisa
  // desse cuidado extra de correspondência de e-mail.
  const [pointCheckEmail, setPointCheckEmail] = useState<string | null>(null);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["namespaces-access-check", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: !!cluster,
  });

  const canVerify = !!cluster && !!namespace && !!email.trim();

  const runOverviewCheck = async () => {
    setRulesLoading(true);
    setRulesError(null);
    setRulesResult(null);
    try {
      const result = await apiClient.getAccessCheckRules(cluster, namespace, email.trim());
      setRulesResult(result);
      setPointCheckEmail(email.trim());
    } catch (err) {
      setRulesError(err instanceof Error ? err.message : "Falha ao verificar acesso");
    } finally {
      setRulesLoading(false);
    }
  };

  const runCanICheck = async () => {
    setCanILoading(true);
    setCanIError(null);
    setCanIResult(null);
    try {
      const result = await apiClient.getAccessCheckCanI({
        cluster,
        namespace,
        email: email.trim(),
        verb,
        resource,
        group: apiGroup,
      });
      setCanIResult(result);
      setPointCheckEmail(email.trim());
    } catch (err) {
      setCanIError(err instanceof Error ? err.message : "Falha ao verificar acesso");
    } finally {
      setCanILoading(false);
    }
  };

  const handleVerify = () => {
    if (section === "overview") runOverviewCheck();
    else runCanICheck();
  };

  const runFleetScan = async () => {
    setFleetLoading(true);
    setFleetError(null);
    setFleetResult(null);
    setSection("fleet");
    try {
      const result = await apiClient.getAccessCheckFleetScan(email.trim(), namespace || undefined);
      setFleetResult(result);
    } catch (err) {
      setFleetError(err instanceof Error ? err.message : "Falha ao varrer os clusters");
    } finally {
      setFleetLoading(false);
    }
  };

  const { data: historyEntries = [], isFetching: historyLoading } = useQuery({
    queryKey: ["access-check-history"],
    queryFn: () => apiClient.get<{ entries: AccessCheckHistoryEntry[] }>("/history?action=access_check"),
    select: (data) => data.entries, // GetAll() no backend já retorna mais recente primeiro
    enabled: section === "history",
  });

  // O banner compartilhado (IAM admin/grupos AAD, renderizado fora do switch de `section`) só
  // pode usar rulesResult/canIResult quando o e-mail que gerou esse resultado ainda bate com o
  // que está no campo agora — evita mostrar dado de um analista antigo rotulado com o e-mail novo
  // se o usuário trocar o e-mail no input sem clicar em "Verificar" de novo (ver Revisão 7).
  const pointCheckEmailStale = pointCheckEmail !== null && pointCheckEmail !== email.trim();
  const matchedGroups = pointCheckEmailStale ? undefined : (rulesResult?.matchedGroups ?? canIResult?.matchedGroups);
  const allGroups = pointCheckEmailStale ? undefined : (rulesResult?.allGroups ?? canIResult?.allGroups);
  const iamAdminAccess = pointCheckEmailStale ? undefined : (rulesResult?.iamAdminAccess ?? canIResult?.iamAdminAccess);
  const groupsResolutionError = pointCheckEmailStale ? undefined : (rulesResult?.groupsResolutionError || canIResult?.groupsResolutionError);
  const matchedGroupIds = new Set((matchedGroups ?? []).map((g) => g.id));
  const isImpersonationBlocked = (msg: string | null) =>
    !!msg && msg.toLowerCase().includes("impersonar");

  return (
    <div className="flex flex-col h-full">
      <div className="px-6 py-3 bg-muted/30 border-b border-border flex flex-wrap items-end gap-3">
        <div className="min-w-[220px]">
          <ClusterSelectorForTab
            selectedCluster={cluster}
            onClusterChange={(v) => {
              setCluster(v);
              setNamespace("");
              setRulesResult(null);
              setCanIResult(null);
            }}
            clusters={clusters.map((c) => c.context)}
            tabLabel="Verificar Acesso"
            clusterProviders={Object.fromEntries(clusters.map((c) => [c.context, c.cloud_provider || "unknown"]))}
          />
        </div>
        <div className="min-w-[200px]">
          <label className="text-xs text-muted-foreground block mb-1">Namespace</label>
          <Select value={namespace} onValueChange={setNamespace} disabled={!cluster}>
            <SelectTrigger>
              <SelectValue placeholder="Selecione o namespace" />
            </SelectTrigger>
            <SelectContent>
              {namespaces.map((ns) => (
                <SelectItem key={ns.name} value={ns.name}>
                  {ns.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="min-w-[260px] flex-1">
          <label className="text-xs text-muted-foreground block mb-1">E-mail do analista</label>
          <Input
            placeholder="analista@viavarejo.com.br"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        <Button onClick={handleVerify} disabled={!canVerify || rulesLoading || canILoading}>
          {(rulesLoading || canILoading) && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
          Verificar
        </Button>
        <Button variant="outline" onClick={runFleetScan} disabled={!email.trim() || fleetLoading}>
          {fleetLoading && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
          Verificar em todos os clusters
        </Button>
      </div>

      {(section === "overview" || section === "pointcheck" || section === "allgroups") && iamAdminAccess && iamAdminAccess.length > 0 && (
        <div className="px-6 py-3 border-b border-border bg-red-500/10">
          <div className="flex items-start gap-2">
            <ShieldAlert className="w-5 h-5 text-red-500 shrink-0 mt-0.5" />
            <div>
              <div className="font-semibold text-red-500">
                ⚠ {email} tem ACESSO ADMIN TOTAL a este cluster via IAM do Azure — fora do alcance desta ferramenta
              </div>
              <div className="text-sm text-muted-foreground mt-1">
                {iamAdminAccess.map((m) => (
                  <div key={m.groupName}>
                    Grupo <span className="font-medium text-foreground">{m.groupName}</span> tem a role{" "}
                    <span className="font-mono text-xs bg-muted px-1 py-0.5 rounded">{m.role}</span> no IAM do recurso AKS —
                    permite buscar o kubeconfig admin (certificado <code>system:masters</code>), que ignora completamente
                    o RBAC do Kubernetes. A Visão Geral e a Verificação Pontual abaixo <strong>não refletem</strong> esse acesso.
                  </div>
                ))}
              </div>
            </div>
          </div>
        </div>
      )}

      {(section === "overview" || section === "pointcheck" || section === "allgroups") && matchedGroups && (
        <div className="px-6 py-3 border-b border-border">
          <div className="text-xs font-medium text-muted-foreground mb-1.5">Grupos AAD (VV_CLOUD_*) usados na verificação</div>
          {groupsResolutionError ? (
            <div className="flex items-center gap-2 text-sm text-amber-500">
              <AlertTriangle className="w-4 h-4 shrink-0" />
              Falha ao resolver grupos AAD ({groupsResolutionError}) — resultado abaixo reflete só permissões genéricas de usuário autenticado, pode ser menos permissivo que a realidade.
            </div>
          ) : matchedGroups.length === 0 ? (
            <div className="text-sm text-muted-foreground">Nenhum grupo VV_CLOUD_* encontrado para este e-mail.</div>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {matchedGroups.map((g) => (
                <Badge key={g.id} variant="outline" className="font-mono text-xs">
                  {g.displayName}
                  <span className="ml-1.5 opacity-60">{g.id.slice(0, 8)}…</span>
                </Badge>
              ))}
            </div>
          )}
        </div>
      )}

      <div className="flex border-b border-border px-6 pt-2 gap-1 flex-shrink-0">
        {([
          { id: "overview", label: "Visão Geral" },
          { id: "pointcheck", label: "Verificação Pontual" },
          { id: "allgroups", label: `Todos os Grupos AAD${allGroups ? ` (${allGroups.length})` : ""}` },
          { id: "fleet", label: `Todos os Clusters${fleetResult ? ` (${fleetResult.results.length})` : ""}` },
          { id: "history", label: "Histórico" },
        ] as const).map((tab) => (
          <button
            key={tab.id}
            onClick={() => setSection(tab.id)}
            className={`px-3 py-1.5 text-sm font-medium transition-colors border-b-2 -mb-px ${
              section === tab.id ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
            }`}
          >
            {tab.label}
          </button>
        ))}
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto p-6">
        {section === "overview" && (
          <>
            {rulesError && (
              <div className={`rounded-md border p-3 mb-4 text-sm flex items-center gap-2 ${
                isImpersonationBlocked(rulesError) ? "border-amber-500/40 bg-amber-500/10 text-amber-600" : "border-red-500/40 bg-red-500/10 text-red-500"
              }`}>
                <ShieldAlert className="w-4 h-4 shrink-0" />
                {rulesError}
              </div>
            )}
            {!rulesResult && !rulesError && (
              <div className="text-sm text-muted-foreground flex items-center gap-2">
                <ShieldQuestion className="w-4 h-4" />
                Selecione cluster, namespace e e-mail, depois clique em "Verificar".
              </div>
            )}
            {rulesResult && (() => {
              const categories = RESOURCE_CATEGORIES.map((cat) => computeCategoryAccess(rulesResult.resourceRules, cat));
              const anyAccess = categories.some((c) => c.read || c.write);
              const accessList = categories.filter((c) => c.read || c.write).map((c) => c.label);
              const noAccessList = categories.filter((c) => !c.read && !c.write).map((c) => c.label);

              return (
                <>
                  {rulesResult.incomplete && (
                    <div className="rounded-md border border-amber-500/40 bg-amber-500/10 text-amber-600 p-3 mb-4 text-sm flex items-center gap-2">
                      <AlertTriangle className="w-4 h-4 shrink-0" />
                      O K8s retornou regras incompletas (wildcard policies complexas) — o acesso real pode ser mais permissivo do que o resumo abaixo mostra.
                    </div>
                  )}

                  {/* Veredito direto — responde "tem ou não tem acesso" sem exigir leitura de regras técnicas */}
                  <div className={`rounded-md border p-3 mb-4 text-sm ${
                    anyAccess ? "border-emerald-500/40 bg-emerald-500/10" : "border-red-500/40 bg-red-500/10"
                  }`}>
                    <div className={`flex items-center gap-2 font-semibold ${anyAccess ? "text-emerald-600" : "text-red-500"}`}>
                      {anyAccess ? <ShieldCheck className="w-4 h-4 shrink-0" /> : <ShieldAlert className="w-4 h-4 shrink-0" />}
                      {anyAccess
                        ? `${email} TEM acesso a recursos no namespace "${namespace}"`
                        : `${email} NÃO tem acesso a nenhum recurso de workload no namespace "${namespace}"`}
                    </div>
                    {anyAccess && (
                      <div className="mt-1.5 text-muted-foreground">
                        <span className="text-foreground font-medium">Tem acesso a: </span>
                        {accessList.join(", ")}
                      </div>
                    )}
                    {noAccessList.length > 0 && noAccessList.length < categories.length && (
                      <div className="mt-1 text-muted-foreground">
                        <span className="text-foreground font-medium">Sem acesso a: </span>
                        {noAccessList.join(", ")}
                      </div>
                    )}
                    {!anyAccess && (
                      <div className="mt-1 text-muted-foreground">
                        Só possui permissões genéricas de qualquer usuário autenticado no cluster (ex: NetworkPolicies, self-review) — ver detalhamento técnico abaixo.
                      </div>
                    )}
                  </div>

                  {/* Grade por recurso — leitura e escrita separadas, nada omitido */}
                  <table className="w-full text-left mb-4">
                    <thead>
                      <tr className="border-b border-border text-xs text-muted-foreground">
                        <th className="pb-2 font-medium">Recurso</th>
                        <th className="pb-2 font-medium text-center">Leitura</th>
                        <th className="pb-2 font-medium text-center">Escrita</th>
                      </tr>
                    </thead>
                    <tbody>
                      {categories.map((cat) => (
                        <tr key={cat.label} className="border-b border-border last:border-0">
                          <td className="py-1.5 text-sm">{cat.label}</td>
                          <td className="py-1.5 text-center">
                            {cat.read ? (
                              <Badge variant="outline" className="bg-emerald-500/10 text-emerald-500 border-emerald-500/30">Sim</Badge>
                            ) : (
                              <Badge variant="outline" className="bg-muted text-muted-foreground border-border">Não</Badge>
                            )}
                          </td>
                          <td className="py-1.5 text-center">
                            {cat.write ? (
                              <Badge variant="outline" className="bg-amber-500/10 text-amber-500 border-amber-500/30">Sim</Badge>
                            ) : (
                              <Badge variant="outline" className="bg-muted text-muted-foreground border-border">Não</Badge>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>

                  {/* Detalhamento técnico bruto — nada é omitido, só fica colapsado por padrão */}
                  <details>
                    <summary className="text-xs text-muted-foreground cursor-pointer">
                      Ver regras técnicas brutas ({rulesResult.resourceRules.length}) — como reportado pelo K8s
                    </summary>
                    <table className="w-full text-left mt-2">
                      <thead>
                        <tr className="border-b border-border text-xs text-muted-foreground">
                          <th className="pb-2 font-medium">Recurso</th>
                          <th className="pb-2 font-medium">API Group</th>
                          <th className="pb-2 font-medium">Verbos</th>
                        </tr>
                      </thead>
                      <tbody>
                        {rulesResult.resourceRules.length === 0 ? (
                          <tr>
                            <td colSpan={3} className="py-4 text-sm text-muted-foreground">
                              Nenhuma permissão sobre recursos encontrada neste namespace.
                            </td>
                          </tr>
                        ) : (
                          rulesResult.resourceRules.map((rule, i) => <ResourceRuleRow key={i} rule={rule} />)
                        )}
                      </tbody>
                    </table>
                  </details>

                  {rulesResult.nonResourceRules.length > 0 && (
                    <details className="mt-4">
                      <summary className="text-xs text-muted-foreground cursor-pointer">
                        URLs não-resource ({rulesResult.nonResourceRules.length}) — discovery/health, geralmente irrelevante
                      </summary>
                      <div className="mt-2 space-y-1">
                        {rulesResult.nonResourceRules.map((rule, i) => (
                          <div key={i} className="text-xs font-mono text-muted-foreground flex gap-2">
                            <span>{rule.nonResourceURLs?.join(", ")}</span>
                            <span className="opacity-60">[{rule.verbs.join(" ")}]</span>
                          </div>
                        ))}
                      </div>
                    </details>
                  )}
                </>
              );
            })()}
          </>
        )}

        {section === "pointcheck" && (
          <>
            <div className="flex flex-wrap items-end gap-3 mb-4">
              <div>
                <label className="text-xs text-muted-foreground block mb-1">Verbo</label>
                <Select value={verb} onValueChange={setVerb}>
                  <SelectTrigger className="w-[160px]">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {VERBS.map((v) => (
                      <SelectItem key={v} value={v}>
                        {v}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div>
                <label className="text-xs text-muted-foreground block mb-1">Resource</label>
                <Input className="w-[180px]" value={resource} onChange={(e) => setResource(e.target.value)} placeholder="secrets" />
              </div>
              <div>
                <label className="text-xs text-muted-foreground block mb-1">API Group (opcional)</label>
                <Input className="w-[180px]" value={apiGroup} onChange={(e) => setApiGroup(e.target.value)} placeholder='core = ""' />
              </div>
              <Button variant="outline" onClick={runCanICheck} disabled={!canVerify || canILoading}>
                {canILoading && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
                Checar
              </Button>
            </div>

            {canIError && (
              <div className={`rounded-md border p-3 mb-4 text-sm flex items-center gap-2 ${
                isImpersonationBlocked(canIError) ? "border-amber-500/40 bg-amber-500/10 text-amber-600" : "border-red-500/40 bg-red-500/10 text-red-500"
              }`}>
                <ShieldAlert className="w-4 h-4 shrink-0" />
                {canIError}
              </div>
            )}

            {canIResult && (
              <div className={`rounded-md border p-4 flex items-start gap-3 ${
                canIResult.allowed ? "border-emerald-500/40 bg-emerald-500/10" : "border-red-500/40 bg-red-500/10"
              }`}>
                {canIResult.allowed ? (
                  <ShieldCheck className="w-5 h-5 text-emerald-500 shrink-0 mt-0.5" />
                ) : (
                  <ShieldAlert className="w-5 h-5 text-red-500 shrink-0 mt-0.5" />
                )}
                <div>
                  <div className={`font-semibold ${canIResult.allowed ? "text-emerald-500" : "text-red-500"}`}>
                    {canIResult.allowed
                      ? `SIM — ${email} PODE executar "${verb} ${resource}" no namespace "${namespace}"`
                      : `NÃO — ${email} NÃO PODE executar "${verb} ${resource}" no namespace "${namespace}"`}
                  </div>
                  {canIResult.reason && <div className="text-sm text-muted-foreground mt-1">{canIResult.reason}</div>}
                </div>
              </div>
            )}
          </>
        )}

        {section === "allgroups" && (
          <>
            {!allGroups && (
              <div className="text-sm text-muted-foreground flex items-center gap-2">
                <ShieldQuestion className="w-4 h-4" />
                Selecione cluster, namespace e e-mail, depois clique em "Verificar".
              </div>
            )}
            {allGroups && (() => {
              const filteredGroups = allGroups
                .filter((g) => g.displayName.toLowerCase().includes(groupsSearch.toLowerCase()))
                .sort((a, b) => a.displayName.localeCompare(b.displayName));

              const copyGroups = async () => {
                const text = filteredGroups.map((g) => `${g.displayName}\t${g.id}`).join("\n");
                try {
                  await navigator.clipboard.writeText(text);
                  toast.success(`${filteredGroups.length} grupo(s) copiado(s) para a área de transferência`);
                } catch {
                  toast.error("Falha ao copiar — verifique permissões do navegador");
                }
              };

              return (
                <>
                  <div className="flex items-center justify-between mb-3 gap-3">
                    <p className="text-sm text-muted-foreground">
                      Todos os {allGroups.length} grupos AAD de <span className="font-medium text-foreground">{email}</span>.
                      Os marcados com <Badge variant="outline" className="border-primary/50 text-primary mx-1">usado</Badge>
                      começam com <code className="text-xs bg-muted px-1 py-0.5 rounded">VV_CLOUD</code> e foram usados na impersonation acima.
                    </p>
                    <div className="flex items-center gap-2 shrink-0">
                      <Input
                        className="w-[240px]"
                        placeholder="Filtrar por nome..."
                        value={groupsSearch}
                        onChange={(e) => setGroupsSearch(e.target.value)}
                      />
                      <Button variant="outline" size="sm" onClick={copyGroups} disabled={filteredGroups.length === 0}>
                        <Copy className="w-3.5 h-3.5 mr-1.5" />
                        Copiar lista
                      </Button>
                    </div>
                  </div>
                  <table className="w-full text-left">
                    <thead>
                      <tr className="border-b border-border text-xs text-muted-foreground">
                        <th className="pb-2 font-medium">Nome do grupo</th>
                        <th className="pb-2 font-medium">Object ID</th>
                        <th className="pb-2 font-medium">Usado na verificação</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filteredGroups.map((g) => (
                        <tr key={g.id} className="border-b border-border last:border-0">
                          <td className="py-1.5 pr-4 text-sm">{g.displayName}</td>
                          <td className="py-1.5 pr-4 text-xs font-mono text-muted-foreground">{g.id}</td>
                          <td className="py-1.5">
                            {matchedGroupIds.has(g.id) ? (
                              <Badge variant="outline" className="border-primary/50 text-primary">usado</Badge>
                            ) : (
                              <span className="text-xs text-muted-foreground">—</span>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </>
              );
            })()}
          </>
        )}

        {section === "fleet" && (
          <>
            {fleetError && (
              <div className="rounded-md border p-3 mb-4 text-sm flex items-center gap-2 border-red-500/40 bg-red-500/10 text-red-500">
                <ShieldAlert className="w-4 h-4 shrink-0" />
                {fleetError}
              </div>
            )}
            {!fleetResult && !fleetError && !fleetLoading && (
              <div className="text-sm text-muted-foreground flex items-center gap-2">
                <ShieldQuestion className="w-4 h-4" />
                Preencha o e-mail e clique em "Verificar em todos os clusters" — varre acesso admin via
                IAM (sempre) e acesso RBAC real em todos os clusters AKS: se um namespace for informado,
                verifica só nele; se não, verifica em TODOS os namespaces (não-sistema) de cada cluster e
                lista onde o analista tem acesso de fato. Pode levar de dezenas de segundos a poucos minutos.
              </div>
            )}
            {fleetResult && (() => {
              // "Conectividade" (r.reachable) é sobre o SERVIDOR conseguir falar com o
              // kube-apiserver — não tem nada a ver com o acesso do analista pesquisado.
              // Por isso ela não aparece mais como coluna do analista: clusters com erro
              // (inclusive de rede) viram uma lista separada, só relevante pra quem está
              // rodando a ferramenta (o pesquisador) entender o que NÃO foi verificado.
              const withAccess = fleetResult.results.filter(
                (r) => (r.iamAdminAccess?.length ?? 0) > 0 || r.namespaceAccess?.anyAccess
              );
              const errored = fleetResult.results.filter((r) => !!r.error);
              const cleanNoAccess = fleetResult.results.filter(
                (r) => !r.error && (r.iamAdminAccess?.length ?? 0) === 0 && !r.namespaceAccess?.anyAccess
              );

              const namespaceCell = (r: (typeof fleetResult.results)[number]) =>
                namespace ? (
                  r.namespaceAccess?.anyAccess ? (
                    <Badge variant="outline" className="bg-emerald-500/10 text-emerald-500 border-emerald-500/30">Sim</Badge>
                  ) : (
                    <Badge variant="outline" className="bg-muted text-muted-foreground border-border">Não</Badge>
                  )
                ) : r.namespaceAccess?.namespaces && r.namespaceAccess.namespaces.length > 0 ? (
                  <div className="flex flex-wrap gap-1 max-w-md">
                    {r.namespaceAccess.namespaces.map((ns) => (
                      <Badge key={ns} variant="outline" className="bg-emerald-500/10 text-emerald-500 border-emerald-500/30 font-mono text-xs">
                        {ns}
                      </Badge>
                    ))}
                  </div>
                ) : (
                  <span className="text-xs text-muted-foreground">—</span>
                );

              return (
                <>
                  <div className="text-sm text-muted-foreground mb-3">
                    {fleetResult.results.length} clusters verificados —{" "}
                    <span className="font-medium text-foreground">{withAccess.length} com acesso real do analista</span>
                    {errored.length > 0 && <span> · {errored.length} não verificados (erro do servidor)</span>}
                  </div>

                  <div className="text-xs font-medium text-muted-foreground mb-1.5">
                    Clusters com acesso real de {email}
                  </div>
                  {withAccess.length === 0 ? (
                    <div className="text-sm text-muted-foreground mb-4">Nenhum acesso real encontrado em nenhum cluster verificado.</div>
                  ) : (
                    <table className="w-full text-left mb-4">
                      <thead>
                        <tr className="border-b border-border text-xs text-muted-foreground">
                          <th className="pb-2 font-medium">Cluster</th>
                          <th className="pb-2 font-medium">Acesso Admin (IAM)</th>
                          <th className="pb-2 font-medium">
                            {namespace ? `Acesso RBAC em "${namespace}"` : "Namespaces com acesso real"}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {withAccess
                          .slice()
                          .sort((a, b) => a.cluster.localeCompare(b.cluster))
                          .map((r) => (
                            <tr
                              key={r.cluster}
                              className={`border-b border-border last:border-0 ${
                                (r.iamAdminAccess?.length ?? 0) > 0 ? "bg-red-500/10" : "bg-emerald-500/5"
                              }`}
                            >
                              <td className="py-1.5 pr-4 text-sm font-mono">{r.cluster}</td>
                              <td className="py-1.5 pr-4">
                                {r.iamAdminAccess && r.iamAdminAccess.length > 0 ? (
                                  <div className="flex flex-col gap-1">
                                    {r.iamAdminAccess.map((m) => (
                                      <Badge key={m.groupName} variant="outline" className="bg-red-500/10 text-red-500 border-red-500/30 w-fit">
                                        {m.groupName}
                                      </Badge>
                                    ))}
                                  </div>
                                ) : (
                                  <span className="text-xs text-muted-foreground">—</span>
                                )}
                              </td>
                              <td className="py-1.5 pr-4">{namespaceCell(r)}</td>
                            </tr>
                          ))}
                      </tbody>
                    </table>
                  )}

                  {errored.length > 0 && (
                    <details className="mb-4">
                      <summary className="text-xs font-medium text-muted-foreground cursor-pointer">
                        {errored.length} clusters não verificados (problema do servidor, não do analista) — clique para ver
                      </summary>
                      <table className="w-full text-left mt-2">
                        <thead>
                          <tr className="border-b border-border text-xs text-muted-foreground">
                            <th className="pb-2 font-medium">Cluster</th>
                            <th className="pb-2 font-medium">Erro</th>
                          </tr>
                        </thead>
                        <tbody>
                          {errored
                            .slice()
                            .sort((a, b) => a.cluster.localeCompare(b.cluster))
                            .map((r) => (
                              <tr key={r.cluster} className="border-b border-border last:border-0">
                                <td className="py-1.5 pr-4 text-sm font-mono">{r.cluster}</td>
                                <td className="py-1.5 text-xs text-amber-500">{r.error}</td>
                              </tr>
                            ))}
                        </tbody>
                      </table>
                    </details>
                  )}

                  <details>
                    <summary className="text-xs font-medium text-muted-foreground cursor-pointer">
                      {cleanNoAccess.length} clusters verificados sem nenhum acesso — clique para ver
                    </summary>
                    <div className="flex flex-wrap gap-1.5 mt-2">
                      {cleanNoAccess
                        .slice()
                        .sort((a, b) => a.cluster.localeCompare(b.cluster))
                        .map((r) => (
                          <Badge key={r.cluster} variant="outline" className="bg-muted text-muted-foreground border-border font-mono text-xs">
                            {r.cluster}
                          </Badge>
                        ))}
                    </div>
                  </details>
                </>
              );
            })()}
          </>
        )}

        {section === "history" && (
          <>
            {historyLoading && (
              <div className="text-sm text-muted-foreground flex items-center gap-2">
                <Loader2 className="w-4 h-4 animate-spin" />
                Carregando histórico...
              </div>
            )}
            {!historyLoading && historyEntries.length === 0 && (
              <div className="text-sm text-muted-foreground">Nenhuma consulta registrada ainda.</div>
            )}
            {!historyLoading && historyEntries.length > 0 && (
              <table className="w-full text-left">
                <thead>
                  <tr className="border-b border-border text-xs text-muted-foreground">
                    <th className="pb-2 font-medium">Data/hora</th>
                    <th className="pb-2 font-medium">Quem consultou</th>
                    <th className="pb-2 font-medium">Cluster</th>
                    <th className="pb-2 font-medium">Namespace</th>
                    <th className="pb-2 font-medium">E-mail analisado</th>
                    <th className="pb-2 font-medium">Verbo/Recurso</th>
                    <th className="pb-2 font-medium">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {historyEntries.map((entry) => (
                    <tr key={entry.id} className="border-b border-border last:border-0">
                      <td className="py-1.5 pr-4 text-xs text-muted-foreground whitespace-nowrap">
                        {new Date(entry.timestamp).toLocaleString("pt-BR")}
                      </td>
                      <td className="py-1.5 pr-4 text-sm">{entry.user_email ?? "—"}</td>
                      <td className="py-1.5 pr-4 text-sm font-mono">{entry.cluster}</td>
                      <td className="py-1.5 pr-4 text-sm">{entry.resource}</td>
                      <td className="py-1.5 pr-4 text-sm">{entry.after?.email_analisado ?? "—"}</td>
                      <td className="py-1.5 pr-4 text-xs font-mono text-muted-foreground">
                        {entry.after?.verb && entry.after?.resource ? `${entry.after.verb} ${entry.after.resource}` : "—"}
                      </td>
                      <td className="py-1.5">
                        <Badge
                          variant="outline"
                          className={
                            entry.status === "success"
                              ? "bg-emerald-500/10 text-emerald-500 border-emerald-500/30"
                              : "bg-red-500/10 text-red-500 border-red-500/30"
                          }
                        >
                          {entry.status}
                        </Badge>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </>
        )}
      </div>
    </div>
  );
}
