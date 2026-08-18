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
import { Badge } from '@/components/ui/badge';
import { Separator } from '@/components/ui/separator';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command';
import {
  CheckCircle2,
  Info,
  Loader2,
  Trash2,
  RefreshCw,
  KeyRound,
  Rocket,
  Check,
  ChevronsUpDown,
} from 'lucide-react';
import { toast } from 'sonner';
import { apiClient } from '@/lib/api/client';
import type { SSOProfile, SpinnakerConfig, SpinnakerProject } from '@/lib/api/types';
import type { CredentialModalProps } from '@/types/profile';
import { cn } from '@/lib/utils';

// Combobox com busca embutida — mesmo padrão de SearchableSelect em KafkaTestTab.tsx (cada
// arquivo mantém sua própria cópia, não há versão compartilhada no projeto ainda).
function ProjectPicker({
  value,
  onChange,
  options,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  options: string[];
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="w-full justify-between font-normal"
        >
          <span className="truncate">{value || 'Selecione um projeto...'}</span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder="Buscar projeto..." />
          <CommandList>
            <CommandEmpty>Nenhum projeto encontrado</CommandEmpty>
            <CommandGroup>
              {options.map((opt) => (
                <CommandItem
                  key={opt}
                  value={opt}
                  onSelect={() => {
                    onChange(opt);
                    setOpen(false);
                  }}
                >
                  <Check className={cn('mr-2 h-4 w-4', value === opt ? 'opacity-100' : 'opacity-0')} />
                  {opt}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

// Entrada própria no menu Credenciais (mesmo padrão de AWXCredentialModal.tsx/
// DynatraceCredentialModal.tsx/ServiceNowSessionModal.tsx) — ver SPINNAKER-INTEGRATION-PLAN.md
// seção 10. Diferente do AWX, o login do Spinnaker sempre usa o Perfil SSO (POST /login direto
// com matrícula/email+senha, confirmado ao vivo — seção 3 do plano); não existe modo manual
// porque o backend (internal/spinnaker/client.go) não aceita usuário/senha avulsos.
export function SpinnakerCredentialModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const [config, setConfig] = useState<SpinnakerConfig | null>(null);
  const [ssoProfile, setSsoProfile] = useState<SSOProfile | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isTesting, setIsTesting] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  const [loginIdentifier, setLoginIdentifier] = useState<'email' | 'matricula'>('email');
  const [hlgUrl, setHlgUrl] = useState('');
  const [prdUrl, setPrdUrl] = useState('');
  const [selectedProject, setSelectedProject] = useState('');
  const [projectEnv, setProjectEnv] = useState<'hlg' | 'prd'>('hlg');
  const [projects, setProjects] = useState<SpinnakerProject[]>([]);

  useEffect(() => {
    if (open) {
      fetchConfig();
      fetchSSOProfile();
    }
  }, [open]);

  const fetchSSOProfile = async () => {
    try {
      const p = await apiClient.getSSOProfile();
      setSsoProfile(p);
    } catch {
      setSsoProfile({ configured: false });
    }
  };

  const fetchConfig = async () => {
    setIsLoading(true);
    try {
      const c = await apiClient.getSpinnakerConfig();
      setConfig(c);
      setLoginIdentifier(c.login_identifier || 'email');
      setHlgUrl(c.hlg_base_url || '');
      setPrdUrl(c.prd_base_url || '');
      setSelectedProject(c.selected_project || '');
    } catch {
      setConfig(null);
    } finally {
      setIsLoading(false);
    }
  };

  const buildConfig = (): SpinnakerConfig => ({
    login_identifier: loginIdentifier,
    hlg_base_url: hlgUrl.trim(),
    prd_base_url: prdUrl.trim(),
    selected_project: selectedProject,
  });

  const handleSave = async () => {
    if (!hlgUrl.trim() && !prdUrl.trim()) {
      toast.error('Informe ao menos uma URL (HLG ou PRD)');
      return;
    }
    if (!ssoProfile?.configured) {
      toast.error('Configure o Perfil SSO corporativo primeiro (Credenciais → Perfil SSO)');
      return;
    }

    setIsSaving(true);
    try {
      const saved = await apiClient.saveSpinnakerConfig(buildConfig());
      setConfig(saved);
      toast.success('Configuração do Spinnaker salva!');
      onSaved?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao salvar configuração');
    } finally {
      setIsSaving(false);
    }
  };

  // "Testar" busca os projetos reais — exige login bem-sucedido no Gate, então é o teste de
  // conectividade de fato (não existe um endpoint /test dedicado, ao contrário do AWX).
  const handleTest = async () => {
    const url = projectEnv === 'hlg' ? hlgUrl : prdUrl;
    if (!url.trim()) {
      toast.error(`Preencha a URL do Spinnaker (${projectEnv.toUpperCase()}) antes de testar`);
      return;
    }
    if (!ssoProfile?.configured) {
      toast.error('Configure o Perfil SSO corporativo primeiro');
      return;
    }

    setIsTesting(true);
    try {
      // Salva antes de testar — o backend lê a config do disco pra saber a URL/login a usar.
      await apiClient.saveSpinnakerConfig(buildConfig());
      const list = await apiClient.listSpinnakerProjects(projectEnv);
      setProjects(list);
      toast.success(`Conectado — ${list.length} projeto(s) encontrado(s) em ${projectEnv.toUpperCase()}`);
      onSaved?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Falha ao conectar no Spinnaker');
    } finally {
      setIsTesting(false);
    }
  };

  const handleDelete = async () => {
    if (!confirm('Remover a configuração do Spinnaker?')) return;
    setIsDeleting(true);
    try {
      await apiClient.saveSpinnakerConfig({});
      setConfig({});
      setHlgUrl('');
      setPrdUrl('');
      setSelectedProject('');
      setProjects([]);
      toast.success('Configuração do Spinnaker removida');
      onSaved?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao remover configuração');
    } finally {
      setIsDeleting(false);
    }
  };

  const isProcessing = isLoading || isSaving || isTesting || isDeleting;
  const isConfigured = !!config?.selected_project;

  const effectiveUsername = loginIdentifier === 'email' ? ssoProfile?.email : ssoProfile?.matricula;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[85vh] overflow-y-auto" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Rocket className="h-5 w-5 text-indigo-500" />
            Spinnaker
          </DialogTitle>
          <DialogDescription>
            Detecção de rollback de deployments (aba Deployments) via login direto no Gate com o
            Perfil SSO corporativo.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {isLoading ? (
            <Alert>
              <Loader2 className="h-4 w-4 animate-spin" />
              <AlertDescription>Verificando configuração...</AlertDescription>
            </Alert>
          ) : isConfigured ? (
            <Alert className="border-green-200 bg-green-50 dark:bg-green-950 dark:border-green-800">
              <CheckCircle2 className="h-4 w-4 text-green-600" />
              <AlertTitle className="text-green-900 dark:text-green-100">Configurado</AlertTitle>
              <AlertDescription className="text-green-800 dark:text-green-200 text-xs">
                Projeto: <strong>{config?.selected_project}</strong>
              </AlertDescription>
            </Alert>
          ) : (
            <Alert>
              <Info className="h-4 w-4" />
              <AlertTitle>Não configurado</AlertTitle>
              <AlertDescription className="text-sm">
                Preencha a URL, o login e escolha um projeto abaixo.
              </AlertDescription>
            </Alert>
          )}

          <Separator />

          {/* Login — sempre via Perfil SSO, sem modo manual (backend não aceita) */}
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <KeyRound className="h-4 w-4 text-blue-500" />
              <span className="text-sm font-medium">Login via Perfil SSO corporativo</span>
              {ssoProfile?.configured ? (
                <Badge className="bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 text-[10px] py-0">
                  Configurado
                </Badge>
              ) : (
                <Badge variant="destructive" className="text-[10px] py-0">Não configurado</Badge>
              )}
            </div>
            {!ssoProfile?.configured && (
              <Alert className="py-2">
                <Info className="h-3.5 w-3.5" />
                <AlertDescription className="text-xs">
                  Configure o <strong>Perfil SSO</strong> em Credenciais → Perfil SSO corporativo primeiro.
                </AlertDescription>
              </Alert>
            )}
            <div className="flex flex-col gap-1.5">
              {(['email', 'matricula'] as const).map((id) => (
                <label
                  key={id}
                  className={`flex items-center gap-1.5 text-sm cursor-pointer px-2 py-1 rounded border transition-colors ${
                    loginIdentifier === id
                      ? 'border-blue-500 bg-blue-100 dark:bg-blue-900/40'
                      : 'border-border hover:border-blue-300'
                  }`}
                  onClick={() => setLoginIdentifier(id)}
                >
                  <input
                    type="radio"
                    name="spinnaker-login-identifier"
                    value={id}
                    checked={loginIdentifier === id}
                    onChange={() => setLoginIdentifier(id)}
                    className="accent-blue-500"
                    disabled={isProcessing}
                  />
                  <span className="capitalize">{id === 'email' ? 'Email' : 'Matrícula'}</span>
                  {ssoProfile?.configured && (
                    <span className="text-xs text-muted-foreground">
                      ({id === 'email' ? (ssoProfile.email || '—') : (ssoProfile.matricula || '—')})
                    </span>
                  )}
                </label>
              ))}
            </div>
            {ssoProfile?.configured && (
              <p className="text-xs text-muted-foreground">
                Login efetivo: <strong>{effectiveUsername || '—'}</strong>
              </p>
            )}
          </div>

          <Separator />

          {/* URLs */}
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="spinnaker-hlg-url">URL Spinnaker (HLG)</Label>
              <Input
                id="spinnaker-hlg-url"
                value={hlgUrl}
                onChange={(e) => setHlgUrl(e.target.value)}
                placeholder="https://spinnaker-hlg.viavarejo.com.br/"
                disabled={isProcessing}
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="spinnaker-prd-url">URL Spinnaker (PRD)</Label>
              <Input
                id="spinnaker-prd-url"
                value={prdUrl}
                onChange={(e) => setPrdUrl(e.target.value)}
                placeholder="https://spinnaker-prd.viavarejo.com.br/"
                disabled={isProcessing}
              />
            </div>
            <p className="text-xs text-muted-foreground">
              A URL do Gate (API) é resolvida sozinha a partir do Deck (settings.js) — não precisa
              informar o domínio "-api" separadamente.
            </p>
          </div>

          <Separator />

          {/* Projeto */}
          <div className="space-y-1.5">
            <Label>Projeto padrão</Label>
            <div className="flex items-center gap-2">
              <div className="inline-flex rounded-md border border-border overflow-hidden shrink-0">
                <button
                  type="button"
                  onClick={() => setProjectEnv('hlg')}
                  disabled={isProcessing}
                  className={`px-2 py-1 text-xs font-medium ${
                    projectEnv === 'hlg' ? 'bg-primary text-white' : 'bg-background text-muted-foreground'
                  }`}
                >
                  HLG
                </button>
                <button
                  type="button"
                  onClick={() => setProjectEnv('prd')}
                  disabled={isProcessing}
                  className={`px-2 py-1 text-xs font-medium border-l border-border ${
                    projectEnv === 'prd' ? 'bg-primary text-white' : 'bg-background text-muted-foreground'
                  }`}
                >
                  PRD
                </button>
              </div>
              <span className="text-xs text-muted-foreground">
                Ambiente usado pelo botão "Testar" abaixo pra buscar a lista de projetos.
              </span>
            </div>
            <ProjectPicker
              value={selectedProject}
              onChange={setSelectedProject}
              options={projects.map((p) => p.name)}
              disabled={isProcessing}
            />
            {projects.length === 0 && (
              <p className="text-xs text-muted-foreground italic">
                Clique em "Testar" pra buscar a lista de projetos reais do Spinnaker.
              </p>
            )}
          </div>
        </div>

        <DialogFooter className="flex justify-between sm:justify-between">
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={fetchConfig} disabled={isProcessing}>
              <RefreshCw className={`h-4 w-4 mr-1 ${isLoading ? 'animate-spin' : ''}`} />
              Atualizar
            </Button>
            {isConfigured && (
              <Button
                variant="ghost"
                size="sm"
                onClick={handleDelete}
                disabled={isProcessing}
                className="text-destructive hover:text-destructive"
              >
                {isDeleting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
              </Button>
            )}
          </div>

          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={handleTest} disabled={isProcessing}>
              {isTesting && <Loader2 className="h-4 w-4 animate-spin mr-1" />}
              Testar
            </Button>
            <Button size="sm" onClick={handleSave} disabled={isProcessing}>
              {isSaving && <Loader2 className="h-4 w-4 animate-spin mr-1" />}
              Salvar
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
