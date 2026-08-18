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
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command';
import {
  CheckCircle2,
  XCircle,
  Info,
  Loader2,
  Trash2,
  RefreshCw,
  ShieldCheck,
  KeyRound,
  ChevronDown,
  Check,
  ChevronsUpDown,
  Rocket,
} from 'lucide-react';
import { toast } from 'sonner';
import { apiClient } from '@/lib/api/client';
import type { SSOProfile } from '@/lib/api/types';
import type { CredentialModalProps } from '@/types/profile';
import { useSpinnakerConfig, useSaveSpinnakerConfig, useSpinnakerProjects } from '@/hooks/useSpinnaker';
import { cn } from '@/lib/utils';

// Combobox com busca embutida — mesmo padrão de SearchableSelect em KafkaTestTab.tsx (cada
// arquivo mantém sua própria cópia, não há versão compartilhada no projeto ainda). Local aqui
// porque só o seletor de projeto Spinnaker usa.
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

export function SSOProfileModal({ open, onOpenChange, onSaved }: CredentialModalProps) {
  const [profile, setProfile] = useState<SSOProfile | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [isSaving, setIsSaving] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);

  const [email, setEmail] = useState('');
  const [matricula, setMatricula] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');

  // Integração Spinnaker (seção 10 do plano) — config separada (endpoint próprio), mas vive
  // no mesmo modal por ser, junto com email/matrícula acima, dado do Perfil do Usuário.
  const [spinnakerOpen, setSpinnakerOpen] = useState(false);
  const [spinnakerLoginId, setSpinnakerLoginId] = useState<'email' | 'matricula'>('email');
  const [spinnakerHlgUrl, setSpinnakerHlgUrl] = useState('');
  const [spinnakerPrdUrl, setSpinnakerPrdUrl] = useState('');
  const [spinnakerProject, setSpinnakerProject] = useState('');
  const [spinnakerProjectEnv, setSpinnakerProjectEnv] = useState<'hlg' | 'prd'>('hlg');
  const [fetchProjectsEnabled, setFetchProjectsEnabled] = useState(false);

  const spinnakerConfigQuery = useSpinnakerConfig();
  const saveSpinnakerConfig = useSaveSpinnakerConfig();
  const projectsQuery = useSpinnakerProjects(spinnakerProjectEnv, fetchProjectsEnabled);

  useEffect(() => {
    if (open) fetchProfile();
  }, [open]);

  // Preenche os campos do Spinnaker quando a config carrega (mesmo padrão do fetchProfile
  // acima, só que via React Query em vez de state manual).
  useEffect(() => {
    const cfg = spinnakerConfigQuery.data;
    if (!cfg) return;
    setSpinnakerLoginId(cfg.login_identifier || 'email');
    setSpinnakerHlgUrl(cfg.hlg_base_url || '');
    setSpinnakerPrdUrl(cfg.prd_base_url || '');
    setSpinnakerProject(cfg.selected_project || '');
  }, [spinnakerConfigQuery.data]);

  const fetchProfile = async () => {
    setIsLoading(true);
    try {
      const p = await apiClient.getSSOProfile();
      setProfile(p);
      if (p.configured) {
        setEmail(p.email ?? '');
        setMatricula(p.matricula ?? '');
      }
    } catch {
      setProfile({ configured: false });
    } finally {
      setIsLoading(false);
    }
  };

  const handleSave = async () => {
    if (!email && !matricula) {
      toast.error('Informe ao menos email ou matrícula');
      return;
    }
    if (!profile?.configured && !password) {
      toast.error('Senha é obrigatória na primeira configuração');
      return;
    }
    if (password && password !== confirmPassword) {
      toast.error('As senhas não coincidem');
      return;
    }

    setIsSaving(true);
    try {
      await apiClient.saveSSOProfile(email.trim(), matricula.trim(), password);
      toast.success('Perfil SSO salvo com sucesso!');
      setPassword('');
      setConfirmPassword('');
      onSaved?.();
      fetchProfile();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao salvar perfil SSO');
    } finally {
      setIsSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!confirm('Remover o perfil SSO? Serviços configurados para usar SSO deixarão de funcionar.')) return;
    setIsDeleting(true);
    try {
      await apiClient.deleteSSOProfile();
      setProfile({ configured: false });
      setEmail('');
      setMatricula('');
      setPassword('');
      setConfirmPassword('');
      toast.success('Perfil SSO removido');
      onSaved?.();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao remover perfil SSO');
    } finally {
      setIsDeleting(false);
    }
  };

  const handleSaveSpinnaker = async () => {
    try {
      await saveSpinnakerConfig.mutateAsync({
        login_identifier: spinnakerLoginId,
        hlg_base_url: spinnakerHlgUrl.trim(),
        prd_base_url: spinnakerPrdUrl.trim(),
        selected_project: spinnakerProject,
      });
      toast.success('Configuração do Spinnaker salva!');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Erro ao salvar configuração do Spinnaker');
    }
  };

  const handleFetchProjects = () => {
    if (!spinnakerHlgUrl && !spinnakerPrdUrl) {
      toast.error('Preencha e salve ao menos uma URL do Spinnaker antes de buscar projetos');
      return;
    }
    setFetchProjectsEnabled(true);
    projectsQuery.refetch();
  };

  const isProcessing = isLoading || isSaving || isDeleting;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md max-h-[85vh] overflow-y-auto" onInteractOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KeyRound className="h-5 w-5 text-blue-500" />
            Perfil SSO Corporativo
          </DialogTitle>
          <DialogDescription>
            Credenciais compartilhadas entre todos os serviços corporativos (AWX, ServiceNow, etc).
            Cada serviço usa email <strong>ou</strong> matrícula conforme configurado nele.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {/* Status */}
          {isLoading ? (
            <Alert>
              <Loader2 className="h-4 w-4 animate-spin" />
              <AlertDescription>Verificando perfil...</AlertDescription>
            </Alert>
          ) : profile?.configured ? (
            <Alert className="border-green-200 bg-green-50 dark:bg-green-950 dark:border-green-800">
              <CheckCircle2 className="h-4 w-4 text-green-600" />
              <AlertTitle className="text-green-900 dark:text-green-100">Perfil configurado</AlertTitle>
              <AlertDescription className="text-green-800 dark:text-green-200 text-xs">
                {profile.email && <span>Email: <strong>{profile.email}</strong></span>}
                {profile.email && profile.matricula && <span className="mx-2">·</span>}
                {profile.matricula && <span>Matrícula: <strong>{profile.matricula}</strong></span>}
                {profile.has_password && <span className="block mt-0.5 opacity-70">Senha: configurada</span>}
              </AlertDescription>
            </Alert>
          ) : (
            <Alert>
              <Info className="h-4 w-4" />
              <AlertTitle>Não configurado</AlertTitle>
              <AlertDescription className="text-sm">
                Preencha os campos abaixo para configurar suas credenciais corporativas.
              </AlertDescription>
            </Alert>
          )}

          <Separator />

          {/* Campos de identificação */}
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="sso-email">Email corporativo</Label>
              <Input
                id="sso-email"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="nome.sobrenome@empresa.com.br"
                disabled={isProcessing}
                autoComplete="email"
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="sso-matricula">
                Matrícula
                <span className="ml-1 text-xs text-muted-foreground">(número de funcionário)</span>
              </Label>
              <Input
                id="sso-matricula"
                value={matricula}
                onChange={(e) => setMatricula(e.target.value)}
                placeholder="ex: 123456"
                disabled={isProcessing}
              />
            </div>
          </div>

          <Separator />

          {/* Senha */}
          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="sso-password">
                Senha SSO
                {profile?.configured && (
                  <span className="ml-1 text-xs text-muted-foreground">(deixe em branco para manter)</span>
                )}
              </Label>
              <Input
                id="sso-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={profile?.configured ? '••••••••' : 'Senha corporativa'}
                disabled={isProcessing}
                autoComplete="current-password"
              />
            </div>

            {password && (
              <div className="space-y-1.5">
                <Label htmlFor="sso-confirm">Confirmar senha</Label>
                <Input
                  id="sso-confirm"
                  type="password"
                  value={confirmPassword}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  placeholder="Repita a senha"
                  disabled={isProcessing}
                  className={confirmPassword && confirmPassword !== password ? 'border-destructive' : ''}
                />
                {confirmPassword && confirmPassword !== password && (
                  <p className="text-xs text-destructive flex items-center gap-1">
                    <XCircle className="h-3 w-3" /> As senhas não coincidem
                  </p>
                )}
              </div>
            )}
          </div>

          {/* Info sobre uso */}
          <Alert>
            <ShieldCheck className="h-4 w-4" />
            <AlertDescription className="text-xs space-y-1">
              <p>A senha é armazenada com criptografia AES-256-GCM localmente.</p>
              <p>
                Serviços usam este perfil quando configurados com{' '}
                <Badge variant="outline" className="text-[10px] py-0">Usar Perfil SSO</Badge>.
                Cada serviço define se usa <strong>email</strong> ou <strong>matrícula</strong> como login.
              </p>
            </AlertDescription>
          </Alert>

          <Separator />

          {/* Integração Spinnaker (SPINNAKER-INTEGRATION-PLAN.md, seção 10) — colapsável,
              fechada por padrão, pra não sobrecarregar o modal de quem não usa o Spinnaker. */}
          <Collapsible open={spinnakerOpen} onOpenChange={setSpinnakerOpen} className="border rounded-md overflow-hidden">
            <CollapsibleTrigger
              className="w-full flex items-center justify-between px-3 py-2 text-sm font-medium hover:bg-muted/50"
              disabled={isProcessing}
            >
              <span className="flex items-center gap-2">
                <Rocket className="h-4 w-4 text-indigo-500" />
                Integração Spinnaker
              </span>
              <ChevronDown className={`h-4 w-4 transition-transform ${spinnakerOpen ? 'rotate-180' : ''}`} />
            </CollapsibleTrigger>
            <CollapsibleContent className="px-3 pb-3 space-y-3">
              <p className="text-xs text-muted-foreground">
                Usado pra detectar rollback de deployments na aba Deployments. Login reaproveita o
                email/matrícula acima — escolha qual campo usar.
              </p>

              <div className="space-y-1.5">
                <Label>Login via</Label>
                <div className="inline-flex rounded-md border border-border overflow-hidden">
                  <button
                    type="button"
                    onClick={() => setSpinnakerLoginId('email')}
                    disabled={isProcessing}
                    className={`px-3 py-1 text-xs font-medium ${
                      spinnakerLoginId === 'email' ? 'bg-primary text-white' : 'bg-background text-muted-foreground'
                    }`}
                  >
                    Email
                  </button>
                  <button
                    type="button"
                    onClick={() => setSpinnakerLoginId('matricula')}
                    disabled={isProcessing}
                    className={`px-3 py-1 text-xs font-medium border-l border-border ${
                      spinnakerLoginId === 'matricula' ? 'bg-primary text-white' : 'bg-background text-muted-foreground'
                    }`}
                  >
                    Matrícula
                  </button>
                </div>
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="spinnaker-hlg-url">URL Spinnaker (HLG)</Label>
                <Input
                  id="spinnaker-hlg-url"
                  value={spinnakerHlgUrl}
                  onChange={(e) => setSpinnakerHlgUrl(e.target.value)}
                  placeholder="https://spinnaker-hlg.viavarejo.com.br/"
                  disabled={isProcessing}
                />
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="spinnaker-prd-url">URL Spinnaker (PRD)</Label>
                <Input
                  id="spinnaker-prd-url"
                  value={spinnakerPrdUrl}
                  onChange={(e) => setSpinnakerPrdUrl(e.target.value)}
                  placeholder="https://spinnaker-prd.viavarejo.com.br/"
                  disabled={isProcessing}
                />
              </div>

              <Button size="sm" variant="outline" onClick={handleSaveSpinnaker} disabled={isProcessing || saveSpinnakerConfig.isPending}>
                {saveSpinnakerConfig.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1" />}
                Salvar login/URLs
              </Button>

              <Separator />

              <div className="space-y-1.5">
                <Label>Projeto padrão</Label>
                <div className="flex items-center gap-2">
                  <div className="inline-flex rounded-md border border-border overflow-hidden shrink-0">
                    <button
                      type="button"
                      onClick={() => setSpinnakerProjectEnv('hlg')}
                      disabled={isProcessing}
                      className={`px-2 py-1 text-xs font-medium ${
                        spinnakerProjectEnv === 'hlg' ? 'bg-primary text-white' : 'bg-background text-muted-foreground'
                      }`}
                    >
                      HLG
                    </button>
                    <button
                      type="button"
                      onClick={() => setSpinnakerProjectEnv('prd')}
                      disabled={isProcessing}
                      className={`px-2 py-1 text-xs font-medium border-l border-border ${
                        spinnakerProjectEnv === 'prd' ? 'bg-primary text-white' : 'bg-background text-muted-foreground'
                      }`}
                    >
                      PRD
                    </button>
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={handleFetchProjects}
                    disabled={isProcessing || projectsQuery.isFetching}
                    className="shrink-0"
                  >
                    {projectsQuery.isFetching ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <RefreshCw className="h-3.5 w-3.5" />
                    )}
                    <span className="ml-1">Buscar projetos</span>
                  </Button>
                </div>

                {projectsQuery.isError && (
                  <p className="text-xs text-destructive">
                    {projectsQuery.error instanceof Error ? projectsQuery.error.message : 'Falha ao buscar projetos'}
                  </p>
                )}

                <ProjectPicker
                  value={spinnakerProject}
                  onChange={(v) => setSpinnakerProject(v)}
                  options={(projectsQuery.data ?? []).map((p) => p.name)}
                  disabled={isProcessing || !projectsQuery.data?.length}
                />

                <Button
                  size="sm"
                  variant="outline"
                  onClick={handleSaveSpinnaker}
                  disabled={isProcessing || saveSpinnakerConfig.isPending || !spinnakerProject}
                >
                  {saveSpinnakerConfig.isPending && <Loader2 className="h-3.5 w-3.5 animate-spin mr-1" />}
                  Salvar projeto
                </Button>
              </div>
            </CollapsibleContent>
          </Collapsible>
        </div>

        <DialogFooter className="flex justify-between sm:justify-between">
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={fetchProfile} disabled={isProcessing}>
              <RefreshCw className={`h-4 w-4 mr-1 ${isLoading ? 'animate-spin' : ''}`} />
              Atualizar
            </Button>
            {profile?.configured && (
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

          <Button size="sm" onClick={handleSave} disabled={isProcessing}>
            {isSaving && <Loader2 className="h-4 w-4 animate-spin mr-1" />}
            Salvar
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
