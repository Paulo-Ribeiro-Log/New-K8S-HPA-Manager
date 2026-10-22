import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Copy, Loader2, ShieldAlert, ShieldQuestion, Users } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { CopyValue } from "@/components/CopyValue";
import { AzureLoginModal } from "@/components/AzureLoginModal";
import { isAzureLoginError } from "@/lib/azureError";
import { apiClient } from "@/lib/api/client";
import { copyText } from "@/lib/clipboard";
import { globMatcher } from "@/lib/groupFilter";
import type { ClusterGroup, ClusterGroupRisk, GroupMember } from "@/lib/api/types";

const GUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// Árvore de membros: usuários são folhas; grupos aninhados abrem sob demanda (um nível por vez).
// `ancestors` guarda os grupos do caminho para não entrar em ciclo (grupo que contém a si mesmo).
const SEVERITY_RANK = { "crítico": 3, alto: 2, baixo: 1 } as const;
const SEVERITY_TEXT = { "crítico": "text-red-500", alto: "text-orange-500", baixo: "text-amber-500" } as const;

function maxSeverity(risks: ClusterGroupRisk[]): ClusterGroupRisk["severity"] {
  return risks.reduce<ClusterGroupRisk["severity"]>((m, r) => (SEVERITY_RANK[r.severity] > SEVERITY_RANK[m] ? r.severity : m), "baixo");
}

// Um acesso aberto: quem recebe, qual binding/role dá o acesso e o que isso significa.
function RiskItem({ r }: { r: ClusterGroupRisk }) {
  return (
    <li className="text-xs">
      <div>
        <span className={`font-semibold uppercase ${SEVERITY_TEXT[r.severity]}`}>{r.severity}</span>
        <span className="ml-2 font-medium text-foreground">{r.who}</span>
        <span className="text-muted-foreground"> recebe a role </span>
        <span className="font-mono text-foreground">{r.role}</span>
      </div>
      <div className="text-muted-foreground">
        via <span className="font-mono">{r.binding}</span>
      </div>
      <div className="text-muted-foreground">{r.message}</div>
    </li>
  );
}

