import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, RefreshCw } from "lucide-react";
import { useState } from "react";
import { apiClient } from "@/lib/api/client";
import { Button } from "./ui/button";
import { CopyValue } from "./CopyValue";

const RECHECK_MS = 5 * 60 * 1000;

const TITLES: Record<string, string> = {
  login_required: "Sessão do Azure CLI expirada",
  cli_missing: "Azure CLI não encontrado no servidor",
  error: "Falha ao obter token do Azure",
};

// Avisa quando o `az` do servidor perdeu a sessão (uso prolongado). Checa a cada 5 min e ao voltar
// para a aba; a correção é um `az login` no terminal — vale para o servidor sem reiniciar.
export function AzureSessionBanner() {
  const queryClient = useQueryClient();
  const [checking, setChecking] = useState(false);
  const { data } = useQuery({
    queryKey: ["azure-session"],
    queryFn: () => apiClient.getAzureSession(),
    refetchInterval: RECHECK_MS,
    refetchOnWindowFocus: true,
    staleTime: 60 * 1000,
    retry: false,
  });

  if (!data || data.ok) return null;

  const recheck = async () => {
    setChecking(true);
    try {
      const fresh = await apiClient.getAzureSession(true);
      queryClient.setQueryData(["azure-session"], fresh);
    } finally {
      setChecking(false);
    }
  };

  const reason = data.reason ?? "error";
  return (
    <div className="bg-amber-900/20 border-l-4 border-amber-500 p-4 mb-4 rounded-r-lg">
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-3 flex-1 min-w-0">
          <AlertTriangle className="w-6 h-6 text-amber-500 flex-shrink-0 mt-0.5" />
          <div className="flex-1 min-w-0">
            <h3 className="text-amber-500 font-semibold text-base mb-1">{TITLES[reason] ?? TITLES.error}</h3>
            <p className="text-sm text-muted-foreground mb-2">
              {reason === "login_required"
                ? "Recursos que usam o Azure (AKS, Graph, grupos do Entra ID) vão falhar até você entrar de novo. No terminal da máquina do servidor, rode:"
                : reason === "cli_missing"
                  ? "Instale o Azure CLI ou ajuste o PATH do processo do servidor."
                  : "O token não pôde ser obtido. Se persistir, rode no terminal do servidor:"}
            </p>
            {reason !== "cli_missing" && (
              <code className="text-sm bg-black/30 rounded px-2 py-1 inline-block">
                <CopyValue value="az login" label="comando">az login</CopyValue>
              </code>
            )}
            {data.detail && (
              <p className="mt-2 text-xs text-muted-foreground break-words select-text">
                {data.resource === "graph" ? "Graph" : "ARM"}: {data.detail}
              </p>
            )}
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={recheck} disabled={checking} className="flex-shrink-0 border-amber-500/30 hover:bg-amber-500/10">
          <RefreshCw className={`w-4 h-4 mr-2 ${checking ? "animate-spin" : ""}`} />
          {checking ? "Verificando..." : "Verificar novamente"}
        </Button>
      </div>
    </div>
  );
}
