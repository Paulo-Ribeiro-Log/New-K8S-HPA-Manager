import { useCallback, useEffect, useState } from "react";
import { Copy, KeyRound, Loader2, RefreshCw, X } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { Button } from "@/components/ui/button";

// Aviso de login do Azure CLI expirado no servidor. Aparece quando:
//  - uma chamada falha com AZ_LOGIN_REQUIRED / "az login" (evento "azure-login-required", client.ts);
//  - a checagem periódica (GET /azure/login-status) volta "login_required".
// O servidor não consegue refazer o login sozinho: o usuário roda `az login` no terminal do
// servidor e clica em "Verificar de novo".

const POLL_MS = 5 * 60 * 1000;
const COMMAND = "az login";

export function AzureLoginBanner() {
  const [required, setRequired] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const [detail, setDetail] = useState("");
  const [checking, setChecking] = useState(false);

  const check = useCallback(async (force: boolean) => {
    try {
      const st = await apiClient.getAzureLoginStatus(force);
      if (st.state === "login_required") {
        setRequired(true);
        setDetail(st.detail ?? "");
      } else if (st.state === "valid") {
        setRequired(false);
        setDismissed(false);
      }
      return st.state;
    } catch {
      return null; // sem sessão/servidor fora: não mexe no aviso
    }
  }, []);

  useEffect(() => {
    check(false);
    const timer = window.setInterval(() => check(false), POLL_MS);
    const onRequired = (e: Event) => {
      setRequired(true);
      setDismissed(false);
      setDetail((e as CustomEvent<{ message?: string }>).detail?.message ?? "");
    };
    window.addEventListener("azure-login-required", onRequired);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("azure-login-required", onRequired);
    };
  }, [check]);

  if (!required || dismissed) return null;

  const recheck = async () => {
    setChecking(true);
    const state = await check(true);
    setChecking(false);
    if (state === "valid") toast.success("Login do Azure válido de novo.");
    else if (state === "login_required") toast.error("O login do Azure continua expirado.");
    else toast.warning("Não foi possível confirmar o login do Azure agora. Tente de novo em instantes.");
  };

  return (
    <div className="flex-shrink-0 flex flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2 text-sm border-b border-amber-500/40 bg-amber-500/10 text-amber-800 dark:text-amber-200">
      <KeyRound className="w-4 h-4 flex-shrink-0" />
      <span className="font-medium">Login do Azure expirado.</span>
      <span>
        Rode <code className="px-1 rounded bg-amber-500/20 font-mono">{COMMAND}</code> no terminal onde o servidor está
        rodando e clique em "Verificar de novo". Node Pools, FinOps e a descoberta de clusters AKS dependem dele.
      </span>
      {detail && (
        <span className="text-xs opacity-70 truncate max-w-[32rem]" title={detail}>
          {detail.split("\n").pop()}
        </span>
      )}
      <div className="ml-auto flex items-center gap-1.5">
        <Button
          variant="outline"
          size="sm"
          className="h-7 gap-1.5 border-amber-500/40 bg-transparent"
          onClick={() =>
            navigator.clipboard
              .writeText(COMMAND)
              .then(() => toast.success("Comando copiado"))
              .catch(() => toast.error("Não foi possível copiar"))
          }
        >
          <Copy className="w-3.5 h-3.5" />
          Copiar comando
        </Button>
        <Button variant="outline" size="sm" className="h-7 gap-1.5 border-amber-500/40 bg-transparent" onClick={recheck} disabled={checking}>
          {checking ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
          Verificar de novo
        </Button>
        <Button variant="ghost" size="icon" className="h-7 w-7" title="Ocultar até o próximo erro" onClick={() => setDismissed(true)}>
          <X className="w-4 h-4" />
        </Button>
      </div>
    </div>
  );
}
