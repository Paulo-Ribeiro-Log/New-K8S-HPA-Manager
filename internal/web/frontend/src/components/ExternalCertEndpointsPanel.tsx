import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Loader2, Plus, RefreshCcw, Trash2, Pencil, Globe, ListPlus, Download, FileSpreadsheet, FileText } from "lucide-react";
import { toast } from "sonner";
import { getStatusBadge } from "@/components/CertificateDetailModal";
import { ExternalEndpointDetailModal } from "@/components/ExternalEndpointDetailModal";
import { apiClient } from "@/lib/api/client";
import {
  useCertEndpoints,
  useCreateCertEndpoint,
  useUpdateCertEndpoint,
  useDeleteCertEndpoint,
  useCheckCertEndpoint,
  useCheckAllCertEndpoints,
} from "@/hooks/useCertEndpoints";
import type { CertEndpointWithStatus } from "@/lib/api/types";

// Parseia uma linha do textarea de import em lote: "host[:porta]" → { name, host, port }. Nome é
// derivado do host (dá pra renomear depois, via editar) — formato deliberadamente simples pra
// colar direto de uma lista de inventário/planilha existente, sem exigir CSV bem formado.
// Não cobre IPv6 (ex: "::1") de propósito — caso raro no uso real (servidor on-prem por
// hostname/IPv4), heurística simples (split no último ":") já cobre o formato pedido.
function parseBulkLine(raw: string): { name: string; host: string; port: number } | null {
  const line = raw.trim();
  if (!line) return null;

  const lastColon = line.lastIndexOf(":");
  if (lastColon > 0 && /^\d+$/.test(line.slice(lastColon + 1))) {
    const host = line.slice(0, lastColon);
    const port = Number(line.slice(lastColon + 1));
    return { name: host, host, port: port || 443 };
  }
  return { name: line, host: line, port: 443 };
}

type EndpointForm = {
  name: string;
  host: string;
  port: number;
  sni: string;
  group_label: string;
  enabled: boolean;
};

const emptyForm: EndpointForm = { name: "", host: "", port: 443, sni: "", group_label: "", enabled: true };

// endpointStatusLabel resolve o mesmo rótulo textual exibido nos badges da tabela (Status) — usado
// pelos exports CSV/TXT pra não duplicar a árvore de decisão em cada formato.
function endpointStatusLabel(e: CertEndpointWithStatus): string {
  const check = e.latest_check;
  if (!check) return "Nunca verificado";
  if (!check.success) return `Erro: ${check.error_message || "erro desconhecido"}`;
  if (check.is_default_fake_cert) return "Certificado Fake (Ingress)";
  switch (check.status) {
    case "valid":
      return "Válido";
    case "expiring":
      return "Expirando";
    case "expired":
      return "Expirado";
    default:
      return check.status || "—";
  }
}

// csvDelimiter=";" (não ",") porque o Excel em locale pt-BR (o idioma de toda a aplicação — datas,
// labels, mensagens) usa vírgula como separador DECIMAL e por isso espera ";" como separador de
// campo no CSV por padrão — abrir um CSV separado por vírgula nesse Excel resulta em tudo numa
// única coluna só, sem nenhum erro visível pro usuário (comportamento silencioso, não um crash).
const csvDelimiter = ";";

