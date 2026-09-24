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
import { CheckCircle2, XCircle, Info, Loader2, AlertTriangle, Eye, EyeOff, User } from 'lucide-react';
import { toast } from 'sonner';
import { useUserPermissions } from '@/hooks/useUserPermissions';
import { CloudAccountHintField } from '@/components/CloudAccountHintField';
import { apiClient } from '@/lib/api/client';
import type { CredentialModalProps } from '@/types/profile';

type DtEnv = 'prd' | 'hlg';
type TestResult = { success: boolean; latency_ms?: number; error?: string };

// Identidade vinculada ao login real (RBAC/JWT) — não mais um "ai_email" digitado manualmente.
// Ver DYNATRACE-PROFILE-MIGRATION-PLAN.md: o backend deriva o e-mail via InjectUserEmail(), o
// mesmo mecanismo já usado por GitHubCredentialModal/Nexus/ServiceNow/AWX.
export function DynatraceCredentialModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const { data: userPerms } = useUserPermissions();
  const rbacEmail = userPerms?.email || '';

  const [url, setUrl] = useState('');
  const [token, setToken] = useState('');
  const [tagFilter, setTagFilter] = useState('');
  const [showToken, setShowToken] = useState(false);
  const [hasToken, setHasToken] = useState(false);
  const [loadingConfig, setLoadingConfig] = useState(false);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState<DtEnv | null>(null);
  const [testResult, setTestResult] = useState<Partial<Record<DtEnv, TestResult>>>({});
  // Tenant de homologação (opcional) — clusters não-produtivos (-hlg/-dev/-stg/...) reportam para
  // um tenant separado; sem ele configurado, esses clusters continuam usando o de produção.
  const [hlgUrl, setHlgUrl] = useState('');
  const [hlgToken, setHlgToken] = useState('');
  const [showHlgToken, setShowHlgToken] = useState(false);
  const [hlgHasToken, setHlgHasToken] = useState(false);

  const loadConfig = async () => {
    setLoadingConfig(true);
    try {
      const cfg = await apiClient.getDynatraceConfig();
      setUrl(cfg.base_url ?? '');
      setTagFilter(cfg.tag_filter ?? '');
      setHasToken(cfg.has_token ?? false);
      setHlgUrl(cfg.hlg_base_url ?? '');
      setHlgHasToken(cfg.hlg_has_token ?? false);
    } catch {
      // silencioso — modal fica com os campos vazios, usuário pode configurar do zero
    } finally {
      setLoadingConfig(false);
    }
  };

  useEffect(() => {
    if (open) {
      setToken('');
      setShowToken(false);
      setHlgToken('');
      setShowHlgToken(false);
      setTestResult({});
      loadConfig();
    }
  }, [open]);

  const handleTest = async (env: DtEnv) => {
    setTesting(env);
    setTestResult((prev) => ({ ...prev, [env]: undefined }));
    try {
      const result = await apiClient.testDynatraceConnection(env);
      setTestResult((prev) => ({ ...prev, [env]: result }));
    } catch (error) {
      setTestResult((prev) => ({
        ...prev,
        [env]: { success: false, error: error instanceof Error ? error.message : 'Erro desconhecido' },
      }));
    } finally {
      setTesting(null);
    }
  };

  const handleSave = async () => {
    if (!url.trim()) {
      toast.error('URL do ambiente Dynatrace é obrigatória');
      return;
    }
    if (hlgUrl.trim() && !hlgHasToken && !hlgToken.trim()) {
      toast.error('Informe o API Token do ambiente de homologação');
      return;
    }
    setSaving(true);
    try {
      const result = await apiClient.saveDynatraceConfig({
        dynatrace_url: url.trim(),
        dynatrace_token: token.trim() || undefined,
        dynatrace_tag_filter: tagFilter,
        dynatrace_hlg_url: hlgUrl.trim(),
        dynatrace_hlg_token: hlgToken.trim() || undefined,
      });
      setHasToken(result.has_token);
      setToken('');
      setHlgUrl(result.hlg_base_url ?? '');
      setHlgHasToken(result.hlg_has_token ?? false);
      setHlgToken('');
      toast.success('Configuração Dynatrace salva');
      onSaved?.();
    } catch (error) {
      toast.error('Erro ao salvar configuração', {
        description: error instanceof Error ? error.message : 'Erro desconhecido',
      });
    } finally {
      setSaving(false);
    }
  };

  const isProcessing = saving || testing !== null;

  const renderTest = (env: DtEnv, enabled: boolean) => {
    const res = testResult[env];
    return (
      <div className="flex items-center gap-3">
        <Button type="button" variant="outline" size="sm" onClick={() => handleTest(env)} disabled={isProcessing || !enabled}>
          {testing === env ? <><Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" />Testando...</> : 'Testar Conexão'}
        </Button>
        {res && (
          res.success
            ? <span className="text-xs text-green-600 flex items-center gap-1">
                <CheckCircle2 className="h-3.5 w-3.5" />
                Conectado ({res.latency_ms}ms)
              </span>
            : <span className="text-xs text-red-500 flex items-center gap-1">
                <XCircle className="h-3.5 w-3.5" />
                {res.error ?? 'Falha na conexão'}
              </span>
        )}
      </div>
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg flex flex-col max-h-[90vh]" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader className="flex-shrink-0">
          <DialogTitle className="flex items-center gap-2">
            <AlertTriangle className="h-5 w-5" />
            Dynatrace
          </DialogTitle>
          <DialogDescription>
            Token individual para análise de problems com AI. Cada analista usa seu próprio token.
          </DialogDescription>
        </DialogHeader>

        <div className="overflow-y-auto flex-1 min-h-0 pr-1">
          <div className="space-y-4 py-2">
            {/* Identidade vinculada (RBAC) */}
            <div className="flex items-center gap-2 px-3 py-2 rounded-md bg-muted/50 border text-sm">
              <User className="h-4 w-4 text-muted-foreground shrink-0" />
              <div className="min-w-0">
                <p className="text-[11px] text-muted-foreground">Token vinculado ao usuário</p>
                <p className="font-mono font-medium truncate">{rbacEmail || 'Carregando...'}</p>
              </div>
            </div>

            {loadingConfig ? (
              <Alert>
                <Loader2 className="h-4 w-4 animate-spin" />
                <AlertDescription>Carregando configuração...</AlertDescription>
              </Alert>
            ) : hasToken ? (
              <Alert className="border-green-200 bg-green-50 dark:bg-green-950 dark:border-green-800">
                <CheckCircle2 className="h-4 w-4 text-green-600" />
                <AlertTitle className="text-green-900 dark:text-green-100">Configurado</AlertTitle>
              </Alert>
            ) : (
              <Alert>
                <Info className="h-4 w-4" />
                <AlertDescription className="text-sm">
                  Nenhum token configurado ainda.
                </AlertDescription>
              </Alert>
            )}

            <p className="text-sm font-medium">Produção</p>

            <div className="space-y-2">
              <Label htmlFor="dt-url" className="text-xs">URL do Ambiente Dynatrace</Label>
              <Input
                id="dt-url"
                type="text"
                placeholder="https://xxxxxxxx.live.dynatrace.com"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                disabled={isProcessing}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="dt-token" className="text-xs">API Token</Label>
              <div className="flex gap-2">
                <Input
                  id="dt-token"
                  type={showToken ? 'text' : 'password'}
                  placeholder={hasToken ? '•••••••••••• (deixe em branco pra manter)' : 'dt0c01.XXXXXXXXXX...'}
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  disabled={isProcessing}
                  className="font-mono text-sm"
                />
                <Button type="button" variant="outline" size="icon" onClick={() => setShowToken(!showToken)} disabled={isProcessing}>
                  {showToken ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                Escopos necessários: <code className="bg-muted px-1 rounded">problems.read</code>{' '}
                <code className="bg-muted px-1 rounded">entities.read</code>{' '}
                <code className="bg-muted px-1 rounded">metrics.read</code>{' '}
                <code className="bg-muted px-1 rounded">events.read</code>
              </p>
              <CloudAccountHintField provider="dynatrace" />
            </div>

            {renderTest('prd', hasToken)}

            <div className="space-y-2 border-t pt-4">
              <p className="text-sm font-medium">
                Homologação <span className="text-muted-foreground font-normal">(opcional)</span>
              </p>
              <p className="text-xs text-muted-foreground">
                Usado nos clusters não-produtivos (nome com <code className="bg-muted px-1 rounded">hlg</code>,{' '}
                <code className="bg-muted px-1 rounded">dev</code>, <code className="bg-muted px-1 rounded">stg</code>,{' '}
                <code className="bg-muted px-1 rounded">preprod</code>…). Sem ele, esses clusters usam o ambiente de produção.
                Deixe a URL em branco para remover.
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="dt-hlg-url" className="text-xs">URL do Ambiente Dynatrace (homologação)</Label>
              <Input
                id="dt-hlg-url"
                type="text"
                placeholder="https://xxxxxxxx.live.dynatrace.com"
                value={hlgUrl}
                onChange={(e) => setHlgUrl(e.target.value)}
                disabled={isProcessing}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="dt-hlg-token" className="text-xs">API Token (homologação)</Label>
              <div className="flex gap-2">
                <Input
                  id="dt-hlg-token"
                  type={showHlgToken ? 'text' : 'password'}
                  placeholder={hlgHasToken ? '•••••••••••• (deixe em branco pra manter)' : 'dt0c01.XXXXXXXXXX...'}
                  value={hlgToken}
                  onChange={(e) => setHlgToken(e.target.value)}
                  disabled={isProcessing}
                  className="font-mono text-sm"
                />
                <Button type="button" variant="outline" size="icon" onClick={() => setShowHlgToken(!showHlgToken)} disabled={isProcessing}>
                  {showHlgToken ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">Mesmos escopos do token de produção.</p>
            </div>

            {renderTest('hlg', hlgHasToken)}

            <div className="border-t" />

            <div className="space-y-2">
              <Label htmlFor="dt-tag-filter" className="text-xs">
                Filtro por Management Zone <span className="text-muted-foreground font-normal">(opcional)</span>
              </Label>
              <Input
                id="dt-tag-filter"
                type="text"
                placeholder="ex: SRE-LOGISTICA (use tag:nome para filtrar por entity tag)"
                value={tagFilter}
                onChange={(e) => setTagFilter(e.target.value)}
                disabled={isProcessing}
              />
              <p className="text-xs text-muted-foreground">
                Filtra problems pela Management Zone (corresponde ao alert profile do seu squad).
                Prefixe com <code className="bg-muted px-1 rounded">tag:</code> para filtrar por entity tag.
                Deixe em branco para ver todos os problems do ambiente.
              </p>
            </div>
          </div>
        </div>

        <DialogFooter className="flex-shrink-0">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={isProcessing}>
            Cancelar
          </Button>
          <Button size="sm" onClick={handleSave} disabled={isProcessing || !url.trim()}>
            {saving ? <Loader2 className="h-4 w-4 animate-spin mr-2" /> : null}
            Salvar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
