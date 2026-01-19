import { useState, useEffect } from 'react';
import { useNexusConfig } from '../hooks/useNexus';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from './ui/card';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from './ui/dialog';
import { Loader2, CheckCircle2, AlertCircle, Settings, Trash2 } from 'lucide-react';
import { Alert, AlertDescription } from './ui/alert';
import type { NexusConfig } from '../types/nexus';
import { DEFAULT_URL_PATTERN } from '../types/nexus';

interface NexusConfigPanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfigSaved?: () => void;
}

export const NexusConfigPanel = ({ open, onOpenChange, onConfigSaved }: NexusConfigPanelProps) => {
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

  // Carrega configuração existente ao abrir
  useEffect(() => {
    if (open) {
      loadExistingConfig();
    }
  }, [open]);

  const loadExistingConfig = async () => {
    try {
      const existingConfig = await loadConfig();
      if (existingConfig) {
        setConfig(existingConfig);
      }
    } catch (err) {
      // Config não existe ainda, use valores padrão
    }
  };

  const handleTestConnection = async () => {
    if (!config.baseUrl || !config.repository || !config.username || !config.password) {
      setTestResult({
        success: false,
        message: 'Preencha todos os campos obrigatórios',
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
        message: err instanceof Error ? err.message : 'Erro ao testar conexão',
      });
    } finally {
      setTesting(false);
    }
  };

  const handleSave = async () => {
    if (!config.baseUrl || !config.repository || !config.username || !config.password) {
      setTestResult({
        success: false,
        message: 'Preencha todos os campos obrigatórios',
      });
      return;
    }

    setSaving(true);
    try {
      await saveConfig(config);
      setTestResult({
        success: true,
        message: 'Configuração salva com sucesso!',
      });
      onConfigSaved?.();
      setTimeout(() => {
        onOpenChange(false);
      }, 1500);
    } catch (err) {
      setTestResult({
        success: false,
        message: err instanceof Error ? err.message : 'Erro ao salvar configuração',
      });
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!confirm('Tem certeza que deseja remover a configuração do Nexus?')) {
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
        message: 'Configuração removida com sucesso!',
      });
    } catch (err) {
      setTestResult({
        success: false,
        message: err instanceof Error ? err.message : 'Erro ao remover configuração',
      });
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings className="h-5 w-5" />
            Configuração do Nexus
          </DialogTitle>
          <DialogDescription>
            Configure a conexão com o Nexus Repository Manager para comparar arquivos values.yaml
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-4">
          {/* Base URL */}
          <div className="space-y-2">
            <Label htmlFor="baseUrl">URL Base do Nexus *</Label>
            <Input
              id="baseUrl"
              value={config.baseUrl}
              onChange={(e) => setConfig({ ...config, baseUrl: e.target.value })}
              placeholder="https://nexus.viavarejo.com.br"
            />
          </div>

          {/* Repository */}
          <div className="space-y-2">
            <Label htmlFor="repository">Repository *</Label>
            <Input
              id="repository"
              value={config.repository}
              onChange={(e) => setConfig({ ...config, repository: e.target.value })}
              placeholder="workspace"
            />
          </div>

          {/* Username */}
          <div className="space-y-2">
            <Label htmlFor="username">Usuário SSO *</Label>
            <Input
              id="username"
              value={config.username}
              onChange={(e) => setConfig({ ...config, username: e.target.value })}
              placeholder="seu.usuario"
            />
          </div>

          {/* Password */}
          <div className="space-y-2">
            <Label htmlFor="password">Senha *</Label>
            <Input
              id="password"
              type="password"
              value={config.password}
              onChange={(e) => setConfig({ ...config, password: e.target.value })}
              placeholder="••••••••"
            />
            <p className="text-xs text-muted-foreground">
              A senha será armazenada de forma criptografada
            </p>
          </div>

          {/* Temp Dir */}
          <div className="space-y-2">
            <Label htmlFor="tempDir">Diretório Temporário</Label>
            <Input
              id="tempDir"
              value={config.tempDir}
              onChange={(e) => setConfig({ ...config, tempDir: e.target.value })}
              placeholder="/tmp/k8s-hpa-nexus"
            />
            <p className="text-xs text-muted-foreground">
              Diretório para armazenar downloads temporários
            </p>
          </div>

          {/* URL Pattern */}
          <div className="space-y-2">
            <Label htmlFor="urlPattern">Padrão de URL (Avançado)</Label>
            <Input
              id="urlPattern"
              value={config.urlPattern}
              onChange={(e) => setConfig({ ...config, urlPattern: e.target.value })}
              placeholder={DEFAULT_URL_PATTERN}
            />
            <p className="text-xs text-muted-foreground">
              Placeholders disponíveis: {'{baseUrl}'}, {'{repository}'}, {'{release}'}, {'{version}'}, {'{environment}'}, {'{type}'}
            </p>
            <p className="text-xs text-muted-foreground">
              Deixe vazio para usar o padrão: <code className="bg-muted px-1 rounded">{DEFAULT_URL_PATTERN}</code>
            </p>
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

          {/* Error */}
          {error && !testResult && (
            <Alert variant="destructive">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription>{error}</AlertDescription>
            </Alert>
          )}
        </div>

        <DialogFooter className="flex justify-between">
          <div className="flex gap-2">
            <Button
              variant="outline"
              onClick={handleTestConnection}
              disabled={testing || saving || deleting}
            >
              {testing ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin mr-2" />
                  Testando...
                </>
              ) : (
                'Testar Conexão'
              )}
            </Button>

            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={testing || saving || deleting}
            >
              {deleting ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin mr-2" />
                  Removendo...
                </>
              ) : (
                <>
                  <Trash2 className="h-4 w-4 mr-2" />
                  Remover
                </>
              )}
            </Button>
          </div>

          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              Cancelar
            </Button>
            <Button
              onClick={handleSave}
              disabled={testing || saving || deleting}
            >
              {saving ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin mr-2" />
                  Salvando...
                </>
              ) : (
                'Salvar'
              )}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
