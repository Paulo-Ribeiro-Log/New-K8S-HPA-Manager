import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Archive, FolderOpen, Loader2, Save, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { apiClient } from "@/lib/api/client";
import type { ReportResult } from "@/lib/metricsReport";
import { MAX_SAVED_NAME, MAX_SAVED_REPORTS, defaultReportName, describeSaved, nameProblem, savedAt, type SavedReport } from "@/lib/metricsSavedReports";

const LIST_KEY = ["metrics-saved-reports"];

/**
 * Relatórios salvos de propósito. O relatório coletado some do servidor depois de 1 hora (ou ao reiniciá-lo);
 * "Salvar" o guarda, por usuário, para reabrir, exportar e comparar depois. Nada é salvo sozinho.
 */
export function MetricsSavedReports({
  jobId, current, opened, onOpen, disabled,
}: {
  /** Relatório recém-coletado e concluído (o que o botão Salvar guarda); nulo quando não há. */
  jobId: string | null;
  current: ReportResult | null;
  /** Já é um relatório salvo aberto: não há o que salvar. */
  opened: boolean;
  onOpen: (r: SavedReport) => void;
  disabled?: boolean;
}) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [confirmDelete, setConfirmDelete] = useState<number | null>(null);

  const list = useQuery({ queryKey: LIST_KEY, queryFn: () => apiClient.listSavedReports(), staleTime: 30_000, retry: false });
  const reports = list.data?.reports ?? [];
  const canSave = !opened && !!jobId && !!current;
  const full = reports.length >= MAX_SAVED_REPORTS;

  // ao abrir, o nome parte da janela do relatório atual
  useEffect(() => {
    if (open && current) setName(defaultReportName(current));
    if (!open) setConfirmDelete(null);
  }, [open, current]);

  const save = useMutation({
    mutationFn: () => apiClient.saveMetricsReport(jobId!, name.trim()),
    onSuccess: (m) => {
      toast.success(`Relatório "${m.name}" salvo`);
      qc.invalidateQueries({ queryKey: LIST_KEY });
    },
    onError: (e: Error) => toast.error(e.message || "Não consegui salvar o relatório"),
  });
  const openReport = useMutation({
    mutationFn: (id: number) => apiClient.getSavedReport(id),
    onSuccess: (r) => {
      onOpen(r);
      setOpen(false);
    },
    onError: (e: Error) => toast.error(e.message || "Não consegui abrir o relatório"),
  });
  const del = useMutation({
    mutationFn: (id: number) => apiClient.deleteSavedReport(id),
    onSuccess: () => {
      setConfirmDelete(null);
      qc.invalidateQueries({ queryKey: LIST_KEY });
    },
    onError: (e: Error) => toast.error(e.message || "Não consegui apagar o relatório"),
  });

  const problem = nameProblem(name);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button size="sm" variant="outline" className="h-8 text-xs gap-1" disabled={disabled} title="Salvar o relatório coletado e reabrir os que você salvou (o relatório coletado some do servidor depois de 1 hora)">
          <Archive className="h-3.5 w-3.5" />Relatórios salvos{reports.length > 0 ? ` (${reports.length})` : ""}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[32rem] space-y-3 p-3 text-xs">
        {canSave && (
          <div className="space-y-1.5">
            <div className="font-medium">Salvar o relatório atual</div>
            <div className="flex items-center gap-1.5">
              <Input className="h-7 text-xs" maxLength={MAX_SAVED_NAME} value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => e.key === "Enter" && !problem && !full && save.mutate()} aria-label="Nome do relatório" />
              <Button size="sm" className="h-7 text-xs gap-1 flex-shrink-0" disabled={!!problem || full || save.isPending} onClick={() => save.mutate()}>
                {save.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Save className="h-3.5 w-3.5" />}Salvar
              </Button>
            </div>
            {full && <div className="text-amber-400">Você já tem {MAX_SAVED_REPORTS} relatórios salvos: apague algum para salvar outro.</div>}
            {!full && problem && name !== "" && <div className="text-amber-400">{problem}</div>}
            <div className="text-[10px] text-muted-foreground">Guarda o resultado completo (todas as aplicações, com o que foi marcado em Opções de coleta). Nada é salvo sozinho.</div>
          </div>
        )}
        {opened && <div className="rounded border border-border bg-muted/30 p-2 text-muted-foreground">Você está vendo um relatório salvo. Colete de novo para ter um novo relatório e salvá-lo.</div>}
        <div className={`space-y-1.5 ${canSave || opened ? "border-t border-border pt-2" : ""}`}>
          <div className="font-medium">Salvos{reports.length > 0 ? ` (${reports.length} de ${MAX_SAVED_REPORTS})` : ""}</div>
          {list.isLoading && <div className="text-muted-foreground">Carregando…</div>}
          {list.isError && <div className="text-red-400">{(list.error as Error).message}</div>}
          {list.data && reports.length === 0 && <div className="text-muted-foreground">Nenhum relatório salvo. Colete um relatório e clique em Salvar.</div>}
          <ul className="max-h-72 space-y-1 overflow-y-auto">
            {reports.map((r) => (
              <li key={r.id} className="rounded border border-border/60 p-2 hover:bg-muted/40">
                <div className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate font-medium" title={r.name}>{r.name}</span>
                  <Button size="sm" variant="outline" className="h-6 text-[11px] gap-1" disabled={openReport.isPending} onClick={() => openReport.mutate(r.id)} title="Abrir este relatório">
                    {openReport.isPending && openReport.variables === r.id ? <Loader2 className="h-3 w-3 animate-spin" /> : <FolderOpen className="h-3 w-3" />}Abrir
                  </Button>
                  {confirmDelete === r.id ? (
                    <span className="flex items-center gap-1">
                      <Button size="sm" variant="destructive" className="h-6 text-[11px]" disabled={del.isPending} onClick={() => del.mutate(r.id)}>Apagar</Button>
                      <Button size="sm" variant="ghost" className="h-6 text-[11px]" onClick={() => setConfirmDelete(null)}>Não</Button>
                    </span>
                  ) : (
                    <button type="button" className="text-muted-foreground hover:text-red-400" title="Apagar este relatório salvo" onClick={() => setConfirmDelete(r.id)}>
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
                <div className="mt-0.5 text-[10px] text-muted-foreground">{describeSaved(r)} · {savedAt(r)}</div>
              </li>
            ))}
          </ul>
        </div>
      </PopoverContent>
    </Popover>
  );
}
