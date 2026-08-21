import { useState, useEffect } from 'react';
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
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { CheckCircle2, XCircle, Info, Loader2, Search, Eye, EyeOff, User } from 'lucide-react';
import { toast } from 'sonner';
import { useUserPermissions } from '@/hooks/useUserPermissions';
import { apiClient } from '@/lib/api/client';
import type { CredentialModalProps } from '@/types/profile';

// Credencial Elasticsearch (HEALTHCHECK-TRIAGE-MODE-PLAN.md Fase 3) — usada hoje só como fonte de
// triagem do Health Check (ElasticsearchTargetSource), não uma aba própria de observabilidade.
// Mesmo padrão do DynatraceCredentialModal.tsx: identidade via login real (RBAC/JWT), token
// individual por analista.
export function ElasticsearchCredentialModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const { data: userPerms } = useUserPermissions();
  const rbacEmail = userPerms?.email || '';

  const [url, setUrl] = useState('');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [indexPattern, setIndexPattern] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [hasPassword, setHasPassword] = useState(false);
  const [loadingConfig, setLoadingConfig] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; latency_ms?: number; error?: string } | null>(null);

  const loadConfig = async () => {
    setLoadingConfig(true);
    try {
      const cfg = await apiClient.getElasticsearchConfig();
      setUrl(cfg.base_url ?? '');
      setUsername(cfg.username ?? '');
      setIndexPattern(cfg.index_pattern ?? '');
      setHasPassword(cfg.has_password ?? false);
    } catch {
      // silencioso — modal fica com os campos vazios, usuário pode configurar do zero
    } finally {
      setLoadingConfig(false);
    }
  };

  useEffect(() => {
    if (open) {
      setPassword('');
      setShowPassword(false);
      setTestResult(null);
      loadConfig();
    }
  }, [open]);

  const handleTest = async () => {
    setTesting(true);
    setTestResult(null);
    try {
      const result = await apiClient.testElasticsearchConnection();
      setTestResult(result);
    } catch (error) {
      setTestResult({ success: false, error: error instanceof Error ? error.message : 'Erro desconhecido' });
    } finally {
      setTesting(false);
    }
  };

  const handleSave = async () => {
    if (!url.trim() || !username.trim()) {
      toast.error('URL e usuário do Elasticsearch são obrigatórios');
      return;
    }
    setSaving(true);
    try {
      const result = await apiClient.saveElasticsearchConfig({
        elasticsearch_url: url.trim(),
        elasticsearch_username: username.trim(),
        elasticsearch_password: password.trim() || undefined,
        elasticsearch_index_pattern: indexPattern.trim(),
      });
      setHasPassword(result.has_password);
      setPassword('');
      toast.success('Configuração Elasticsearch salva');
      onSaved?.();
    } catch (error) {
      toast.error('Erro ao salvar configuração', {
        description: error instanceof Error ? error.message : 'Erro desconhecido',
      });
    } finally {
      setSaving(false);
    }
  };

  const isProcessing = saving || testing;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg flex flex-col max-h-[90vh]" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2">
            <Search className="h-5 w-5" />
            Elasticsearch
          </DialogTitle>
          <DialogDescription>
            Credencial individual usada pelo Modo Triagem do Health Check pra contar erros de log
            por namespace. Acesso direto ao Elasticsearch (Basic Auth), sem passar pelo Kibana.
          </DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto flex-1 min-h-0 pr-1">
          <div className="space-y-4 py-2">
            {/* Identidade vinculada (RBAC) */}
            <div className="flex items-center gap-2 px-3 py-2 rounded-md bg-muted/50 border text-sm">
              <User className="h-4 w-4 text-muted-foreground shrink-0" />
              <div className="min-w-0">
                <p className="text-[11px] text-muted-foreground">Credencial vinculada ao usuário</p>
                <p className="font-mono font-medium truncate">{rbacEmail || 'Carregando...'}</p>
              </div>
            </div>

            {loadingConfig ? (
              <Alert>
                <Loader2 className="h-4 w-4 animate-spin" />
                <AlertDescription>Carregando configuração...</AlertDescription>
              </Alert>
            ) : hasPassword ? (
              <Alert className="border-green-200 bg-green-50 dark:bg-green-950 dark:border-green-800">
                <CheckCircle2 className="h-4 w-4 text-green-600" />
                <AlertTitle className="text-green-900 dark:text-green-100">Configurado</AlertTitle>
              </Alert>
            ) : (
              <Alert>
                <Info className="h-4 w-4" />
                <AlertDescription className="text-sm">
                  Nenhuma credencial configurada ainda.
                </AlertDescription>
              </Alert>
            )}

            <div className="space-y-2">
              <Label htmlFor="es-url" className="text-xs">URL do Elasticsearch</Label>
              <Input
                id="es-url"
                type="text"
                placeholder="https://elastic.empresa.com:9200"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                disabled={isProcessing}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="es-username" className="text-xs">Usuário</Label>
              <Input
                id="es-username"
                type="text"
                placeholder="usuário de leitura"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                disabled={isProcessing}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="es-password" className="text-xs">Senha</Label>
              <div className="flex gap-2">
                <Input
                  id="es-password"
                  type={showPassword ? 'text' : 'password'}
                  placeholder={hasPassword ? '•••••••••••• (deixe em branco pra manter)' : 'senha'}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  disabled={isProcessing}
                  className="font-mono text-sm"
                />
                <Button type="button" variant="outline" size="icon" onClick={() => setShowPassword(!showPassword)} disabled={isProcessing}>
                  {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="es-index-pattern" className="text-xs">
                Padrão de Índice <span className="text-muted-foreground font-normal">(opcional)</span>
              </Label>
              <Input
                id="es-index-pattern"
                type="text"
                placeholder="ex: k8s-logs-* (padrão: * — busca em todos os índices, pode ser lento)"
                value={indexPattern}
                onChange={(e) => setIndexPattern(e.target.value)}
                disabled={isProcessing}
              />
              <p className="text-xs text-muted-foreground">
                Recomendado configurar algo específico do seu pipeline de log — buscar em{' '}
                <code className="bg-muted px-1 rounded">*</code> (todos os índices) funciona, mas
                pode ser lento num cluster Elasticsearch grande.
              </p>
            </div>

            <Alert>
              <Info className="h-4 w-4" />
              <AlertDescription className="text-xs">
                Convenções assumidas (não confirmadas contra um índice real — ajuste o código se
                não bater): campo de namespace <code className="bg-muted px-1 rounded">kubernetes.namespace_name</code>,
                nível de log <code className="bg-muted px-1 rounded">level</code>, timestamp{' '}
                <code className="bg-muted px-1 rounded">@timestamp</code>, cluster{' '}
                <code className="bg-muted px-1 rounded">cluster_name</code>.
              </AlertDescription>
            </Alert>

            <div className="flex items-center gap-3">
              <Button type="button" variant="outline" size="sm" onClick={handleTest} disabled={isProcessing || !hasPassword}>
                {testing ? <><Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />Testando...</> : 'Testar Conexão'}
              </Button>
              {testResult && (
                testResult.success
                  ? <span className="text-xs text-green-600 flex items-center gap-1">
                      <CheckCircle2 className="h-3.5 w-3.5" />
                      Conectado ({testResult.latency_ms}ms)
                    </span>
                  : <span className="text-xs text-red-500 flex items-center gap-1">
                      <XCircle className="h-3.5 w-3.5" />
                      {testResult.error ?? 'Falha na conexão'}
                    </span>
              )}
            </div>
          </div>
        </div>

        <DialogFooter className="flex-shrink-0">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={isProcessing}>
            Cancelar
          </Button>
          <Button size="sm" onClick={handleSave} disabled={isProcessing || !url.trim() || !username.trim()}>
            {saving ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : null}
            Salvar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
