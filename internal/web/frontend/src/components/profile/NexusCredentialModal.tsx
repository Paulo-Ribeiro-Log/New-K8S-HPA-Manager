import { useState, useEffect } from 'react';
import { useNexusConfig } from '@/hooks/useNexus';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog';
import { Loader2, CheckCircle2, AlertCircle, Database, Trash2 } from 'lucide-react';
import { Alert, AlertDescription } from '@/components/ui/alert';
import type { NexusConfig } from '@/types/nexus';
import { DEFAULT_URL_PATTERN } from '@/types/nexus';
import type { CredentialModalProps } from '@/types/profile';

export function NexusCredentialModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const { testConnection, saveConfig, loadConfig, deleteConfig, loading, error } = useNexusConfig();

  const [config, setConfig] = useState<NexusConfig>({
    baseUrl: 'https://nexus.viavarejo.com.br',
    repository: 'workspace',
    username: '',
    password: '',
    tempDir: '/tmp/k8s-hpa-nexus',
    urlPattern: '',
  });

  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null);
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);

  // Carrega configuracao existente ao abrir
  useEffect(() => {
    if (open) {
      loadExistingConfig();
      setTestResult(null);
    }
  }, [open]);

  const loadExistingConfig = async () => {
    try {
      const existingConfig = await loadConfig();
      if (existingConfig) {
        setConfig(existingConfig);
      }
    } catch (err) {
      // Config nao existe ainda, use valores padrao
    }
  };

  const handleTestConnection = async () => {
    if (!config.baseUrl || !config.repository || !config.username || !config.password) {
      setTestResult({
        success: false,
        message: 'Preencha todos os campos obrigatorios',
      });
      return;
    }

    setTesting(true);
    setTestResult(null);

    try {
      const result = await testConnection(config);
      setTestResult(result);
    } catch (err) {
      setTestResult({
        success: false,
        message: err instanceof Error ? err.message : 'Erro ao testar conexao',
      });
    } finally {
      setTesting(false);
    }
  };

  const handleSave = async () => {
    if (!config.baseUrl || !config.repository || !config.username || !config.password) {
      setTestResult({
        success: false,
        message: 'Preencha todos os campos obrigatorios',
      });
      return;
    }

    setSaving(true);
    try {
      await saveConfig(config);
      setTestResult({
        success: true,
        message: 'Configuracao salva com sucesso!',
      });
      onSaved?.();
      setTimeout(() => {
        onOpenChange(false);
      }, 1500);
    } catch (err) {
      setTestResult({
        success: false,
        message: err instanceof Error ? err.message : 'Erro ao salvar configuracao',
      });
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!confirm('Tem certeza que deseja remover a configuracao do Nexus?')) {
      return;
    }

    setDeleting(true);
    try {
      await deleteConfig();
      setConfig({
        baseUrl: 'https://nexus.viavarejo.com.br',
        repository: 'workspace',
        username: '',
        password: '',
        tempDir: '/tmp/k8s-hpa-nexus',
        urlPattern: '',
      });
      setTestResult({
        success: true,
        message: 'Configuracao removida com sucesso!',
      });
      onSaved?.();
    } catch (err) {
      setTestResult({
        success: false,
        message: err instanceof Error ? err.message : 'Erro ao remover configuracao',
      });
    } finally {
      setDeleting(false);
    }
  };

  const isProcessing = testing || saving || deleting;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Database className="h-5 w-5" />
            Nexus Repository
          </DialogTitle>
          <DialogDescription>
            Configure a conexao com o Nexus para comparar arquivos values.yaml
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          <div className="grid grid-cols-2 gap-4">
            {/* Base URL */}
            <div className="space-y-2">
              <Label htmlFor="nexus-baseUrl">URL Base *</Label>
              <Input
                id="nexus-baseUrl"
                value={config.baseUrl}
                onChange={(e) => setConfig({ ...config, baseUrl: e.target.value })}
                placeholder="https://nexus.example.com"
                disabled={isProcessing}
              />
            </div>

            {/* Repository */}
            <div className="space-y-2">
              <Label htmlFor="nexus-repository">Repository *</Label>
              <Input
                id="nexus-repository"
                value={config.repository}
                onChange={(e) => setConfig({ ...config, repository: e.target.value })}
                placeholder="workspace"
                disabled={isProcessing}
              />
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4">
            {/* Username */}
            <div className="space-y-2">
              <Label htmlFor="nexus-username">Usuario *</Label>
              <Input
                id="nexus-username"
                value={config.username}
                onChange={(e) => setConfig({ ...config, username: e.target.value })}
                placeholder="seu.usuario"
                disabled={isProcessing}
              />
            </div>

            {/* Password */}
            <div className="space-y-2">
              <Label htmlFor="nexus-password">Senha *</Label>
              <Input
                id="nexus-password"
                type="password"
                value={config.password}
                onChange={(e) => setConfig({ ...config, password: e.target.value })}
                placeholder="********"
                disabled={isProcessing}
              />
            </div>
          </div>

          {/* URL Pattern (colapsado por padrao) */}
          <details className="text-sm">
            <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
              Configuracoes avancadas
            </summary>
            <div className="mt-2 space-y-2">
              <Label htmlFor="nexus-urlPattern">Padrao de URL</Label>
              <Input
                id="nexus-urlPattern"
                value={config.urlPattern}
                onChange={(e) => setConfig({ ...config, urlPattern: e.target.value })}
                placeholder={DEFAULT_URL_PATTERN}
                disabled={isProcessing}
                className="text-xs"
              />
              <p className="text-xs text-muted-foreground">
                Placeholders: {'{baseUrl}'}, {'{repository}'}, {'{release}'}, {'{version}'}, {'{environment}'}, {'{type}'}
              </p>
            </div>
          </details>

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

          {/* Error */}
          {error && !testResult && (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>{error}</AlertDescription>
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
              {testing ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                'Testar'
              )}
            </Button>

            <Button
              variant="ghost"
              size="sm"
              onClick={handleDelete}
              disabled={isProcessing}
              className="text-destructive hover:text-destructive"
            >
              {deleting ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Trash2 className="h-4 w-4" />
              )}
            </Button>
          </div>

          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
              Cancelar
            </Button>
            <Button size="sm" onClick={handleSave} disabled={isProcessing}>
              {saving ? (
                <Loader2 className="h-4 w-4 animate-spin mr-2" />
              ) : null}
              Salvar
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
