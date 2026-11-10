import { AlertTriangle, RefreshCw, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useBuildInfo } from "@/hooks/useBuildInfo";
import { buildLabel } from "@/lib/buildInfo";
import { Button } from "./ui/button";

// O servidor embute outro build do frontend: esta página (ou o cache do navegador) é de uma versão
// anterior. Sem aviso, testa-se código antigo achando que é o novo.
export function StaleFrontendBanner() {
  const { build, stale } = useBuildInfo();
  const [dismissedFor, setDismissedFor] = useState("");
  // Se aparecer um build ainda mais novo, o aviso volta mesmo depois de dispensado.
  const key = build?.frontend_bundle ?? "";
  useEffect(() => { if (!stale) setDismissedFor(""); }, [stale]);
  if (!stale || dismissedFor === key) return null;
  return (
    <div className="bg-amber-900/20 border-l-4 border-amber-500 p-3 mb-4 rounded-r-lg">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-start gap-3 min-w-0">
          <AlertTriangle className="w-5 h-5 text-amber-500 flex-shrink-0 mt-0.5" />
          <div className="min-w-0">
            <h3 className="text-amber-500 font-semibold text-sm">Há uma versão mais nova da interface neste servidor</h3>
            <p className="text-xs text-muted-foreground">
              O servidor está no build <span className="font-mono">{buildLabel(build)}</span>, mas esta página foi carregada de um build anterior. Recarregue
              para usar a versão atual; se persistir, faça um hard refresh (Ctrl+Shift+R).
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          <Button size="sm" variant="outline" className="border-amber-500/30 hover:bg-amber-500/10" onClick={() => window.location.reload()}>
            <RefreshCw className="w-4 h-4 mr-2" />Recarregar
          </Button>
          <Button size="sm" variant="ghost" className="h-8 w-8 p-0" title="Dispensar" onClick={() => setDismissedFor(key)}>
            <X className="w-4 h-4" />
          </Button>
        </div>
      </div>
    </div>
  );
}
