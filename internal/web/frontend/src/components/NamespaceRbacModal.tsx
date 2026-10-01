import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, ChevronDown, ChevronRight, Copy, KeyRound, Loader2, RefreshCcw, Search, Users } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import type { RBACBinding, RBACOverview, RBACPolicyRule, RBACSquad, RBACSubject } from "@/lib/api/types";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

// RBAC da aba Namespaces: cruza as squads declaradas no namespace (label/annotation
// squads.devops.k8s.io/<id>) com os subjects das RoleBindings/ClusterRoleBindings.
// Dois modos: "Por namespace" (quem acessa o namespace e se a squad declarada tem binding)
// e "Por grupo" (onde um grupo/squad tem acesso e com quais regras). Tudo vem de uma única
// chamada (GET /rbac/overview); os modos são só visões diferentes dos mesmos dados.

type Mode = "namespace" | "group";

const roleKey = (b: RBACBinding) =>
  b.role_kind === "ClusterRole" ? `ClusterRole/${b.role_name}` : `Role/${b.namespace}/${b.role_name}`;
const subjectKey = (s: RBACSubject) => `${s.kind}|${s.namespace ?? ""}|${s.name}`;
const isSystemSubject = (s: RBACSubject) => s.name.startsWith("system:");
const kindOrder: Record<string, number> = { Group: 0, User: 1, ServiceAccount: 2 };

function copy(text: string) {
  navigator.clipboard.writeText(text).then(() => toast.success("Copiado"), () => toast.error("Falha ao copiar"));
}

