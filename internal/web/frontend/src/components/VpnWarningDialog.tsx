import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

interface VpnWarningDialogProps {
  open: boolean;
  cloudProvider: string;
  clusterName: string;
  onClose: () => void;
}

const CLOUD_LABELS: Record<string, { name: string; vpn: string }> = {
  eks: { name: "AWS EKS", vpn: "AWS" },
  aks: { name: "Azure AKS", vpn: "Azure" },
};

export function VpnWarningDialog({ open, cloudProvider, clusterName, onClose }: VpnWarningDialogProps) {
  const info = CLOUD_LABELS[cloudProvider];
  if (!info) return null;

  return (
    <AlertDialog open={open} onOpenChange={(o) => { if (!o) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            Cluster {info.name} selecionado
          </AlertDialogTitle>
          <AlertDialogDescription asChild>
            <div className="space-y-2 text-sm">
              <p>
                O cluster <span className="font-medium">{clusterName}</span> é{" "}
                <span className="font-medium">{info.name}</span>.
              </p>
              <p>
                Verifique se sua <span className="font-medium">VPN {info.vpn}</span> está
                ativa antes de continuar.
              </p>
            </div>
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogAction onClick={onClose}>OK, VPN verificada</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
