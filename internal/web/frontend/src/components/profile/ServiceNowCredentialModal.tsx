import { useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Alert, AlertDescription } from '@/components/ui/alert';
import {
  Loader2,
  AlertCircle,
  CheckCircle2,
  Trash2,
  Ticket,
  Eye,
  EyeOff,
  Construction,
} from 'lucide-react';
import { toast } from 'sonner';
import type { CredentialModalProps } from '@/types/profile';

// TODO: Criar hook useServiceNowConfig quando backend estiver pronto
// import { useServiceNowConfig } from '@/hooks/useServiceNow';

interface ServiceNowConfig {
  instanceUrl: string;
  username: string;
  password: string;
}

export function ServiceNowCredentialModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const [config, setConfig] = useState<ServiceNowConfig>({
    instanceUrl: '',
    username: '',
    password: '',
  });
  const [showPassword, setShowPassword] = useState(false);
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null);

  // TODO: Integrar com backend quando disponivel
  const isConfigured = false; // Placeholder
  const isProcessing = testing || saving;

  const handleTestConnection = async () => {
    if (!config.instanceUrl || !config.username || !config.password) {
      setTestResult({
        success: false,
        message: 'Preencha todos os campos',
      });
      return;
    }

    setTesting(true);
    setTestResult(null);

    // TODO: Implementar chamada ao backend
    // Simulando teste por enquanto
    setTimeout(() => {
      setTestResult({
        success: false,
        message: 'Integracao com ServiceNow em desenvolvimento',
      });
      setTesting(false);
    }, 1000);
  };

  const handleSave = async () => {
    if (!config.instanceUrl || !config.username || !config.password) {
      toast.error('Preencha todos os campos');
      return;
    }

    setSaving(true);

    // TODO: Implementar chamada ao backend
    // Simulando save por enquanto
    setTimeout(() => {
      toast.info('Integracao com ServiceNow em desenvolvimento');
      setSaving(false);
    }, 500);
  };

  const handleDelete = async () => {
    if (!confirm('Tem certeza que deseja remover a configuracao do ServiceNow?')) {
      return;
    }

    // TODO: Implementar chamada ao backend
    toast.info('Integracao com ServiceNow em desenvolvimento');
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Ticket className="h-5 w-5" />
            ServiceNow
          </DialogTitle>
          <DialogDescription>
            Configure a integracao com ServiceNow para importar CHGs
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {/* Aviso de desenvolvimento */}
          <Alert>
            <Construction className="h-4 w-4" />
            <AlertDescription>
              Esta integracao esta em desenvolvimento. A configuracao sera persistida em breve.
            </AlertDescription>
          </Alert>

          {/* Instance URL */}
          <div className="space-y-2">
            <Label htmlFor="snow-instance">URL da Instancia</Label>
            <Input
              id="snow-instance"
              value={config.instanceUrl}
              onChange={(e) => setConfig({ ...config, instanceUrl: e.target.value })}
              placeholder="https://empresa.service-now.com"
              disabled={isProcessing}
            />
          </div>

          <div className="grid grid-cols-2 gap-4">
            {/* Username */}
            <div className="space-y-2">
              <Label htmlFor="snow-username">Usuario</Label>
              <Input
                id="snow-username"
                value={config.username}
                onChange={(e) => setConfig({ ...config, username: e.target.value })}
                placeholder="seu.usuario"
                disabled={isProcessing}
              />
            </div>

            {/* Password */}
            <div className="space-y-2">
              <Label htmlFor="snow-password">Senha</Label>
              <div className="flex gap-1">
                <Input
                  id="snow-password"
                  type={showPassword ? 'text' : 'password'}
                  value={config.password}
                  onChange={(e) => setConfig({ ...config, password: e.target.value })}
                  placeholder="********"
                  disabled={isProcessing}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  onClick={() => setShowPassword(!showPassword)}
                  disabled={isProcessing}
                >
                  {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
              </div>
            </div>
          </div>

          {/* Test Result */}
          {testResult && (
            <Alert variant={testResult.success ? 'default' : 'destructive'}>
              {testResult.success ? (
                <CheckCircle2 className="h-4 w-4" />
              ) : (
                <AlertCircle className="h-4 w-4" />
              )}
              <AlertDescription>{testResult.message}</AlertDescription>
            </Alert>
          )}
        </div>

        <DialogFooter className="flex justify-between sm:justify-between">
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleTestConnection}
              disabled={isProcessing}
            >
              {testing ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Testar'}
            </Button>

            {isConfigured && (
              <Button
                variant="ghost"
                size="sm"
                onClick={handleDelete}
                disabled={isProcessing}
                className="text-destructive hover:text-destructive"
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            )}
          </div>

          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
              Cancelar
            </Button>
            <Button size="sm" onClick={handleSave} disabled={isProcessing}>
              {saving ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : null}
              Salvar
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
