import { useState, useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Loader2, ShieldCheck, ShieldAlert, ShieldX } from "lucide-react";
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
  const [approverSquad, setApproverSquad] = useState<string>("");
  const [justApproved, setJustApproved] = useState(false);

  useEffect(() => {
    if (approvalUrl) {
      checkStatus();
    }
  }, [approvalUrl]);

  const checkStatus = async () => {
    setStatus("loading");
    try {
      const res = await apiClient.getSreApprovalInfo(approvalUrl);
      if (res.success && res.approval_info) {
        const info = res.approval_info;
        if (info.is_finalized || info.status === "FINALIZED") {
          setStatus("finalized");
          setApproverEmail(info.approver_email || "");
          setApproverSquad(info.approver_squad || "");
        } else if (info.status === "APPROVED") {
          setStatus("approved");
          setApproverEmail(info.approver_email || "");
          setApproverSquad(info.approver_squad || "");
        } else {
          setStatus("pending");
        }
      } else {
        setStatus("pending");
      }
    } catch {
      setStatus("pending");
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
        setJustApproved(true);
        setStatus("approved");
        setApproverEmail(userEmail);
        toast.success(`${chgNumber || "CHG"} aprovada com sucesso!`);
        setTimeout(checkStatus, 2000);
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

  if (status === "loading") {
    return (
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground py-1">
        <Loader2 className="h-3 w-3 animate-spin" />
        Verificando aprovação...
      </div>
    );
  }

  if (status === "approving") {
    return (
      <Button disabled size="sm" className="w-full h-7 text-xs">
        <Loader2 className="h-3 w-3 mr-1.5 animate-spin" />
        Aprovando...
      </Button>
    );
  }

  if (status === "approved" || status === "finalized") {
    const label = status === "finalized"
      ? "Mudança finalizada"
      : justApproved
        ? "Aprovado com sucesso"
        : "Mudança já aprovada";

    return (
      <div className="space-y-0.5">
        <Badge
          variant="outline"
          className="w-full justify-center gap-1.5 py-1 text-xs bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-300 border-green-200 dark:border-green-800"
        >
          <ShieldCheck className="h-3 w-3 flex-shrink-0" />
          {label}
        </Badge>
        {approverEmail && (
          <p className="text-[10px] text-center text-muted-foreground truncate" title={approverEmail}>
            por {approverEmail}{approverSquad ? ` · ${approverSquad}` : ""}
          </p>
        )}
      </div>
    );
  }

  // pending / unknown
  return (
    <Button
      size="sm"
      onClick={handleApprove}
      className="w-full h-7 text-xs bg-orange-500 hover:bg-orange-600 text-white"
      title={`Aprovar ${chgNumber || "SRE"} — preenche email automaticamente`}
    >
      <ShieldAlert className="h-3 w-3 mr-1 flex-shrink-0" />
      Aprovar SRE
    </Button>
  );
}