function RoleRules({ rules }: { rules: RBACPolicyRule[] | undefined }) {
  if (!rules) {
    return <p className="text-xs text-muted-foreground px-2 py-1">Role não encontrada ou sem permissão para lê-la.</p>;
  }
  if (rules.length === 0) return <p className="text-xs text-muted-foreground px-2 py-1">Role sem regras.</p>;
  return (
    <table className="w-full text-[11px] font-mono">
      <thead>
        <tr className="text-muted-foreground text-left">
          <th className="px-2 py-1 font-medium">Verbos</th>
          <th className="px-2 py-1 font-medium">Recursos</th>
          <th className="px-2 py-1 font-medium">Nomes</th>
        </tr>
      </thead>
      <tbody>
        {rules.map((r, i) => {
          const groups = r.apiGroups ?? [];
          const resources = r.nonResourceURLs?.length
            ? r.nonResourceURLs
            : (r.resources ?? []).flatMap((res) =>
                groups.length === 0 ? [res] : groups.map((g) => (g === "" ? res : `${res}.${g}`))
              );
          const wildcard = r.verbs.includes("*") || resources.some((x) => x.startsWith("*"));
          return (
            <tr key={i} className="border-t border-border/30 align-top">
              <td className={`px-2 py-1 ${wildcard ? "text-red-400" : ""}`}>{r.verbs.join(", ")}</td>
              <td className="px-2 py-1 break-all">{resources.join(", ") || "—"}</td>
              <td className="px-2 py-1 break-all text-muted-foreground">{r.resourceNames?.join(", ") || "todos"}</td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function BindingRow({ binding, roles }: { binding: RBACBinding; roles: RBACOverview["roles"] }) {
  const [open, setOpen] = useState(false);
  const cluster = binding.kind === "ClusterRoleBinding";
  return (
    <div className="border border-border/40 rounded">
      <button className="w-full flex items-center gap-2 px-2 py-1 text-xs text-left hover:bg-muted/40" onClick={() => setOpen((v) => !v)}>
        {open ? <ChevronDown className="w-3 h-3 flex-shrink-0" /> : <ChevronRight className="w-3 h-3 flex-shrink-0" />}
        <Badge variant={cluster ? "destructive" : "secondary"} className="text-[10px] px-1 py-0">
          {cluster ? "Cluster" : binding.namespace}
        </Badge>
        <span className="font-mono truncate" title={`${binding.kind} ${binding.name}`}>{binding.name}</span>
        <span className="text-muted-foreground">→</span>
        <span className="font-mono font-semibold truncate">{binding.role_kind}/{binding.role_name}</span>
      </button>
      {open && (
        <div className="border-t border-border/30 bg-muted/20">
          <RoleRules rules={roles[roleKey(binding)]} />
        </div>
      )}
    </div>
  );
}

export function NamespaceRbacModal({
  open,
  onOpenChange,
  cluster,
  initialNamespace,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  initialNamespace?: string;
}) {
  const [mode, setMode] = useState<Mode>(initialNamespace ? "namespace" : "group");
  const [namespace, setNamespace] = useState(initialNamespace ?? "");
  const [group, setGroup] = useState("");
  const [groupSearch, setGroupSearch] = useState("");
  const [hideSystem, setHideSystem] = useState(true);
  const [hideServiceAccounts, setHideServiceAccounts] = useState(true);
  const [includeClusterBindings, setIncludeClusterBindings] = useState(true);

  useEffect(() => {
    if (!open) return;
    setMode(initialNamespace ? "namespace" : "group");
    if (initialNamespace) setNamespace(initialNamespace);
  }, [open, initialNamespace]);

  const { data, isLoading, isFetching, error, refetch } = useQuery({
    queryKey: ["rbac-overview", cluster],
    queryFn: () => apiClient.getRBACOverview(cluster),
    enabled: open && !!cluster,
    staleTime: 60_000,
  });

  // Índices derivados
  const idx = useMemo(() => {
    const squadName = new Map<string, string>();
    const squadsByNs = new Map<string, RBACSquad[]>();
    const nsBySquad = new Map<string, RBACSquad[]>();
    for (const s of data?.squads ?? []) {
      if (s.name && !squadName.has(s.id)) squadName.set(s.id, s.name);
      squadsByNs.set(s.namespace, [...(squadsByNs.get(s.namespace) ?? []), s]);
      nsBySquad.set(s.id, [...(nsBySquad.get(s.id) ?? []), s]);
    }
    return { squadName, squadsByNs, nsBySquad };
  }, [data]);

  const subjectVisible = (s: RBACSubject) =>
    !(hideSystem && isSystemSubject(s)) && !(hideServiceAccounts && s.kind === "ServiceAccount");

  const subjectLabel = (s: RBACSubject) => {
    if (s.kind === "ServiceAccount") return `${s.namespace}/${s.name}`;
    return idx.squadName.get(s.name) ?? s.name;
  };

  // ── Modo "Por namespace" ──
  const nsView = useMemo(() => {
    if (!data || !namespace) return null;
    const relevant = data.bindings.filter(
      (b) => (b.kind === "RoleBinding" && b.namespace === namespace) || (includeClusterBindings && b.kind === "ClusterRoleBinding")
    );
    const rows = new Map<string, { subject: RBACSubject; bindings: RBACBinding[] }>();
    for (const b of relevant) {
      for (const s of b.subjects) {
        if (!subjectVisible(s)) continue;
        const k = subjectKey(s);
        const row = rows.get(k) ?? { subject: s, bindings: [] };
        row.bindings.push(b);
        rows.set(k, row);
      }
    }
    const squads = idx.squadsByNs.get(namespace) ?? [];
    const squadIds = new Set(squads.map((s) => s.id));
    const sorted = [...rows.values()].sort(
      (a, b) =>
        Number(squadIds.has(b.subject.name)) - Number(squadIds.has(a.subject.name)) ||
        (kindOrder[a.subject.kind] ?? 9) - (kindOrder[b.subject.kind] ?? 9) ||
        subjectLabel(a.subject).localeCompare(subjectLabel(b.subject))
    );
    const groupBindings = (id: string) =>
      data.bindings.filter((b) => b.subjects.some((s) => s.kind === "Group" && s.name === id));
    return {
      squads: squads.map((sq) => {
        const bs = groupBindings(sq.id);
        return {
          squad: sq,
          nsBindings: bs.filter((b) => b.kind === "RoleBinding" && b.namespace === namespace),
          clusterBindings: bs.filter((b) => b.kind === "ClusterRoleBinding"),
        };
      }),
      squadIds,
      rows: sorted,
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, namespace, includeClusterBindings, hideSystem, hideServiceAccounts, idx]);

  // ── Modo "Por grupo" ──
  const groups = useMemo(() => {
    if (!data) return [];
    const m = new Map<string, { id: string; nsBound: Set<string>; cluster: number }>();
    const get = (id: string) => {
      const g = m.get(id) ?? { id, nsBound: new Set<string>(), cluster: 0 };
      m.set(id, g);
      return g;
    };
    for (const b of data.bindings) {
      for (const s of b.subjects) {
        if (s.kind !== "Group" || (hideSystem && isSystemSubject(s))) continue;
        const g = get(s.name);
        if (b.kind === "ClusterRoleBinding") g.cluster++;
        else if (b.namespace) g.nsBound.add(b.namespace);
      }
    }
    for (const id of idx.nsBySquad.keys()) get(id);
    return [...m.values()]
      .map((g) => ({ ...g, name: idx.squadName.get(g.id) ?? "", nsAnnotated: idx.nsBySquad.get(g.id)?.length ?? 0 }))
      .sort((a, b) => (a.name || "~" + a.id).localeCompare(b.name || "~" + b.id));
  }, [data, idx, hideSystem]);

  const filteredGroups = useMemo(() => {
    const q = groupSearch.trim().toLowerCase();
    if (!q) return groups;
    return groups.filter((g) => g.id.toLowerCase().includes(q) || g.name.toLowerCase().includes(q));
  }, [groups, groupSearch]);

  const groupView = useMemo(() => {
    if (!data || !group) return null;
    const bs = data.bindings.filter((b) => b.subjects.some((s) => s.kind === "Group" && s.name === group));
    const annotated = new Map((idx.nsBySquad.get(group) ?? []).map((s) => [s.namespace, s]));
    const byNs = new Map<string, RBACBinding[]>();
    for (const b of bs) {
      if (b.kind === "RoleBinding" && b.namespace) byNs.set(b.namespace, [...(byNs.get(b.namespace) ?? []), b]);
    }
    const namespaces = [...new Set([...annotated.keys(), ...byNs.keys()])].sort();
    return {
      clusterBindings: bs.filter((b) => b.kind === "ClusterRoleBinding"),
      rows: namespaces.map((ns) => ({ ns, squad: annotated.get(ns), bindings: byNs.get(ns) ?? [] })),
    };
  }, [data, group, idx]);

  const goToGroup = (id: string) => {
    setGroup(id);
    setMode("group");
  };
  const goToNamespace = (ns: string) => {
    setNamespace(ns);
    setMode("namespace");
  };

  const groupTitle = (id: string) => {
    const name = idx.squadName.get(id);
    return (
      <span className="flex items-center gap-1.5 min-w-0">
        {name && <span className="font-semibold truncate">{name}</span>}
        <span className={`font-mono text-[11px] truncate ${name ? "text-muted-foreground" : ""}`}>{id}</span>
        <button title="Copiar ID" className="text-muted-foreground hover:text-foreground flex-shrink-0" onClick={(e) => { e.stopPropagation(); copy(id); }}>
          <Copy className="w-3 h-3" />
        </button>
      </span>
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-6xl h-[85vh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2">
            <KeyRound className="w-5 h-5" />
            RBAC e squads
          </DialogTitle>
          <DialogDescription className="flex items-center justify-between gap-2">
            <span className="truncate">
              {cluster} · squads de <span className="font-mono">squads.devops.k8s.io/&lt;id&gt;</span> × RoleBindings/ClusterRoleBindings
            </span>
            <Button variant="outline" size="sm" onClick={() => refetch()} disabled={!cluster || isFetching}>
              {isFetching ? <Loader2 className="w-4 h-4 mr-1 animate-spin" /> : <RefreshCcw className="w-4 h-4 mr-1" />}
              Atualizar
            </Button>
          </DialogDescription>
        </DialogHeader>

        {/* Abas manuais (shadcn <Tabs> quebra a cadeia flex-1 min-h-0) + filtros */}
        <div className="flex items-center gap-3 border-b border-border/50 flex-shrink-0 flex-wrap">
          {(["namespace", "group"] as Mode[]).map((m) => (
            <button
              key={m}
              onClick={() => setMode(m)}
              className={`px-3 py-1.5 text-sm ${mode === m ? "border-b-2 border-primary text-foreground" : "text-muted-foreground hover:text-foreground"}`}
            >
              {m === "namespace" ? "Por namespace" : "Por grupo"}
            </button>
          ))}
          <div className="ml-auto flex items-center gap-4 text-xs pb-1">
            <Label className="flex items-center gap-1.5 text-xs font-normal cursor-pointer">
              <Switch checked={hideSystem} onCheckedChange={setHideSystem} /> Ocultar system:*
            </Label>
            {mode === "namespace" && (
              <>
                <Label className="flex items-center gap-1.5 text-xs font-normal cursor-pointer">
                  <Switch checked={hideServiceAccounts} onCheckedChange={setHideServiceAccounts} /> Ocultar ServiceAccounts
                </Label>
                <Label className="flex items-center gap-1.5 text-xs font-normal cursor-pointer">
                  <Switch checked={includeClusterBindings} onCheckedChange={setIncludeClusterBindings} /> Incluir ClusterRoleBindings
                </Label>
              </>
            )}
          </div>
        </div>

        {data && data.warnings.length > 0 && (
          <div className="flex-shrink-0 text-xs text-yellow-400 bg-yellow-500/10 border border-yellow-500/30 rounded px-2 py-1">
            {data.warnings.map((w) => <div key={w}>⚠ {w}</div>)}
          </div>
        )}

        <div className="flex-1 min-h-0 flex flex-col">
          {isLoading && (
            <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground gap-2">
              <Loader2 className="w-4 h-4 animate-spin" /> Lendo RBAC do cluster…
            </div>
          )}
          {error && (
            <div className="text-sm text-red-400 p-4">Erro ao ler o RBAC: {(error as Error).message}</div>
          )}

          {/* ── Por namespace ── */}
          {data && mode === "namespace" && (
            <div className="flex-1 min-h-0 flex flex-col gap-3 pt-2">
              <div className="flex-shrink-0 w-80">
                <Select value={namespace} onValueChange={setNamespace}>
                  <SelectTrigger className="h-8 text-xs"><SelectValue placeholder="Escolha um namespace" /></SelectTrigger>
                  <SelectContent>
                    {data.namespaces.map((ns) => (
                      <SelectItem key={ns} value={ns} className="text-xs">
                        {ns}{idx.squadsByNs.has(ns) ? ` · ${idx.squadsByNs.get(ns)!.length} squad(s)` : ""}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {nsView && (
                <div className="flex-1 min-h-0 overflow-auto space-y-4 pr-1">
                  <section>
                    <h3 className="text-sm font-semibold mb-2 flex items-center gap-2"><Users className="w-4 h-4" /> Squads declaradas</h3>
                    {nsView.squads.length === 0 && (
                      <p className="text-xs text-muted-foreground">Nenhuma label/annotation <span className="font-mono">squads.devops.k8s.io/*</span> neste namespace.</p>
                    )}
                    <div className="space-y-2">
                      {nsView.squads.map(({ squad, nsBindings, clusterBindings }) => {
                        const hasAccess = nsBindings.length > 0 || clusterBindings.length > 0;
                        return (
                          <div key={squad.id} className="border border-border/50 rounded p-2 space-y-1.5">
                            <div className="flex items-center gap-2 flex-wrap text-xs">
                              <button className="hover:underline min-w-0" onClick={() => goToGroup(squad.id)} title="Ver onde este grupo tem acesso">
                                {groupTitle(squad.id)}
                              </button>
                              {squad.in_label && <Badge variant="outline" className="text-[10px] px-1 py-0">label</Badge>}
                              {squad.in_annotation && <Badge variant="outline" className="text-[10px] px-1 py-0">annotation</Badge>}
                              <span className="ml-auto">
                                {hasAccess ? (
                                  <span className="flex items-center gap-1 text-green-400"><CheckCircle2 className="w-3.5 h-3.5" /> com RBAC</span>
                                ) : (
                                  <span className="flex items-center gap-1 text-yellow-400" title="Nenhuma RoleBinding/ClusterRoleBinding tem um subject Group com este ID">
                                    <AlertTriangle className="w-3.5 h-3.5" /> sem RoleBinding para este ID
                                  </span>
                                )}
                              </span>
                            </div>
                            {[...nsBindings, ...clusterBindings].map((b) => (
                              <BindingRow key={`${b.kind}/${b.namespace}/${b.name}`} binding={b} roles={data.roles} />
                            ))}
                          </div>
                        );
                      })}
                    </div>
                  </section>

                  <section>
                    <h3 className="text-sm font-semibold mb-2 flex items-center gap-2">
                      <KeyRound className="w-4 h-4" /> Quem tem acesso a <span className="font-mono">{namespace}</span>
                      <Badge variant="secondary" className="text-[10px]">{nsView.rows.length}</Badge>
                    </h3>
                    {nsView.rows.length === 0 && <p className="text-xs text-muted-foreground">Nenhum subject com os filtros atuais.</p>}
                    <div className="space-y-2">
                      {nsView.rows.map(({ subject, bindings }) => (
                        <div key={subjectKey(subject)} className="border border-border/40 rounded p-2 space-y-1.5">
                          <div className="flex items-center gap-2 text-xs flex-wrap">
                            <Badge variant="outline" className="text-[10px] px-1 py-0">{subject.kind}</Badge>
                            {subject.kind === "Group" ? (
                              <button className="hover:underline min-w-0" onClick={() => goToGroup(subject.name)}>{groupTitle(subject.name)}</button>
                            ) : (
                              <span className="font-mono truncate">{subjectLabel(subject)}</span>
                            )}
                            {subject.kind === "Group" && nsView.squadIds.has(subject.name) && (
                              <Badge className="text-[10px] px-1 py-0 bg-green-600/80">squad do namespace</Badge>
                            )}
                          </div>
                          {bindings.map((b) => (
                            <BindingRow key={`${b.kind}/${b.namespace}/${b.name}`} binding={b} roles={data.roles} />
                          ))}
                        </div>
                      ))}
                    </div>
                  </section>
                </div>
              )}
            </div>
          )}

          {/* ── Por grupo ── */}
          {data && mode === "group" && (
            <div className="flex-1 min-h-0 flex gap-3 pt-2">
              <div className="w-80 flex-shrink-0 flex flex-col min-h-0 border border-border/40 rounded">
                <div className="p-2 border-b border-border/40 flex-shrink-0">
                  <div className="relative">
                    <Search className="w-3.5 h-3.5 absolute left-2 top-1/2 -translate-y-1/2 text-muted-foreground" />
                    <Input value={groupSearch} onChange={(e) => setGroupSearch(e.target.value)} placeholder="Buscar por nome da squad ou ID…" className="h-7 text-xs pl-7" />
                  </div>
                  <p className="text-[10px] text-muted-foreground mt-1">{filteredGroups.length} grupo(s)</p>
                </div>
                <div className="flex-1 min-h-0 overflow-auto p-1">
                  {filteredGroups.map((g) => (
                    <button
                      key={g.id}
                      onClick={() => setGroup(g.id)}
                      className={`w-full text-left px-2 py-1.5 rounded text-xs hover:bg-muted/50 ${group === g.id ? "bg-muted" : ""}`}
                    >
                      <div className="truncate font-medium">{g.name || <span className="font-mono">{g.id}</span>}</div>
                      {g.name && <div className="truncate font-mono text-[10px] text-muted-foreground">{g.id}</div>}
                      <div className="text-[10px] text-muted-foreground">
                        {g.nsBound.size} ns com binding · {g.nsAnnotated} ns declarado(s){g.cluster > 0 ? ` · ${g.cluster} cluster-wide` : ""}
                      </div>
                    </button>
                  ))}
                </div>
              </div>

              <div className="flex-1 min-w-0 min-h-0 overflow-auto pr-1">
                {!groupView && <p className="text-sm text-muted-foreground p-4">Escolha um grupo à esquerda.</p>}
                {groupView && (
                  <div className="space-y-4">
                    <div className="text-sm">{groupTitle(group)}</div>

                    <section>
                      <h3 className="text-sm font-semibold mb-2">Acesso cluster-wide (ClusterRoleBindings)</h3>
                      {groupView.clusterBindings.length === 0 ? (
                        <p className="text-xs text-muted-foreground">Nenhum.</p>
                      ) : (
                        <div className="space-y-1.5">
                          {groupView.clusterBindings.map((b) => <BindingRow key={b.name} binding={b} roles={data.roles} />)}
                        </div>
                      )}
                    </section>

                    <section>
                      <h3 className="text-sm font-semibold mb-2">Namespaces ({groupView.rows.length})</h3>
                      {groupView.rows.length === 0 && <p className="text-xs text-muted-foreground">Nenhum namespace declara este grupo nem tem RoleBinding para ele.</p>}
                      <div className="space-y-2">
                        {groupView.rows.map(({ ns, squad, bindings }) => (
                          <div key={ns} className="border border-border/40 rounded p-2 space-y-1.5">
                            <div className="flex items-center gap-2 text-xs flex-wrap">
                              <button className="font-mono font-semibold hover:underline" onClick={() => goToNamespace(ns)}>{ns}</button>
                              {squad ? (
                                <Badge variant="outline" className="text-[10px] px-1 py-0">declarado como squad</Badge>
                              ) : (
                                <Badge variant="outline" className="text-[10px] px-1 py-0 text-yellow-400 border-yellow-500/50" title="Há RoleBinding para o grupo, mas o namespace não tem a label/annotation da squad">
                                  não declarado como squad
                                </Badge>
                              )}
                              {bindings.length === 0 && (
                                <span className="ml-auto flex items-center gap-1 text-yellow-400">
                                  <AlertTriangle className="w-3.5 h-3.5" /> sem RoleBinding
                                </span>
                              )}
                            </div>
                            {bindings.map((b) => <BindingRow key={b.name} binding={b} roles={data.roles} />)}
                          </div>
                        ))}
                      </div>
                    </section>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
