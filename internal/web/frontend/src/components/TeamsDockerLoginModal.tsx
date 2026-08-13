import { useState } from "react";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Loader2, X, MonitorPlay, ShieldCheck } from "lucide-react";

interface TeamsDockerLoginModalProps {
  /** URL do noVNC — só usada como fallback avançado, ver comentário abaixo. */
  vncUrl: string | null;
  /** Número do "number matching" do Authenticator (1-3 dígitos) — visão padrão do modal. */
  mfaNumber: string | null;
  /** Usuário fechou manualmente — sessão/polling continuam, só a UI fica oculta até reabrir. */
  dismissed: boolean;
  onDismiss: () => void;
  onReopen: () => void;
}

// Modal do login Docker do Teams (modo opt-in K8S_HPA_TEAMS_DOCKER_BROWSER) — abre sozinho assim
// que ServiceNowImportModal detecta (via polling ambiente em GetDockerSession) que existe um
// login em andamento. E-mail/senha são preenchidos automaticamente pelo Perfil SSO
// (internal/browser/sso_autologin.go) — a ÚNICA coisa que sobra pro usuário fazer, na prática, é
// aprovar o MFA no celular digitando o número que o Azure AD mostra ("number matching"). Por isso
// a visão padrão é só esse número, grande — não a tela inteira do Chrome via noVNC. Bug real
// corrigido: a primeira versão embutia um iframe do noVNC como visão padrão; o usuário tinha que
// caçar o número dentro de uma tela de remote-desktop pequena e lenta, quando ele já sabe
// exatamente o que fazer com o número assim que o vê.
export function TeamsDockerLoginModal({ vncUrl, mfaNumber, dismissed, onDismiss, onReopen }: TeamsDockerLoginModalProps) {
  const [showAdvanced, setShowAdvanced] = useState(false);

  if (!vncUrl) return null;

  if (dismissed) {
    return (
      <div className="fixed bottom-4 right-4 z-50">
        <Button size="sm" variant="secondary" className="shadow-lg gap-2" onClick={onReopen}>
          <MonitorPlay className="h-4 w-4" />
          {mfaNumber ? `Login Teams: digite ${mfaNumber} no celular` : "Ver login do Teams em andamento"}
        </Button>
      </div>
    );
  }

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onDismiss(); }}>
      <DialogContent className={showAdvanced ? "max-w-4xl h-[80vh] flex flex-col overflow-hidden" : "max-w-lg"}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Loader2 className="h-4 w-4 animate-spin" />
            Login do Teams em andamento
          </DialogTitle>
          {!showAdvanced && (
            <DialogDescription>
              E-mail e senha já foram preenchidos automaticamente pelo Perfil SSO.
            </DialogDescription>
          )}
        </DialogHeader>

        {!showAdvanced && (
          <div className="flex flex-col items-center gap-4 py-4">
            {mfaNumber ? (
              <>
                <ShieldCheck className="h-8 w-8 text-primary" />
                <div className="text-sm text-muted-foreground text-center">
                  Abra o Microsoft Authenticator no celular e digite o número abaixo pra aprovar o login:
                </div>
                <div className="text-6xl font-bold tracking-widest tabular-nums">{mfaNumber}</div>
              </>
            ) : (
              <div className="text-sm text-muted-foreground text-center">
                Aguardando a tela de aprovação (MFA) aparecer... Se o seu login não usa
                "number matching", pode ser necessário agir na tela cheia — use "Avançado" abaixo.
              </div>
            )}
          </div>
        )}

        {showAdvanced && (
          <div className="flex-1 min-h-0 rounded border border-border overflow-hidden bg-black">
            <iframe
              src={vncUrl}
              title="Login do Teams (Chrome em Docker via noVNC)"
              className="w-full h-full border-0"
              allow="clipboard-read; clipboard-write"
            />
          </div>
        )}

        <div className="flex justify-between items-center flex-shrink-0">
          <Button variant="ghost" size="sm" onClick={() => setShowAdvanced(v => !v)}>
            {showAdvanced ? "Ver só o número" : "Avançado: ver tela completa"}
          </Button>
          <Button variant="outline" size="sm" onClick={onDismiss}>
            <X className="h-3.5 w-3.5 mr-1" />
            Ocultar (continua em andamento)
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
