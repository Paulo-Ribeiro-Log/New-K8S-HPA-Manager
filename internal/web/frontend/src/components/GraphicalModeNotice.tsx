import { useState } from "react";
import { AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useDisplayEnv } from "@/hooks/useDisplayEnv";
import { needsGraphicalNotice } from "@/lib/wslgTutorial";
import { WSLgTutorialModal } from "@/components/WSLgTutorialModal";

// Aviso de falta de suporte ao WSLg: só aparece no WSL, quando o robô (Teams/ServiceNow) não tem display.
export function GraphicalModeNotice({ className = "" }: { className?: string }) {
  const { report, recheck } = useDisplayEnv();
  const [open, setOpen] = useState(false);
  if (!report || !needsGraphicalNotice(report)) return null;
  const cap = report.capabilities.find((c) => (c.id === "teams_automation" || c.id === "servicenow_login") && !c.ok);
  return (
    <div className={`flex items-center gap-3 border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs ${className}`}>
      <AlertTriangle className="h-4 w-4 shrink-0 text-amber-500" />
      <span className="flex-1">
        Este WSL não tem modo gráfico (WSLg): o login automático do Teams/ServiceNow precisa dele. {cap?.detail}
      </span>
      <Button size="sm" variant="outline" onClick={() => setOpen(true)}>Ver tutorial de instalação</Button>
      <WSLgTutorialModal open={open} onOpenChange={setOpen} report={report} recheck={recheck} />
    </div>
  );
}
