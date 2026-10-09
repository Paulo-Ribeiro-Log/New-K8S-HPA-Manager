// Aba "Deep Analysis" do FinOps (FINOPS-DEEP-ANALYSIS-PLAN.md): lista os pools do último relatório e
// abre o modal de análise profunda de cada um.
import { useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Microscope } from "lucide-react";
import { fmtBRL } from "@/lib/finopsFormat";
import type { FinOpsPool } from "./types";
import { DeepAnalysisModal } from "./DeepAnalysisModal";

export function DeepAnalysisTab({ cluster, pools }: { cluster: string; pools: FinOpsPool[] }) {
  const [open, setOpen] = useState<string | null>(null);
  const sorted = [...pools].sort((a, b) => (b.total_cost_brl || b.monthly_cost_brl) - (a.total_cost_brl || a.monthly_cost_brl));

  return (
    <div className="space-y-3">
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm flex items-center gap-2">
            <Microscope className="h-4 w-4 text-indigo-500" /> Deep Analysis por node pool
          </CardTitle>
        </CardHeader>
        <CardContent className="text-xs text-muted-foreground space-y-1">
          <p>
            Diagnóstico completo de um pool: qual recurso trava o agendamento e qual é o gargalo real, custo fixo de DaemonSets,
            workloads sub-requisitados, interação dos requests com os HPAs, throttling de CPU e quantos nodes o pool precisa
            em cada SKU depois do ajuste de requests.
          </p>
          <p>
            Coleta ao vivo só os nodes e pods do pool e usa o histórico (P95/picos) do último relatório deste cluster — rode
            <strong> Analisar</strong> antes para ter o histórico atualizado.
          </p>
        </CardContent>
      </Card>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Pool</TableHead>
            <TableHead>VM</TableHead>
            <TableHead>Nodes</TableHead>
            <TableHead>Modo</TableHead>
            <TableHead>Custo/mês</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {sorted.map(p => (
            <TableRow key={p.name}>
              <TableCell className="font-medium">{p.name}</TableCell>
              <TableCell className="font-mono text-xs">
                {p.vm_size}
                <span className="text-muted-foreground"> · {p.vm_cpu_cores} vCPU / {p.vm_memory_gb} GB</span>
              </TableCell>
              <TableCell>{p.node_count}</TableCell>
              <TableCell><Badge variant="outline" className="text-[10px]">{p.mode || "—"}</Badge></TableCell>
              <TableCell>{fmtBRL(p.total_cost_brl || p.monthly_cost_brl)}</TableCell>
              <TableCell className="text-right">
                <Button size="sm" variant="outline" className="h-7" onClick={() => setOpen(p.name)}>
                  <Microscope className="h-3.5 w-3.5 mr-1" /> Analisar pool
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>

      {open && <DeepAnalysisModal cluster={cluster} pool={open} onClose={() => setOpen(null)} />}
    </div>
  );
}