// csvField escapa um valor pra CSV (RFC 4180): entre aspas quando contém o delimitador, aspas ou
// quebra de linha; aspas internas duplicadas. Necessário porque Subject/SAN/mensagens de erro
// podem conter ";"/"," — join(csvDelimiter) simples (como em HPAExportButton.tsx, que usa ",")
// quebraria o CSV nesses casos.
function csvField(value: string | number | undefined | null): string {
  const s = value === undefined || value === null ? "" : String(value);
  if (s.includes(csvDelimiter) || /["\n]/.test(s)) {
    return `"${s.replace(/"/g, '""')}"`;
  }
  return s;
}

const exportColumns = [
  "Nome",
  "Host",
  "Porta",
  "SNI",
  "Grupo",
  "Habilitado",
  "Nome do Certificado",
  "Status",
  "Válido até",
  "Dias restantes",
  "Emissor",
  "Última verificação",
] as const;

function exportRow(e: CertEndpointWithStatus): (string | number)[] {
  const check = e.latest_check;
  return [
    e.name,
    e.host,
    e.port,
    e.sni ?? "",
    e.group_label ?? "",
    e.enabled ? "Sim" : "Não",
    check?.success ? check.subject ?? "" : "",
    endpointStatusLabel(e),
    check?.success && check.not_after ? new Date(check.not_after).toLocaleDateString("pt-BR") : "",
    check?.success ? check.days_remaining ?? "" : "",
    check?.success ? check.issuer ?? "" : "",
    check?.checked_at ? new Date(check.checked_at).toLocaleString("pt-BR") : "",
  ];
}

function downloadBlob(content: string, mimeType: string, filename: string) {
  const blob = new Blob([content], { type: `${mimeType};charset=utf-8;` });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  URL.revokeObjectURL(url);
}

// Sub-aba "Endpoints Externos" — monitor de certificados TLS de servidores fora de qualquer
// cluster K8s (on-prem Windows/Linux, serviços externos), estilo blackbox_exporter do
// Prometheus mas sem depender dele: handshake TLS real sob demanda. Ver
// EXTERNAL-CERT-MONITOR-PLAN.md.
export function ExternalCertEndpointsPanel() {
  const queryClient = useQueryClient();
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

  // Import em lote — "host[:porta]" um por linha, ver parseBulkLine acima.
  const [bulkOpen, setBulkOpen] = useState(false);
  const [bulkText, setBulkText] = useState("");
  const [bulkImporting, setBulkImporting] = useState(false);
  const [bulkProgress, setBulkProgress] = useState<{ current: number; total: number } | null>(null);

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

  // Importa várias linhas de uma vez — chama apiClient direto (não a mutation) pra não invalidar
  // a query N vezes seguidas; um único invalidateQueries no final já cobre a lista inteira.
  const handleBulkImport = async () => {
    const parsed = bulkText
      .split("\n")
      .map(parseBulkLine)
      .filter((p): p is NonNullable<typeof p> => p !== null);

    if (parsed.length === 0) {
      toast.error("Cole ao menos um host por linha");
      return;
    }

    setBulkImporting(true);
    setBulkProgress({ current: 0, total: parsed.length });
    let successCount = 0;
    const errors: string[] = [];

    for (let i = 0; i < parsed.length; i++) {
      const item = parsed[i];
      try {
        await apiClient.createCertEndpoint(item);
        successCount++;
      } catch (err) {
        errors.push(`${item.host}: ${err instanceof Error ? err.message : "erro"}`);
      }
      setBulkProgress({ current: i + 1, total: parsed.length });
    }

    queryClient.invalidateQueries({ queryKey: ["cert-endpoints"] });
    setBulkImporting(false);
    setBulkProgress(null);

    if (successCount > 0) {
      toast.success(`${successCount} endpoint${successCount > 1 ? "s" : ""} importado${successCount > 1 ? "s" : ""}`);
    }
    if (errors.length > 0) {
      toast.error(`${errors.length} falharam: ${errors.slice(0, 3).join("; ")}${errors.length > 3 ? "…" : ""}`);
    }
    if (errors.length === 0) {
      setBulkText("");
      setBulkOpen(false);
    }
  };

  // Export CSV/TXT — client-side puro (sem rota de backend), mesmo padrão de
  // HPAExportButton.tsx: gera o conteúdo em memória e dispara download via <a download>. Exporta
  // sempre a lista completa carregada (não há filtro/busca nesta tabela hoje).
  const exportCSV = () => {
    if (endpoints.length === 0) {
      toast.error("Nenhum endpoint para exportar");
      return;
    }
    const lines = [exportColumns.join(csvDelimiter)];
    endpoints.forEach((e) => {
      lines.push(exportRow(e).map(csvField).join(csvDelimiter));
    });
    // BOM UTF-8 (﻿) na frente: sem ele, o Excel abre o arquivo assumindo Windows-1252 e
    // qualquer acento (ã, ç, é — presentes em Status/Grupo/Subject) vira caractere corrompido; com
    // o BOM, o Excel detecta UTF-8 sozinho. \r\n (não só \n) é o quebra-linha que o Excel espera
    // por padrão nesse tipo de arquivo — CRLF explícito evita depender do comportamento do
    // navegador/SO na hora de gravar o Blob.
    downloadBlob(
      "﻿" + lines.join("\r\n"),
      "text/csv",
      `endpoints-externos-${new Date().toISOString().split("T")[0]}.csv`
    );
    toast.success(`CSV exportado: ${endpoints.length} endpoint${endpoints.length > 1 ? "s" : ""}`);
  };

  const exportTXT = () => {
    if (endpoints.length === 0) {
      toast.error("Nenhum endpoint para exportar");
      return;
    }
    const separator = "=".repeat(60);
    let txt = "Endpoints Externos — Certificados TLS\n";
    txt += `Exportado em: ${new Date().toLocaleString("pt-BR")}\n\n`;
    endpoints.forEach((e) => {
      const check = e.latest_check;
      txt += `${separator}\n`;
      txt += `Nome: ${e.name}\n`;
      txt += `Host:Porta: ${e.host}:${e.port}${e.sni ? ` (SNI: ${e.sni})` : ""}\n`;
      if (e.group_label) txt += `Grupo: ${e.group_label}\n`;
      txt += `Habilitado: ${e.enabled ? "Sim" : "Não"}\n`;
      if (check?.success) {
        txt += `Nome do Certificado: ${check.subject || "—"}\n`;
        txt += `Status: ${endpointStatusLabel(e)}\n`;
        if (check.not_after) txt += `Válido até: ${new Date(check.not_after).toLocaleDateString("pt-BR")} (${check.days_remaining} dias restantes)\n`;
        txt += `Emissor: ${check.issuer || "—"}\n`;
        if (check.dns_names && check.dns_names.length > 0) txt += `SAN: ${check.dns_names.join(", ")}\n`;
      } else {
        txt += `Status: ${endpointStatusLabel(e)}\n`;
      }
      txt += `Última verificação: ${check?.checked_at ? new Date(check.checked_at).toLocaleString("pt-BR") : "nunca"}\n`;
    });
    txt += `${separator}\n`;
    downloadBlob(txt, "text/plain", `endpoints-externos-${new Date().toISOString().split("T")[0]}.txt`);
    toast.success(`TXT exportado: ${endpoints.length} endpoint${endpoints.length > 1 ? "s" : ""}`);
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
          <Button variant="outline" size="sm" onClick={() => setBulkOpen(true)}>
            <ListPlus className="h-3.5 w-3.5 mr-1.5" />
            Importar em Lote
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" size="sm" disabled={endpoints.length === 0}>
                <Download className="h-3.5 w-3.5 mr-1.5" />
                Exportar
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={exportCSV}>
                <FileSpreadsheet className="h-3.5 w-3.5 mr-2" />
                Exportar como CSV
              </DropdownMenuItem>
              <DropdownMenuItem onClick={exportTXT}>
                <FileText className="h-3.5 w-3.5 mr-2" />
                Exportar como TXT
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
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
                <th className="px-3 py-2 font-medium">Nome do Certificado</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Válido até</th>
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
                  <td
                    className="px-3 py-2 max-w-[220px] truncate"
                    title={
                      e.latest_check?.dns_names && e.latest_check.dns_names.length > 0
                        ? `SAN: ${e.latest_check.dns_names.join(", ")}`
                        : e.latest_check?.subject
                    }
                  >
                    {e.latest_check?.success ? e.latest_check.subject || "—" : "—"}
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
                    ) : e.latest_check.is_default_fake_cert ? (
                      <Badge
                        className="bg-orange-500/20 text-orange-400 border-orange-500/30"
                        title="O servidor devolveu o certificado autoassinado padrão do ingress-nginx — o host/porta não tem TLS real configurado (SNI não bate com nenhum Ingress). Não é o certificado da aplicação."
                      >
                        Cert. Fake (Ingress)
                      </Badge>
                    ) : (
                      <span className="inline-flex items-center gap-1.5">
                        {getStatusBadge(e.latest_check.status || "")}
                        <span className="text-muted-foreground">{e.latest_check.days_remaining}d</span>
                      </span>
                    )}
                  </td>
                  <td className="px-3 py-2 text-muted-foreground">
                    {e.latest_check?.success && e.latest_check.not_after
                      ? new Date(e.latest_check.not_after).toLocaleDateString("pt-BR")
                      : "—"}
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

      {/* Import em lote */}
      <Dialog open={bulkOpen} onOpenChange={(open) => !bulkImporting && setBulkOpen(open)}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Importar Endpoints em Lote</DialogTitle>
            <DialogDescription>
              Um host por linha, porta opcional (padrão 443) — ex:{" "}
              <code className="bg-muted px-1 rounded">servidor.local</code> ou{" "}
              <code className="bg-muted px-1 rounded">servidor.local:8443</code>. O nome de cada
              endpoint é derivado do host — dá pra renomear depois, editando individualmente.
            </DialogDescription>
          </DialogHeader>
          <Textarea
            value={bulkText}
            onChange={(e) => setBulkText(e.target.value)}
            placeholder={"servidor1.local\nservidor2.local:8443\n10.0.5.20\nad.datacenter.local:636"}
            rows={10}
            className="font-mono text-xs"
            disabled={bulkImporting}
          />
          {bulkProgress && (
            <p className="text-xs text-muted-foreground">
              Importando {bulkProgress.current}/{bulkProgress.total}...
            </p>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setBulkOpen(false)} disabled={bulkImporting}>
              Cancelar
            </Button>
            <Button onClick={handleBulkImport} disabled={bulkImporting || !bulkText.trim()}>
              {bulkImporting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              Importar
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
