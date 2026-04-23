import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Loader2, ShieldCheck, ShieldAlert } from "lucide-react";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";

interface SreApprovalButtonProps {
  approvalUrl: string;
  chgNumber?: string;
}

type ApprovalStatus = "unknown" | "loading" | "pending" | "approved" | "finalized" | "approving" | "error";

export function SreApprovalButton({ approvalUrl, chgNumber }: SreApprovalButtonProps) {
  const [status, setStatus] = useState<ApprovalStatus>("unknown");
  const [approverEmail, setApproverEmail] = useState<string>("");

  useEffect(() => {
    if (approvalUrl) checkStatus();
  }, [approvalUrl]);

  const checkStatus = async (silent = false) => {
    if (!silent) setStatus("loading");
    try {
      const res = await apiClient.getSreApprovalInfo(approvalUrl);
      if (res.success && res.approval_info) {
        const info = res.approval_info;
        if (info.is_finalized || info.status === "FINALIZED") {
          setStatus("finalized");
          setApproverEmail(info.approver_email || "");
        } else if (info.status === "APPROVED") {
          setStatus("approved");
          setApproverEmail(info.approver_email || "");
        } else if (!silent) {
          setStatus("pending");
        }
      } else if (!silent) {
        setStatus("pending");
      }
    } catch {
      if (!silent) setStatus("pending");
    }
  };

  const handleApprove = async () => {
    setStatus("approving");
    try {
      let userEmail = "";
      try {
        const userRes = await apiClient.getSreCurrentUser();
        if (userRes.success) userEmail = userRes.email;
      } catch {
        // backend usará Azure CLI
      }

      const res = await apiClient.sreApprove(approvalUrl, userEmail);
      if (res.success) {
        if (res.already_finalized) {
          setStatus("finalized");
          setApproverEmail(res.approver_email || "");
          toast.info(`${chgNumber || "CHG"} já havia sido finalizada`);
        } else {
          setStatus("approved");
          setApproverEmail(userEmail);
          toast.success(`${chgNumber || "CHG"} aprovada com sucesso!`);
        }
      } else {
        toast.error(res.error || "Falha ao aprovar");
        setStatus("pending");
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Erro desconhecido";
      toast.error(msg);
      setStatus("pending");
    }
  };

  const isApprovedOrFinalized = status === "approved" || status === "finalized";
  const canApprove = status === "pending" || status === "unknown";
  const isLoading = status === "loading" || status === "approving";

  return (
    <div className="flex items-center gap-2">
      <Button
        size="sm"
        onClick={canApprove ? handleApprove : undefined}
        disabled={isLoading || isApprovedOrFinalized}
        className="h-6 px-2.5 text-xs bg-blue-600 hover:bg-blue-700 text-white disabled:bg-blue-400"
        title={`Aprovar ${chgNumber || ""} — preenche email automaticamente`}
      >
        {isLoading ? (
          <Loader2 className="h-3 w-3 animate-spin" />
        ) : isApprovedOrFinalized ? (
          <ShieldCheck className="h-3 w-3" />
        ) : (
          <ShieldAlert className="h-3 w-3 mr-1" />
        )}
        {!isLoading && "Aprovar"}
      </Button>

      {status === "loading" && (
        <span className="text-xs text-muted-foreground">Verificando...</span>
      )}
      {status === "approving" && (
        <span className="text-xs text-muted-foreground">Enviando...</span>
      )}
      {isApprovedOrFinalized && (
        <span className="text-xs text-white flex items-center gap-1 max-w-[600px] truncate" title={approverEmail || undefined}>
          {approverEmail ? (
            <>
              <span className="shrink-0">Já aprovado por</span>
              <span className="text-yellow-400 truncate">{approverEmail}</span>
            </>
          ) : (
            status === "finalized" ? "Já finalizado" : "Já aprovado"
          )}
        </span>
      )}
    </div>
  );
}
