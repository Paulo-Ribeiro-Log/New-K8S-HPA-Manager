import { useState } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { CheckCircle2, Circle, Loader2, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { CopyValue } from "@/components/CopyValue";
import { tutorialSteps, type DisplayReport } from "@/lib/wslgTutorial";

interface Props {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  report: DisplayReport;
  recheck: () => Promise<DisplayReport>;
}

// Passo a passo para ativar o modo gráfico (WSLg) no WSL2. Os passos já resolvidos aparecem marcados.
export function WSLgTutorialModal({ open, onOpenChange, report, recheck }: Props) {
  const [busy, setBusy] = useState(false);
  const steps = tutorialSteps(report);

  const test = async () => {
    setBusy(true);
    try {
      const r = await recheck();
      if (r.graphical_display) toast.success("Modo gráfico detectado. Reinicie o servidor para o robô usá-lo.");
      else toast.info("Ainda sem display gráfico. Siga os passos pendentes.");
    } catch (e: any) {
      toast.error(e?.message || "Falha ao testar");
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Instalar o modo gráfico (WSLg) no WSL</DialogTitle>
          <DialogDescription>{report.summary}</DialogDescription>
        </DialogHeader>
        <ol className="space-y-3">
          {steps.map((s, i) => (
            <li key={s.id} className="rounded-md border p-3">
              <div className="flex items-center gap-2 text-sm font-medium">
                {s.done ? <CheckCircle2 className="h-4 w-4 text-green-500" /> : <Circle className="h-4 w-4 text-muted-foreground" />}
                {i + 1}. {s.title}
                <span className="ml-auto text-xs text-muted-foreground">{s.where === "windows" ? "PowerShell (Windows)" : "Terminal do WSL"}</span>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">{s.text}</p>
              <div className="mt-2 space-y-1">
                {s.commands.map((c) => (
                  <div key={c} className="rounded bg-muted px-2 py-1 font-mono text-xs">
                    <CopyValue value={c} label="comando" />
                  </div>
                ))}
              </div>
            </li>
          ))}
        </ol>
        {report.notes && report.notes.length > 0 && (
          <ul className="list-disc pl-5 text-xs text-muted-foreground">
            {report.notes.map((n) => <li key={n}>{n}</li>)}
          </ul>
        )}
        <div className="flex justify-end">
          <Button onClick={test} disabled={busy} className="gap-2">
            {busy ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
            Testar de novo
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
