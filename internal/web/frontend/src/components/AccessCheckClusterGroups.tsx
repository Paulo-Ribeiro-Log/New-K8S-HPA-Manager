import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Copy, Loader2, ShieldAlert, ShieldQuestion, Users } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { apiClient } from "@/lib/api/client";
import type { ClusterGroup, GroupMember } from "@/lib/api/types";

const GUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// Filtro por nome ou ID: "vv_cloud_*_*" e "az_c_*" funcionam como curinga (* = qualquer trecho);
// sem curinga é "contém". Sem diferenciar maiúsculas.
function buildMatcher(term: string): (g: ClusterGroup) => boolean {
  const t = term.trim().toLowerCase();
  if (!t) return () => true;
  if (!t.includes("*")) return (g) => g.displayName.toLowerCase().includes(t) || g.id.toLowerCase().includes(t);
  const re = new RegExp(t.split("*").map((p) => p.replace(/[.+?^${}()|[\]\\]/g, "\\$&")).join(".*"), "i");
  return (g) => re.test(g.displayName) || re.test(g.id);
}

async function copyText(text: string, what: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(`${what} copiado(s) para a área de transferência`);
  } catch {
    toast.error("Falha ao copiar — verifique permissões do navegador");
  }
}

// Árvore de membros: usuários são folhas; grupos aninhados abrem sob demanda (um nível por vez).
// `ancestors` guarda os grupos do caminho para não entrar em ciclo (grupo que contém a si mesmo).
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
        <span className={`text-sm break-all ${isGroup ? "font-medium" : ""}`}>{m.displayName || "—"}</span>
        <span className="text-xs text-muted-foreground break-all">{m.mail || m.upn || ""}</span>
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
        <button
          type="button"
          onClick={onToggle}
          className="flex items-start gap-2 text-left flex-1 min-w-0"
          title={group.isObjectId ? "Mostrar bindings e membros" : "Mostrar as bindings (sem Object ID não há membros para listar)"}
        >
          {open ? <ChevronDown className="w-4 h-4 mt-0.5 shrink-0" /> : <ChevronRight className="w-4 h-4 mt-0.5 shrink-0" />}
          <div className="min-w-0">
            <div className="text-sm font-medium break-all">
              {group.displayName || <span className="text-muted-foreground italic">não resolvido no Entra ID</span>}
            </div>
            <div className="text-xs font-mono text-muted-foreground break-all">{group.id}</div>
          </div>
        </button>
        <div className="flex items-center gap-1.5 shrink-0">
          {group.risks.length > 0 && <Badge variant="destructive">acesso aberto ({group.risks.length})</Badge>}
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
            <ul className="mt-2 pl-6 text-xs text-red-500 space-y-0.5">
              {group.risks.map((r) => (
                <li key={r}>⚠ {r}</li>
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

  const { data, isLoading, isFetching, error, refetch } = useQuery({
    queryKey: ["access-check-cluster-groups", cluster],
    queryFn: () => apiClient.getAccessCheckClusterGroups(cluster),
    enabled: !!cluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const filtered = useMemo(() => {
    const match = buildMatcher(search);
    return (data?.groups ?? []).filter(match);
  }, [data, search]);
  const entraGroups = filtered.filter((g) => !g.builtin);
  const builtinGroups = filtered.filter((g) => g.builtin);
  const allRisks = (data?.groups ?? []).flatMap((g) => g.risks);

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
        </div>
      ))}

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
        <div className="mb-3 border border-red-500/50 bg-red-500/10 rounded p-3">
          <div className="flex items-center gap-2 font-semibold text-red-500">
            <ShieldAlert className="w-4 h-4" /> Acesso concedido a quem não foi autorizado individualmente ({allRisks.length})
          </div>
          <ul className="mt-1 text-xs text-muted-foreground space-y-0.5">
            {allRisks.map((r) => (
              <li key={r}>• {r}</li>
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
