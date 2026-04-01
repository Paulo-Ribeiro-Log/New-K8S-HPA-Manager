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
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onClose}>Ignorar</AlertDialogCancel>
          <AlertDialogAction onClick={onRetry} disabled={retrying}>
            {retrying ? "Testando..." : "Tentar novamente"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
