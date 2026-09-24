import { useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Download, Info, Loader2, RefreshCw, ScanSearch } from "lucide-react";
import { apiClient } from "@/lib/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Alert, AlertDescription } from "@/components/ui/alert";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { cn } from "@/lib/utils";

type CoverageData = Awaited<ReturnType<typeof apiClient.getDynatraceCoverage>>;
type StatusFilter = "all" | "Ativo" | "Nao resolvido";
type CoverageRow = NonNullable<CoverageData["rows"]>[number];
const NO_ROWS: CoverageRow[] = [];

const csvCell = (v: string | number) => {
  const s = String(v);
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
};

// Cobertura de Deep Monitoring do Dynatrace por namespace — equivalente (via API clássica v2) à
// DQL do dashboard de cobertura: processos × tecnologia × versão do OneAgent, com contagem de
// hosts e pods. Usa o cluster selecionado globalmente.
export const DynatraceCoverageTab = ({ selectedCluster }: { selectedCluster?: string }) => {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [refreshing, setRefreshing] = useState(false);

  const queryKey = ["dt-coverage", selectedCluster];
  const { data, isLoading, error } = useQuery<CoverageData>({
    queryKey,
    queryFn: () => apiClient.getDynatraceCoverage(selectedCluster!),
    enabled: !!selectedCluster,
    staleTime: 5 * 60 * 1000,
    retry: false,
  });

  const rows = data?.rows ?? NO_ROWS;

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return rows.filter(
      (r) =>
        (statusFilter === "all" || r.deep_monitoring_status === statusFilter) &&
        (!q ||
          r.namespace.toLowerCase().includes(q) ||
          r.service_name.toLowerCase().includes(q) ||
          r.technology.toLowerCase().includes(q)),
    );
  }, [rows, search, statusFilter]);

  const summary = useMemo(() => {
    const pods = rows.reduce((acc, r) => acc + r.pod_count, 0);
    const activePods = rows
      .filter((r) => r.deep_monitoring_status === "Ativo")
      .reduce((acc, r) => acc + r.pod_count, 0);
    return {
      namespaces: new Set(rows.map((r) => r.namespace)).size,
      pods,
      activePct: pods > 0 ? Math.round((activePods / pods) * 100) : 0,
      unresolved: rows.filter((r) => r.deep_monitoring_status !== "Ativo").length,
      versions: new Set(rows.map((r) => r.oneagent_version).filter(Boolean)).size,
    };
  }, [rows]);

  const handleRefresh = async () => {
    if (!selectedCluster) return;
    setRefreshing(true);
    try {
      const fresh = await apiClient.getDynatraceCoverage(selectedCluster, true);
      queryClient.setQueryData(queryKey, fresh);
    } catch (err) {
      toast.error(`Falha ao atualizar: ${err instanceof Error ? err.message : String(err)}`);
    } finally {
      setRefreshing(false);
    }
  };

  const handleExport = () => {
    const header = ["Namespace", "Service Name", "Technology", "OneAgent Version", "Deep Monitoring Status", "Host Count", "Pod Count"];
    const lines = filtered.map((r) =>
      [r.namespace, r.service_name, r.technology, r.oneagent_version, r.deep_monitoring_status, r.host_count, r.pod_count]
        .map(csvCell)
        .join(","),
    );
    const blob = new Blob([[header.join(","), ...lines].join("\n")], { type: "text/csv;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `dt-coverage-${(data?.cluster ?? selectedCluster ?? "cluster").replace(/[/:]/g, "_")}-${new Date().toISOString().slice(0, 10)}.csv`;
    a.click();
    URL.revokeObjectURL(url);
    toast.success("CSV exportado com sucesso");
  };

  return (
    <div className="flex flex-col h-full p-4 gap-4 overflow-auto">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div>
          <h2 className="text-lg font-semibold flex items-center gap-2">
            <ScanSearch className="h-5 w-5 text-violet-500" />
            Cobertura Dynatrace
          </h2>
          <p className="text-xs text-muted-foreground">
            Deep monitoring por namespace no cluster <span className="font-mono">{data?.cluster ?? selectedCluster ?? "—"}</span>
            {data?.generated_at && <> · coletado às {new Date(data.generated_at).toLocaleTimeString()}</>}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleRefresh} disabled={!selectedCluster || refreshing || isLoading}>
            <RefreshCw className={cn("h-4 w-4 mr-1", refreshing && "animate-spin")} />
            Atualizar
          </Button>
          <Button variant="outline" size="sm" onClick={handleExport} disabled={filtered.length === 0}>
            <Download className="h-4 w-4 mr-1" />
            CSV
          </Button>
        </div>
      </div>

      {!selectedCluster && (
        <Alert>
          <Info className="h-4 w-4" />
          <AlertDescription>Selecione um cluster para ver a cobertura.</AlertDescription>
        </Alert>
      )}

      {isLoading && (
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          Consultando o Dynatrace (clusters grandes podem levar até 1–2 min)…
        </div>
      )}

      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error instanceof Error ? error.message : String(error)}</AlertDescription>
        </Alert>
      )}

      {data?.dt_not_configured && (
        <Alert>
          <Info className="h-4 w-4" />
          <AlertDescription>Dynatrace não configurado: {data.message}</AlertDescription>
        </Alert>
      )}

      {data && !data.dt_not_configured && data.host_group_found === false && (
        <Alert>
          <Info className="h-4 w-4" />
          <AlertDescription>
            Nenhum host group com o nome deste cluster foi encontrado no Dynatrace — o cluster não está monitorado por OneAgent (ou o
            host group tem outro nome).
          </AlertDescription>
        </Alert>
      )}

      {data?.host_group_found && (
        <>
          <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
            {[
              { label: "Namespaces", value: summary.namespaces },
              { label: "Pods (processos)", value: summary.pods },
              { label: "Pods com deep monitoring", value: `${summary.activePct}%` },
              { label: "Linhas não resolvidas", value: summary.unresolved },
              { label: "Versões de OneAgent", value: summary.versions },
            ].map((c) => (
              <Card key={c.label}>
                <CardContent className="p-3">
                  <div className="text-xs text-muted-foreground">{c.label}</div>
                  <div className="text-xl font-semibold">{c.value}</div>
                </CardContent>
              </Card>
            ))}
          </div>

          {(data.processes_without_service ?? 0) > 0 && (
            <p className="text-xs text-muted-foreground">
              {data.processes_without_service} processo(s) sem serviço associado não aparecem na tabela (mesmo critério da DQL do dashboard).
            </p>
          )}

          <div className="flex items-center gap-2 flex-wrap">
            <Input
              placeholder="Filtrar por namespace, serviço ou tecnologia…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="max-w-sm h-8"
            />
            {(["all", "Ativo", "Nao resolvido"] as StatusFilter[]).map((s) => (
              <Button
                key={s}
                size="sm"
                variant={statusFilter === s ? "default" : "outline"}
                className="h-8"
                onClick={() => setStatusFilter(s)}
              >
                {s === "all" ? "Todos" : s === "Ativo" ? "Ativo" : "Não resolvido"}
              </Button>
            ))}
            <span className="text-xs text-muted-foreground ml-auto">
              {filtered.length} de {rows.length} linhas
            </span>
          </div>

          <div className="border rounded-md">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Namespace</TableHead>
                  <TableHead>Service Name</TableHead>
                  <TableHead>Technology</TableHead>
                  <TableHead>OneAgent Version</TableHead>
                  <TableHead>Deep Monitoring</TableHead>
                  <TableHead className="text-right">Hosts</TableHead>
                  <TableHead className="text-right">Pods</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filtered.map((r) => (
                  <TableRow key={`${r.namespace}|${r.service_name}|${r.technology}|${r.oneagent_version}|${r.deep_monitoring_status}`}>
                    <TableCell className="font-mono text-xs">{r.namespace}</TableCell>
                    <TableCell className="text-xs">{r.service_name}</TableCell>
                    <TableCell className="text-xs">{r.technology || "—"}</TableCell>
                    <TableCell className="font-mono text-xs">{r.oneagent_version || "—"}</TableCell>
                    <TableCell>
                      <Badge
                        variant="outline"
                        className={cn(
                          "text-[10px]",
                          r.deep_monitoring_status === "Ativo"
                            ? "border-green-500/40 text-green-600 dark:text-green-400"
                            : "border-amber-500/40 text-amber-600 dark:text-amber-400",
                        )}
                      >
                        {r.deep_monitoring_status === "Ativo" ? "Ativo" : "Não resolvido"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right text-xs">{r.host_count}</TableCell>
                    <TableCell className="text-right text-xs">{r.pod_count}</TableCell>
                  </TableRow>
                ))}
                {filtered.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={7} className="text-center text-sm text-muted-foreground py-6">
                      Nenhuma linha {rows.length > 0 ? "para o filtro atual" : "retornada pelo Dynatrace"}.
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
        </>
      )}
    </div>
  );
};
