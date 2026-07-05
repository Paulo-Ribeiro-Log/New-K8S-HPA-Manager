import { useCallback, useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import { apiClient } from "@/lib/api/client";
import { CloudAccountHintField } from "@/components/CloudAccountHintField";
import type { AwsSsoState } from "@/hooks/useAwsSsoAuth";

interface Props {
  state: AwsSsoState;
  onRetryAfterConfig: (profile: string) => void;
  onClose: () => void;
}

interface SsoFormValues {
  startUrl: string;
  ssoRegion: string;
  accountId: string;
  roleName: string;
  region: string;
}

export function AwsSsoLoginDialog({ state, onRetryAfterConfig, onClose }: Props) {
  const [form, setForm] = useState<SsoFormValues>({
    startUrl: "",
    ssoRegion: "us-east-1",
    accountId: "",
    roleName: "Administrators",
    region: "us-east-1",
  });
  const [saving, setSaving] = useState(false);

  const handleCopyUrl = useCallback(() => {
    if (state.url) {
      navigator.clipboard.writeText(state.url);
      toast.success("URL copiada!");
    }
  }, [state.url]);

  const handleOpenUrl = useCallback(() => {
    if (state.url) window.open(state.url, "_blank");
  }, [state.url]);

  const handleSaveConfig = useCallback(async () => {
    if (!form.startUrl || !form.accountId || !form.roleName) {
      toast.error("Preencha todos os campos obrigatórios.");
      return;
    }
    setSaving(true);
    try {
      await apiClient.saveAwsSsoConfig(
        state.profile,
        { start_url: form.startUrl, region: form.ssoRegion, account_id: form.accountId, role_name: form.roleName },
        form.region,
      );
      toast.success(`Configuração salva para o perfil "${state.profile}"`);
      onRetryAfterConfig(state.profile);
    } catch (err: unknown) {
      toast.error(err instanceof Error ? err.message : "Erro ao salvar configuração");
    } finally {
      setSaving(false);
    }
  }, [form, state.profile, onRetryAfterConfig]);

  const field = (id: keyof SsoFormValues, label: string, placeholder: string, required = true) => (
    <div className="space-y-1">
      <Label htmlFor={id}>
        {label}{required && <span className="text-destructive ml-1">*</span>}
      </Label>
      <Input
        id={id}
        value={form[id]}
        onChange={(e) => setForm((f) => ({ ...f, [id]: e.target.value }))}
        placeholder={placeholder}
      />
    </div>
  );

  const title = state.success
    ? "Autenticado com sucesso!"
    : state.needsConfig
    ? "Configurar AWS SSO"
    : "Login AWS SSO necessário";

  return (
    <Dialog open={state.open} onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span>🔐</span> {title}
          </DialogTitle>
          <DialogDescription asChild>
            <div className="space-y-1 text-sm">
              <span>Perfil AWS: </span>
              <Badge variant="secondary" className="font-mono">{state.profile || "—"}</Badge>
            </div>
          </DialogDescription>
        </DialogHeader>

        {/* Sucesso */}
        {state.success && (
          <div className="flex flex-col items-center gap-3 py-4 text-center">
            <span className="text-4xl">✅</span>
            <p className="text-sm text-muted-foreground">
              Token renovado. As operações no cluster serão retomadas automaticamente.
            </p>
          </div>
        )}

        {/* Carregando */}
        {state.loading && !state.success && (
          <div className="flex flex-col items-center gap-3 py-6 text-center">
            <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
            <p className="text-sm text-muted-foreground">Iniciando login SSO…</p>
          </div>
        )}

        {/* Erro */}
        {state.error && !state.loading && (
          <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
            {state.error}
          </div>
        )}

        {/* Formulário de configuração SSO */}
        {state.needsConfig && !state.loading && (
          <div className="space-y-3">
            <p className="text-sm text-muted-foreground">
              O perfil <span className="font-medium">"{state.profile}"</span> ainda não está configurado.
              Preencha as informações do AWS SSO:
            </p>
            {field("startUrl", "Start URL", "https://myorg.awsapps.com/start")}
            {field("ssoRegion", "Região SSO", "us-east-1")}
            {field("accountId", "Account ID", "123456789012")}
            {field("roleName", "Role Name", "Administrators")}
            {field("region", "Região padrão", "us-east-1", false)}
          </div>
        )}

        {/* URL de autorização + código */}
        {state.url && !state.loading && !state.success && (
          <div className="space-y-4">
            <div className="rounded-md bg-muted p-3 space-y-2">
              <p className="text-sm font-medium">1. Abra a URL no navegador:</p>
              <div className="flex gap-2">
                <Input
                  readOnly
                  value={state.url}
                  className="font-mono text-xs"
                />
                <Button variant="outline" size="sm" onClick={handleCopyUrl}>Copiar</Button>
                <Button variant="outline" size="sm" onClick={handleOpenUrl}>Abrir</Button>
              </div>
            </div>

            {state.userCode && (
              <div className="rounded-md bg-muted p-3 space-y-1 text-center">
                <p className="text-sm font-medium">2. Insira o código:</p>
                <p className="font-mono text-2xl font-bold tracking-widest text-primary">
                  {state.userCode}
                </p>
              </div>
            )}

            <CloudAccountHintField provider="aws" />

            {state.polling && (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <div className="h-4 w-4 animate-spin rounded-full border-2 border-primary border-t-transparent" />
                Aguardando autenticação…
              </div>
            )}
          </div>
        )}

        <DialogFooter className="gap-2">
          {state.needsConfig && !state.loading && (
            <Button onClick={handleSaveConfig} disabled={saving}>
              {saving ? "Salvando…" : "Salvar e continuar"}
            </Button>
          )}
          {!state.success && (
            <Button variant="outline" onClick={onClose}>
              {state.polling ? "Cancelar" : "Fechar"}
            </Button>
          )}
          {state.success && (
            <Button onClick={onClose}>Fechar</Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
