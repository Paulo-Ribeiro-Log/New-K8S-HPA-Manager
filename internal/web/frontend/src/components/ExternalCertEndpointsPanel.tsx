import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Loader2, Plus, RefreshCcw, Trash2, Pencil, Globe } from "lucide-react";
import { toast } from "sonner";
import { getStatusBadge } from "@/components/CertificateDetailModal";
import { ExternalEndpointDetailModal } from "@/components/ExternalEndpointDetailModal";
import {
  useCertEndpoints,
  useCreateCertEndpoint,
  useUpdateCertEndpoint,
  useDeleteCertEndpoint,
  useCheckCertEndpoint,
  useCheckAllCertEndpoints,
} from "@/hooks/useCertEndpoints";
import type { CertEndpointWithStatus } from "@/lib/api/types";

type EndpointForm = {
  name: string;
  host: string;
  port: number;
  sni: string;
  group_label: string;
  enabled: boolean;
};

const emptyForm: EndpointForm = { name: "", host: "", port: 443, sni: "", group_label: "", enabled: true };

// Sub-aba "Endpoints Externos" — monitor de certificados TLS de servidores fora de qualquer
// cluster K8s (on-prem Windows/Linux, serviços externos), estilo blackbox_exporter do
// Prometheus mas sem depender dele: handshake TLS real sob demanda. Ver
// EXTERNAL-CERT-MONITOR-PLAN.md.
export function ExternalCertEndpointsPanel() {
  const { data: endpoints = [], isLoading, isError, refetch } = useCertEndpoints();
  const createMutation = useCreateCertEndpoint();
  const updateMutation = useUpdateCertEndpoint();
  const deleteMutation = useDeleteCertEndpoint();
  const checkOneMutation = useCheckCertEndpoint();
  const checkAllMutation = useCheckAllCertEndpoints();

  const [formOpen, setFormOpen] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);
  const [form, setForm] = useState<EndpointForm>(emptyForm);
  const [deleteTarget, setDeleteTarget] = useState<CertEndpointWithStatus | null>(null);
  const [detailTarget, setDetailTarget] = useState<CertEndpointWithStatus | null>(null);
  const [checkingId, setCheckingId] = useState<number | null>(null);

  const openCreateForm = () => {
    setEditingId(null);
    setForm(emptyForm);
    setFormOpen(true);
  };

  const openEditForm = (e: CertEndpointWithStatus) => {
    setEditingId(e.id);
    setForm({
      name: e.name,
      host: e.host,
      port: e.port,
      sni: e.sni ?? "",
      group_label: e.group_label ?? "",
      enabled: e.enabled,
    });
    setFormOpen(true);
  };

  const handleSubmit = async () => {
    if (!form.name.trim() || !form.host.trim()) {
      toast.error("Nome e Host são obrigatórios");
      return;
    }
    try {
      if (editingId != null) {
        await updateMutation.mutateAsync({ id: editingId, data: form });
        toast.success("Endpoint atualizado");
      } else {
        // enabled é ignorado pelo backend em Create (endpoint sempre nasce habilitado) —
        // enviar mesmo assim não tem efeito, mantém o form data uniforme entre create/edit.
        await createMutation.mutateAsync(form);
        toast.success("Endpoint cadastrado");
      }
      setFormOpen(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Erro ao salvar endpoint");
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await deleteMutation.mutateAsync(deleteTarget.id);
      toast.success("Endpoint removido");
      setDeleteTarget(null);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Erro ao remover endpoint");
    }
  };

  const handleCheckOne = async (id: number) => {
    setCheckingId(id);
    try {
      await checkOneMutation.mutateAsync(id);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Erro ao verificar endpoint");
    } finally {
      setCheckingId(null);
    }
  };

  const handleCheckAll = async () => {
    try {
      await checkAllMutation.mutateAsync();
      toast.success("Verificação concluída");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Erro ao verificar endpoints");
    }
  };

  // "Última verificação" no cabeçalho = a mais recente entre todos os endpoints — os
  // checked_at do backend são RFC3339 UTC, comparáveis lexicograficamente sem parsear Date.
  const lastCheckedAt = endpoints.reduce<string | null>((latest, e) => {
    const at = e.latest_check?.checked_at;
    if (!at) return latest;
    if (!latest || at > latest) return at;
    return latest;
  }, null);

  return (
    <div className="h-full flex flex-col min-h-0">
      <div className="flex items-center justify-between flex-shrink-0 mb-3 gap-2 flex-wrap">
        <div className="text-xs text-muted-foreground max-w-md">
          Certificados TLS de servidores fora de qualquer cluster K8s — handshake real, sem
          depender de Prometheus/blackbox_exporter.
          {lastCheckedAt && (
            <span className="block opacity-70 mt-0.5">
              Última verificação: {new Date(lastCheckedAt).toLocaleString("pt-BR")}
            </span>
          )}
        </div>
        <div className="flex gap-2 flex-shrink-0">
          <Button
            variant="outline"
            size="sm"
            onClick={handleCheckAll}
            disabled={checkAllMutation.isPending || endpoints.length === 0}
          >
            {checkAllMutation.isPending ? (
              <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />
            ) : (
              <RefreshCcw className="h-3.5 w-3.5 mr-1.5" />
            )}
            Verificar agora
          </Button>
          <Button size="sm" onClick={openCreateForm}>
            <Plus className="h-3.5 w-3.5 mr-1.5" />
            Adicionar Endpoint
          </Button>
        </div>
      </div>

      <div className="flex-1 min-h-0 overflow-auto border border-border rounded-md">
        {isLoading ? (
          <div className="flex items-center justify-center py-12 text-sm text-muted-foreground gap-2">
            <Loader2 className="h-4 w-4 animate-spin" /> Carregando...
          </div>
        ) : isError ? (
          <div className="flex flex-col items-center justify-center py-12 gap-2">
            <p className="text-sm text-muted-foreground">Erro ao carregar endpoints</p>
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              Tentar novamente
            </Button>
          </div>
        ) : endpoints.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 gap-2 text-center px-4">
            <Globe className="h-8 w-8 text-muted-foreground opacity-50" />
            <p className="text-sm text-muted-foreground">Nenhum endpoint cadastrado ainda</p>
            <p className="text-xs text-muted-foreground">
              Clique em "Adicionar Endpoint" para monitorar um servidor on-prem ou serviço externo
            </p>
          </div>
        ) : (
          <table className="w-full text-xs">
            <thead className="sticky top-0 bg-background border-b border-border z-10">
              <tr className="text-left text-muted-foreground">
                <th className="px-3 py-2 font-medium">Nome</th>
                <th className="px-3 py-2 font-medium">Host:Porta</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Emissor</th>
                <th className="px-3 py-2 font-medium">Última verificação</th>
                <th className="px-3 py-2 font-medium text-right">Ações</th>
              </tr>
            </thead>
            <tbody>
              {endpoints.map((e) => (
                <tr
                  key={e.id}
                  className={`border-b border-border/50 hover:bg-muted/30 cursor-pointer transition-colors ${
                    !e.enabled ? "opacity-50" : ""
                  }`}
                  onClick={() => setDetailTarget(e)}
                >
                  <td className="px-3 py-2 font-medium">
                    {e.name}
                    {!e.enabled && (
                      <Badge variant="secondary" className="ml-1.5 text-[10px]">
                        desabilitado
                      </Badge>
                    )}
                    {e.group_label && (
                      <Badge variant="outline" className="ml-1.5 text-[10px]">
                        {e.group_label}
                      </Badge>
                    )}
                  </td>
                  <td className="px-3 py-2 font-mono">
                    {e.host}:{e.port}
                    {e.sni ? ` (SNI: ${e.sni})` : ""}
                  </td>
                  <td className="px-3 py-2">
                    {!e.latest_check ? (
                      <Badge variant="secondary">Nunca verificado</Badge>
                    ) : !e.latest_check.success ? (
                      <Badge
                        className="bg-red-500/20 text-red-400 border-red-500/30"
                        title={e.latest_check.error_message}
                      >
                        Erro
                      </Badge>
                    ) : (
                      <span className="inline-flex items-center gap-1.5">
                        {getStatusBadge(e.latest_check.status || "")}
                        <span className="text-muted-foreground">{e.latest_check.days_remaining}d</span>
                      </span>
                    )}
                  </td>
                  <td className="px-3 py-2 text-muted-foreground">{e.latest_check?.issuer || "—"}</td>
                  <td className="px-3 py-2 text-muted-foreground">
                    {e.latest_check?.checked_at
                      ? new Date(e.latest_check.checked_at).toLocaleString("pt-BR")
                      : "—"}
                  </td>
                  <td className="px-3 py-2" onClick={(ev) => ev.stopPropagation()}>
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 w-7 p-0"
                        title="Verificar agora"
                        onClick={() => handleCheckOne(e.id)}
                        disabled={checkingId === e.id}
                      >
                        {checkingId === e.id ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          <RefreshCcw className="h-3.5 w-3.5" />
                        )}
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 w-7 p-0"
                        title="Editar"
                        onClick={() => openEditForm(e)}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 w-7 p-0 text-destructive hover:text-destructive"
                        title="Excluir"
                        onClick={() => setDeleteTarget(e)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Form de cadastro/edição */}
      <Dialog open={formOpen} onOpenChange={setFormOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{editingId != null ? "Editar Endpoint" : "Adicionar Endpoint"}</DialogTitle>
            <DialogDescription>
              Cadastre um host:porta externo (servidor on-prem, serviço fora do cluster) para
              monitorar a validade do certificado TLS.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <div className="space-y-1.5">
              <Label htmlFor="cep-name">Nome</Label>
              <Input
                id="cep-name"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Ex: AD Datacenter SP"
              />
            </div>
            <div className="grid grid-cols-3 gap-2">
              <div className="col-span-2 space-y-1.5">
                <Label htmlFor="cep-host">Host</Label>
                <Input
                  id="cep-host"
                  value={form.host}
                  onChange={(e) => setForm({ ...form, host: e.target.value })}
                  placeholder="ad.datacenter.local ou IP"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="cep-port">Porta</Label>
                <Input
                  id="cep-port"
                  type="number"
                  value={form.port}
                  onChange={(e) => setForm({ ...form, port: Number(e.target.value) || 443 })}
                />
              </div>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="cep-sni">SNI (opcional)</Label>
              <Input
                id="cep-sni"
                value={form.sni}
                onChange={(e) => setForm({ ...form, sni: e.target.value })}
                placeholder="Só necessário se o host não bate com o hostname real do certificado"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="cep-group">Grupo (opcional)</Label>
              <Input
                id="cep-group"
                value={form.group_label}
                onChange={(e) => setForm({ ...form, group_label: e.target.value })}
                placeholder="Ex: on-prem, windows"
              />
            </div>
            {editingId != null && (
              <div className="flex items-center justify-between pt-1">
                <Label htmlFor="cep-enabled">Habilitado (incluído em "Verificar agora")</Label>
                <Switch
                  id="cep-enabled"
                  checked={form.enabled}
                  onCheckedChange={(v) => setForm({ ...form, enabled: v })}
                />
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setFormOpen(false)}>
              Cancelar
            </Button>
            <Button onClick={handleSubmit} disabled={createMutation.isPending || updateMutation.isPending}>
              {(createMutation.isPending || updateMutation.isPending) && (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              )}
              Salvar
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Confirmação de exclusão */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Excluir endpoint?</AlertDialogTitle>
            <AlertDialogDescription>
              Isso remove "{deleteTarget?.name}" ({deleteTarget?.host}:{deleteTarget?.port}) e todo
              o histórico de checagens. Essa ação não pode ser desfeita.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Excluir
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <ExternalEndpointDetailModal
        endpoint={detailTarget}
        open={!!detailTarget}
        onOpenChange={(open) => !open && setDetailTarget(null)}
      />
    </div>
  );
}