function MemberNode({ m, depth, ancestors }: { m: GroupMember; depth: number; ancestors: string[] }) {
  const [open, setOpen] = useState(false);
  const isGroup = m.type === "group";
  const cycle = isGroup && ancestors.includes(m.id.toLowerCase());
  const typeLabel = { user: "usuário", group: "subgrupo", servicePrincipal: "service principal", other: "outro" }[m.type];

  return (
    <div>
      <div className="flex items-center gap-2 py-0.5" style={{ paddingLeft: depth * 16 }}>
        {isGroup && !cycle ? (
          <button type="button" onClick={() => setOpen(!open)} className="shrink-0" title="Mostrar os membros do subgrupo">
            {open ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
          </button>
        ) : (
          <span className="w-3.5 shrink-0" />
        )}
        <CopyValue value={m.displayName} label={isGroup ? "nome do subgrupo" : "nome"} className={`text-sm break-all ${isGroup ? "font-medium" : ""}`} />
        <CopyValue value={m.mail || m.upn || ""} label="e-mail" className="text-xs text-muted-foreground break-all" />
        <CopyValue value={m.id} label="Object ID" className="text-xs font-mono text-muted-foreground/60 break-all" />
        <Badge variant={isGroup ? "secondary" : "outline"} className="ml-auto shrink-0">
          {typeLabel}
        </Badge>
      </div>
      {cycle && (
        <div className="text-xs text-amber-500" style={{ paddingLeft: (depth + 1) * 16 }}>
          ⚠ ciclo: esse grupo já está acima na árvore
        </div>
      )}
      {open && !cycle && <GroupMembers groupId={m.id} depth={depth + 1} ancestors={ancestors} />}
    </div>
  );
}

function GroupMembers({ groupId, depth = 0, ancestors = [] }: { groupId: string; depth?: number; ancestors?: string[] }) {
  const { data, isLoading, error } = useQuery({
    queryKey: ["access-check-group-members", groupId],
    queryFn: () => apiClient.getAccessCheckGroupMembers(groupId),
    staleTime: 5 * 60 * 1000,
    retry: false,
  });
  const path = [...ancestors, groupId.toLowerCase()];
  const pad = { paddingLeft: depth * 16 + (depth === 0 ? 24 : 0) };

  return (
    <div className={depth === 0 ? "mt-2" : ""}>
      {depth === 0 && data && (
        <div className="flex items-center gap-3 mb-1 pl-6">
          <span className="text-xs text-muted-foreground">
            {data.members.filter((m) => m.type === "group").length} subgrupo(s),{" "}
            {data.members.filter((m) => m.type !== "group").length} membro(s)
            {data.truncated ? " — lista cortada no limite de segurança" : ""}
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={() =>
              copyText(
                data.members.map((m) => `${m.displayName}\t${m.mail || m.upn || ""}\t${m.type}`).join("\n"),
                `${data.members.length} membro(s)`,
              )
            }
            disabled={data.members.length === 0}
          >
            <Copy className="w-3.5 h-3.5 mr-1.5" />
            Copiar membros
          </Button>
        </div>
      )}
      {isLoading && (
        <div className="text-sm text-muted-foreground flex items-center gap-2" style={pad}>
          <Loader2 className="w-4 h-4 animate-spin" /> Buscando membros no Entra ID...
        </div>
      )}
      {error && (
        <div className="text-sm text-red-500 break-words" style={pad}>
          {(error as Error).message}
        </div>
      )}
      {data && data.members.length === 0 && (
        <div className="text-sm text-muted-foreground" style={pad}>
          Grupo sem membros.
        </div>
      )}
      {data && (
        <div className={depth === 0 ? "pl-6 border-l border-border ml-2" : "border-l border-border ml-2"}>
          {data.members.map((m) => (
            <MemberNode key={m.id} m={m} depth={depth === 0 ? 0 : 1} ancestors={path} />
          ))}
        </div>
      )}
    </div>
  );
}

function GroupRow({ group, open, onToggle }: { group: ClusterGroup; open: boolean; onToggle: () => void }) {
  return (
    <div className="border-b border-border py-2">
      <div className="flex items-start gap-2">
        <div className="flex items-start gap-2 flex-1 min-w-0">
          <button
            type="button"
            onClick={onToggle}
            className="shrink-0 mt-0.5"
            title={group.isObjectId ? "Mostrar bindings e membros" : "Mostrar as bindings (sem Object ID não há membros para listar)"}
          >
            {open ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
          </button>
          {/* Fora do <button>: o navegador não deixa selecionar texto dentro de botão. */}
          <div className="min-w-0">
            <div>
              {group.displayName ? (
                <CopyValue value={group.displayName} label="nome do grupo" className="text-sm font-medium break-all" />
              ) : (
                <span className="text-sm text-muted-foreground italic">não resolvido no Entra ID</span>
              )}
            </div>
            <div>
              <CopyValue value={group.id} label={group.isObjectId ? "Object ID" : "nome"} className="text-xs font-mono text-muted-foreground break-all" />
            </div>
          </div>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          {group.risks.length > 0 && (
            <Badge variant={maxSeverity(group.risks) === "baixo" ? "secondary" : "destructive"}>
              acesso aberto: {maxSeverity(group.risks)} ({group.risks.length})
            </Badge>
          )}
          {group.sources.includes("aks-admin") && <Badge variant="destructive">admin AKS</Badge>}
          {group.sources.includes("rbac") && <Badge variant="secondary">RBAC ({group.bindings.length})</Badge>}
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7"
            title="Copiar nome e ID"
            onClick={() => copyText(`${group.displayName}\t${group.id}`, "Grupo")}
          >
            <Copy className="w-3.5 h-3.5" />
          </Button>
        </div>
      </div>
      {open && (
        <>
          {group.risks.length > 0 && (
            <ul className="mt-2 pl-6 space-y-2">
              {group.risks.map((r) => (
                <RiskItem key={`${r.binding}/${r.role}`} r={r} />
              ))}
            </ul>
          )}
          {group.bindings.length > 0 && (
            <ul className="mt-2 pl-6 text-xs text-muted-foreground space-y-0.5">
              {group.bindings.map((b, i) => (
                <li key={`${b.kind}/${b.namespace ?? ""}/${b.name}/${i}`}>
                  <span className="font-mono">{b.kind}</span> {b.namespace ? `${b.namespace}/` : ""}
                  {b.name} → <span className="font-mono">{b.role_kind}/{b.role_name}</span>
                </li>
              ))}
            </ul>
          )}
          {group.isObjectId && <GroupMembers groupId={group.id} />}
          {!group.isObjectId && (
            <div className="mt-2 pl-6 text-xs text-muted-foreground">
              {group.builtin
                ? "Grupo interno do Kubernetes: não existe no Entra ID, então não há lista de membros."
                : "Subject citado pelo nome (não é Object ID): não dá para listar os membros."}
            </div>
          )}
        </>
      )}
    </div>
  );
}

export default function AccessCheckClusterGroups({ cluster }: { cluster: string }) {
  const [search, setSearch] = useState("");
  const [openId, setOpenId] = useState<string | null>(null);
  const [loginDismissed, setLoginDismissed] = useState("");

  const { data, isLoading, isFetching, error, refetch } = useQuery({
    queryKey: ["access-check-cluster-groups", cluster],
    queryFn: () => apiClient.getAccessCheckClusterGroups(cluster),
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  // Sessão do `az` expirada: avisos do servidor (ou o erro da própria chamada) que pedem `az login`.
  const loginErrors = useMemo(() => {
    const texts = [...(data?.warnings ?? []), ...(error ? [(error as Error).message] : [])];
    return texts.filter(isAzureLoginError);
  }, [data, error]);
  const loginKey = loginErrors.join("\n");
  const loginModalOpen = loginErrors.length > 0 && loginDismissed !== loginKey;

  const filtered = useMemo(() => {
    const match = globMatcher(search);
    return (data?.groups ?? []).filter((g) => match(g.displayName, g.id));
  }, [data, search]);
  const entraGroups = filtered.filter((g) => !g.builtin);
  const builtinGroups = filtered.filter((g) => g.builtin);
  const allRisks = (data?.groups ?? []).flatMap((g) => g.risks);
  const topSeverity = maxSeverity(allRisks);

  // ID colado que não está na lista do cluster: dá para consultar o grupo mesmo assim.
  const typed = search.trim();
  const extraId = GUID_RE.test(typed) && !(data?.groups ?? []).some((g) => g.id === typed.toLowerCase()) ? typed.toLowerCase() : null;

  if (!cluster) {
    return (
      <div className="text-sm text-muted-foreground flex items-center gap-2">
        <ShieldQuestion className="w-4 h-4" />
        Selecione o cluster acima para listar os grupos do Entra ID que ele usa.
      </div>
    );
  }

  return (
    <>
      <div className="flex items-center justify-between mb-3 gap-3">
        <p className="text-sm text-muted-foreground">
          Grupos do Entra ID em <span className="font-medium text-foreground">{cluster}</span>: administradores do AKS e
          grupos citados em RoleBindings/ClusterRoleBindings. Clique no grupo para ver os membros.
        </p>
        <div className="flex items-center gap-2 shrink-0">
          <Input
            className="w-[280px]"
            placeholder="Nome (vv_cloud_*, az_c_*) ou Object ID"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
            {isFetching && <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />}
            Atualizar
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={filtered.length === 0}
            onClick={() => copyText(filtered.map((g) => `${g.displayName}\t${g.id}`).join("\n"), `${filtered.length} grupo(s)`)}
          >
            <Copy className="w-3.5 h-3.5 mr-1.5" />
            Copiar lista
          </Button>
        </div>
      </div>

      {isLoading && (
        <div className="text-sm text-muted-foreground flex items-center gap-2">
          <Loader2 className="w-4 h-4 animate-spin" /> Lendo o AKS, o RBAC do cluster e o Entra ID...
        </div>
      )}
      {error && <div className="text-sm text-red-500 break-words">{(error as Error).message}</div>}
      {data?.warnings.map((w) => (
        <div key={w} className="text-xs text-amber-500 mb-1 break-words">
          ⚠ {w}
          {isAzureLoginError(w) && (
            <button type="button" className="ml-2 underline" onClick={() => setLoginDismissed("")}>
              como resolver
            </button>
          )}
        </div>
      ))}
      <AzureLoginModal
        open={loginModalOpen}
        onOpenChange={(o) => !o && setLoginDismissed(loginKey)}
        errors={loginErrors}
        onRetry={() => refetch()}
      />

      {extraId && (
        <div className="mb-2 border border-dashed border-border rounded p-2">
          <div className="flex items-center gap-2 text-sm">
            <Users className="w-4 h-4" /> Esse ID não aparece no cluster. Consultar mesmo assim:
            <Button size="sm" variant="outline" onClick={() => setOpenId(extraId)}>
              Ver membros
            </Button>
          </div>
          {openId === extraId && <GroupMembers groupId={extraId} />}
        </div>
      )}

      {data && (
        <div className="text-xs text-muted-foreground mb-1">
          {filtered.length} de {data.groups.length} grupo(s)
        </div>
      )}
      {allRisks.length > 0 && (
        <div
          className={`mb-3 border rounded p-3 ${
            topSeverity === "baixo" ? "border-amber-500/50 bg-amber-500/10" : "border-red-500/50 bg-red-500/10"
          }`}
        >
          <div className={`flex items-center gap-2 font-semibold ${SEVERITY_TEXT[topSeverity]}`}>
            <ShieldAlert className="w-4 h-4" /> Permissões abertas a todos ({allRisks.length}) — gravidade máxima: {topSeverity}
          </div>
          <div className="text-xs text-muted-foreground mt-1 mb-2">
            Bindings que dão permissão a quem não tem login (anônimo) ou a qualquer usuário logado, além do que o
            Kubernetes concede por padrão.
          </div>
          <ul className="space-y-2">
            {[...allRisks]
              .sort((x, y) => SEVERITY_RANK[y.severity] - SEVERITY_RANK[x.severity])
              .map((r) => (
                <RiskItem key={`${r.who}/${r.binding}/${r.role}`} r={r} />
              ))}
          </ul>
        </div>
      )}

      {data && <div className="text-sm font-semibold mt-2">Grupos do Entra ID ({entraGroups.length})</div>}
      {entraGroups.map((g) => (
        <GroupRow key={g.id} group={g} open={openId === g.id} onToggle={() => setOpenId(openId === g.id ? null : g.id)} />
      ))}

      {builtinGroups.length > 0 && (
        <>
          <div className="text-sm font-semibold mt-5">Grupos internos do Kubernetes ({builtinGroups.length})</div>
          <div className="text-xs text-muted-foreground mb-1">
            Não são do Entra ID (system:*). Abertos a todos: system:unauthenticated e system:authenticated.
          </div>
          {builtinGroups.map((g) => (
            <GroupRow key={g.id} group={g} open={openId === g.id} onToggle={() => setOpenId(openId === g.id ? null : g.id)} />
          ))}
        </>
      )}
    </>
  );
}
