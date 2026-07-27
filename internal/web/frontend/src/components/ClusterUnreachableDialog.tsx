import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { WifiOff } from "lucide-react";

interface ClusterUnreachableDialogProps {
  open: boolean;
  clusterName: string;
  cloudProvider: string;
  onClose: () => void;
  onRetry: () => void;
  retrying?: boolean;
  // lastAttemptFailed: true logo após um "Tentar novamente" que ainda falhou — mostra um aviso
  // inline no próprio modal, não só um toast (fácil de perder já que o modal permanece aberto na
  // mesma tela). undefined/false = nenhuma tentativa ainda, ou modal acabou de abrir.
  lastAttemptFailed?: boolean;
}

const VPN_INSTRUCTIONS: Record<string, { cloud: string; vpn: string; hint: string }> = {
  eks: {
    cloud: "AWS EKS",
    vpn: "AWS",
    hint: "Se a VPN Azure estiver ativa, desconecte-a e conecte-se à VPN AWS antes de continuar.",
  },
  aks: {
    cloud: "Azure AKS",
    vpn: "Azure",
    hint: "Se a VPN AWS estiver ativa, desconecte-a e conecte-se à VPN Azure antes de continuar.",
  },
};

const DEFAULT_HINT = {
  cloud: "Kubernetes",
  vpn: "correta",
  hint: "Verifique se a VPN do ambiente está ativa e tente novamente.",
};

export function ClusterUnreachableDialog({
  open,
  clusterName,
  cloudProvider,
  onClose,
  onRetry,
  retrying = false,
  lastAttemptFailed = false,
}: ClusterUnreachableDialogProps) {
  const info = VPN_INSTRUCTIONS[cloudProvider] ?? DEFAULT_HINT;

  return (
    <AlertDialog open={open} onOpenChange={(o) => { if (!o) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <WifiOff className="w-5 h-5 text-destructive" />
            Cluster inacessível
          </AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-3 text-sm">
              <p>
                O cluster <span className="font-medium">{clusterName}</span>
                {info.cloud !== "Kubernetes" && (
                  <> ({info.cloud})</>
                )}{" "}
                não está respondendo.
              </p>
              <p className="text-amber-600 dark:text-amber-400 font-medium">
                {info.hint}
              </p>
              <p className="text-muted-foreground">
                Após trocar a VPN, clique em <span className="font-medium">Tentar novamente</span>.
              </p>
              {lastAttemptFailed && !retrying && (
                <p className="text-destructive font-medium">
                  Ainda inacessível — verifique a VPN e tente de novo.
                </p>
              )}
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onClose}>Ignorar</AlertDialogCancel>
          {/* AlertDialogAction == DialogPrimitive.Close por baixo do Radix — fecha o modal
              IMEDIATAMENTE ao clicar, antes mesmo do teste de conectividade rodar, a menos que o
              evento tenha preventDefault() chamado (só então Radix não dispara onOpenChange(false)
              internamente). Sem isso, "Tentar novamente" sempre fechava o modal na hora do clique,
              e só um toast (fácil de perder) avisava se o cluster continuava inacessível — bug
              real reportado: o retry "falhava silenciosamente" e a função ficava perdida, já que
              reabrir o modal exige trocar de cluster de novo. */}
          <AlertDialogAction
            onClick={(e) => {
              e.preventDefault();
              onRetry();
            }}
            disabled={retrying}
          >
            {retrying ? "Testando..." : "Tentar novamente"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
