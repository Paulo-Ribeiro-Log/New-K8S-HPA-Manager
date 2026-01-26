import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { UserCircle, ArrowRight } from 'lucide-react';

interface CredentialRedirectDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  credentialName: string;
}

/**
 * Dialog que informa ao usuario que a configuracao foi movida para o menu de perfil
 */
export function CredentialRedirectDialog({
  open,
  onOpenChange,
  credentialName,
}: CredentialRedirectDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UserCircle className="h-5 w-5" />
            Configuracao Centralizada
          </DialogTitle>
          <DialogDescription className="pt-2">
            A configuracao do <strong>{credentialName}</strong> foi movida para o menu de perfil.
          </DialogDescription>
        </DialogHeader>

        <div className="py-4">
          <div className="flex items-center gap-3 p-3 bg-muted rounded-lg">
            <div className="flex-1">
              <p className="text-sm font-medium">Como configurar:</p>
              <p className="text-xs text-muted-foreground mt-1">
                Clique no seu nome/avatar no canto superior direito e selecione "{credentialName}" na secao Credenciais.
              </p>
            </div>
            <ArrowRight className="h-5 w-5 text-muted-foreground" />
          </div>
        </div>

        <DialogFooter>
          <Button onClick={() => onOpenChange(false)}>
            Entendi
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
