import { ShieldCheck } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { AWXCertForm } from "@/components/AWXCertForm";

interface AWXCertModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace: string;
}

export function AWXCertModal({ open, onOpenChange, cluster, namespace }: AWXCertModalProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="max-w-2xl"
        onInteractOutside={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldCheck className="w-5 h-5 text-blue-500" />
            Certificado TLS — AWX
          </DialogTitle>
        </DialogHeader>

        {open && (
          <AWXCertForm
            key={`${cluster}-${namespace}`}
            cluster={cluster}
            namespace={namespace}
            onCancel={() => onOpenChange(false)}
            onSuccess={() => onOpenChange(false)}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}
