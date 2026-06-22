import { useState } from 'react';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { Badge } from '@/components/ui/badge';
import {
  ChevronDown,
  LogOut,
  Sun,
  Moon,
  Monitor,
  Settings,
  CheckCircle2,
  XCircle,
  Loader2,
  KeyRound,
  Key,
} from 'lucide-react';

// ── Perfis do Editor de Código ──────────────────────────────────────────────
interface GitHubProfile { id: string; name: string; token: string }
const CE_PROFILES_KEY = 'ce_github_profiles';
function ceLoadProfiles(): GitHubProfile[] {
  try { return JSON.parse(localStorage.getItem(CE_PROFILES_KEY) || '[]'); } catch { return []; }
}
function ceGetActiveId(): string {
  return localStorage.getItem('ce_default_profile') ?? '';
}
function ceSetActiveId(id: string) {
  if (id) localStorage.setItem('ce_default_profile', id);
  else localStorage.removeItem('ce_default_profile');
}
import { useTheme } from '@/components/theme-provider';
import { useUserProfile } from '@/hooks/useUserProfile';
import { apiClient } from '@/lib/api/client';
import { NexusCredentialModal } from '@/components/profile/NexusCredentialModal';
import { GitHubCredentialModal } from '@/components/profile/GitHubCredentialModal';
import { ServiceNowSessionModal } from '@/components/profile/ServiceNowSessionModal';
import { AWXCredentialModal } from '@/components/profile/AWXCredentialModal';
import { SSOProfileModal } from '@/components/profile/SSOProfileModal';
import type { CredentialStatus } from '@/types/profile';

interface UserProfileMenuProps {
  onLogout: () => void;
}

export function UserProfileMenu({ onLogout }: UserProfileMenuProps) {
  const { user, isLoading, credentials, refreshCredentials } = useUserProfile();
  const { theme, setTheme } = useTheme();

  // Estados dos modais de credenciais
  const [nexusModalOpen, setNexusModalOpen] = useState(false);
  const [githubModalOpen, setGithubModalOpen] = useState(false);
  const [serviceNowModalOpen, setServiceNowModalOpen] = useState(false);
  const [awxModalOpen, setAwxModalOpen] = useState(false);
  const [ssoProfileModalOpen, setSsoProfileModalOpen] = useState(false);

  // Contas do Editor de Código
  const [ceProfiles, setCeProfiles] = useState<GitHubProfile[]>([]);
  const [ceActiveId, setCeActiveId] = useState<string>('');

  async function onMenuOpen(open: boolean) {
    if (!open) return;
    try {
      const r = await apiClient.codeEditorGetGitHubProfiles();
      setCeProfiles(r.profiles);
      const active = r.profiles.find(p => p.active);
      setCeActiveId(active?.id ?? '');
      // Atualiza cache localStorage
      localStorage.setItem('ce_github_profiles', JSON.stringify(r.profiles));
      if (active) localStorage.setItem('ce_default_profile', active.id);
    } catch {
      // Fallback para localStorage se API não responder
      setCeProfiles(ceLoadProfiles());
      setCeActiveId(ceGetActiveId());
    }
  }

  async function handleCeSwitch(id: string) {
    // Marca o perfil selecionado como active=true no servidor
    const updated = ceProfiles.map(p => ({ ...p, active: p.id === id }));
    try {
      await apiClient.codeEditorSaveGitHubProfiles(updated);
    } catch { /* silencioso */ }
    localStorage.setItem('ce_github_profiles', JSON.stringify(updated));
    ceSetActiveId(id);
    setCeActiveId(id);
    setCeProfiles(updated);
  }

  // Renderizar icone de status da credencial
  const renderStatusIcon = (status: CredentialStatus) => {
    switch (status) {
      case 'configured':
        return <CheckCircle2 className="h-4 w-4 text-green-500" />;
      case 'error':
        return <XCircle className="h-4 w-4 text-red-500" />;
      case 'validating':
        return <Loader2 className="h-4 w-4 text-yellow-500 animate-spin" />;
      default:
        return <XCircle className="h-4 w-4 text-muted-foreground" />;
    }
  };

  // Renderizar icone do tema atual
  const renderThemeIcon = () => {
    switch (theme) {
      case 'light':
        return <Sun className="h-4 w-4" />;
      case 'dark':
        return <Moon className="h-4 w-4" />;
      default:
        return <Monitor className="h-4 w-4" />;
    }
  };

  if (isLoading) {
    return (
      <Button variant="ghost" disabled className="text-white/70">
        <Loader2 className="h-4 w-4 animate-spin mr-2" />
        Carregando...
      </Button>
    );
  }

  return (
    <>
      <DropdownMenu onOpenChange={onMenuOpen}>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            className="flex items-center gap-2 text-white hover:bg-white/20 hover:text-white px-2 md:px-3"
          >
            <Avatar className="h-8 w-8 border border-white/30">
              <AvatarFallback className="bg-white/20 text-white text-sm">
                {user?.initials || '??'}
              </AvatarFallback>
            </Avatar>
            {/* Nome e email - ocultos em telas pequenas */}
            <div className="hidden md:flex flex-col items-start">
              <span className="text-sm font-medium">{user?.displayName || 'Usuario'}</span>
              <span className="text-xs text-white/60">{user?.email || ''}</span>
            </div>
            {user?.isSRE && (
              <Badge variant="secondary" className="ml-1 bg-green-500/20 text-green-300 border-green-500/30 text-xs">
                SRE
              </Badge>
            )}
            <ChevronDown className="h-4 w-4 ml-1 opacity-70" />
          </Button>
        </DropdownMenuTrigger>

        <DropdownMenuContent className="w-72" align="end" sideOffset={8}>
          {/* Header do Usuario */}
          <DropdownMenuLabel className="font-normal">
            <div className="flex items-center gap-3 py-1">
              <Avatar className="h-10 w-10">
                <AvatarFallback className="bg-primary text-primary-foreground">
                  {user?.initials || '??'}
                </AvatarFallback>
              </Avatar>
              <div className="flex flex-col">
                <span className="font-semibold">{user?.displayName || 'Usuario'}</span>
                <span className="text-xs text-muted-foreground">{user?.email}</span>
                {user?.isSRE && (
                  <Badge variant="outline" className="w-fit mt-1 text-xs">
                    SRE Team
                  </Badge>
                )}
              </div>
            </div>
          </DropdownMenuLabel>

          <DropdownMenuSeparator />

          {/* Secao de Tema */}
          <DropdownMenuGroup>
            <DropdownMenuLabel className="text-xs text-muted-foreground flex items-center gap-2">
              {renderThemeIcon()}
              Tema
            </DropdownMenuLabel>
            <DropdownMenuItem
              onClick={() => setTheme('light')}
              className="cursor-pointer"
            >
              <Sun className="h-4 w-4 mr-2" />
              <span>Claro</span>
              {theme === 'light' && <CheckCircle2 className="h-4 w-4 ml-auto text-green-500" />}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => setTheme('dark')}
              className="cursor-pointer"
            >
              <Moon className="h-4 w-4 mr-2" />
              <span>Escuro</span>
              {theme === 'dark' && <CheckCircle2 className="h-4 w-4 ml-auto text-green-500" />}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => setTheme('system')}
              className="cursor-pointer"
            >
              <Monitor className="h-4 w-4 mr-2" />
              <span>Sistema</span>
              {theme === 'system' && <CheckCircle2 className="h-4 w-4 ml-auto text-green-500" />}
            </DropdownMenuItem>
          </DropdownMenuGroup>

          <DropdownMenuSeparator />

          {/* Secao de Credenciais */}
          <DropdownMenuGroup>
            <DropdownMenuLabel className="text-xs text-muted-foreground flex items-center gap-2">
              <Settings className="h-4 w-4" />
              Credenciais
            </DropdownMenuLabel>
            <DropdownMenuItem
              onClick={() => setSsoProfileModalOpen(true)}
              className="cursor-pointer"
            >
              <KeyRound className="h-4 w-4 mr-2 text-blue-500" />
              <span className="flex-1">Perfil SSO corporativo</span>
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => setNexusModalOpen(true)}
              className="cursor-pointer"
            >
              <span className="flex-1">Nexus Repository</span>
              {renderStatusIcon(credentials.nexus.status)}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => setGithubModalOpen(true)}
              className="cursor-pointer"
            >
              <span className="flex-1">GitHub Token (SSO/SAML)</span>
              {renderStatusIcon(credentials.github.status)}
            </DropdownMenuItem>

            {/* Contas do Editor de Código */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger className="cursor-pointer">
                <Key className="h-4 w-4 mr-2 text-amber-500" />
                <span className="flex-1">Contas do Editor de Código</span>
                {ceActiveId && ceProfiles.find(p => p.id === ceActiveId) && (
                  <span className="text-xs text-muted-foreground ml-2 max-w-20 truncate">
                    {ceProfiles.find(p => p.id === ceActiveId)?.name}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-56">
                <DropdownMenuLabel className="text-xs text-muted-foreground pb-1">
                  Conta usada para push/pull no editor
                </DropdownMenuLabel>
                <DropdownMenuRadioGroup value={ceActiveId} onValueChange={handleCeSwitch}>
                  <DropdownMenuRadioItem value="" className="cursor-pointer text-sm">
                    Sem conta (credential helper)
                  </DropdownMenuRadioItem>
                  {ceProfiles.map(p => (
                    <DropdownMenuRadioItem key={p.id} value={p.id} className="cursor-pointer">
                      <div className="flex flex-col min-w-0">
                        <span className="text-sm font-medium truncate">{p.name}</span>
                        <span className="text-[10px] text-muted-foreground font-mono">
                          {p.token.slice(0, 8)}••••{p.token.slice(-4)}
                        </span>
                      </div>
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
                {ceProfiles.length === 0 && (
                  <div className="px-3 py-1.5 text-xs text-muted-foreground italic">
                    Nenhuma conta configurada ainda.
                  </div>
                )}
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  className="cursor-pointer text-xs"
                  onClick={() => setGithubModalOpen(true)}
                >
                  <KeyRound className="h-3.5 w-3.5 mr-2" />
                  Gerenciar contas...
                </DropdownMenuItem>
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            <DropdownMenuItem
              onClick={() => setServiceNowModalOpen(true)}
              className="cursor-pointer"
            >
              <span className="flex-1">ServiceNow Session</span>
              {renderStatusIcon(credentials.servicenow?.status || 'not_configured')}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => setAwxModalOpen(true)}
              className="cursor-pointer"
            >
              <span className="flex-1">AWX / Ansible Tower</span>
              {renderStatusIcon(credentials.awx?.status || 'not_configured')}
            </DropdownMenuItem>
          </DropdownMenuGroup>

          <DropdownMenuSeparator />

          {/* Logout */}
          <DropdownMenuItem
            onClick={onLogout}
            className="cursor-pointer text-red-600 focus:text-red-600 focus:bg-red-50 dark:focus:bg-red-950"
          >
            <LogOut className="h-4 w-4 mr-2" />
            <span>Sair</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Modais de Credenciais */}
      <NexusCredentialModal
        open={nexusModalOpen}
        onOpenChange={setNexusModalOpen}
        onSaved={refreshCredentials}
      />
      <GitHubCredentialModal
        open={githubModalOpen}
        onOpenChange={setGithubModalOpen}
        onSaved={refreshCredentials}
      />
      <ServiceNowSessionModal
        open={serviceNowModalOpen}
        onOpenChange={setServiceNowModalOpen}
        onSaved={refreshCredentials}
      />
      <AWXCredentialModal
        open={awxModalOpen}
        onOpenChange={setAwxModalOpen}
        onSaved={refreshCredentials}
      />
      <SSOProfileModal
        open={ssoProfileModalOpen}
        onOpenChange={setSsoProfileModalOpen}
        onSaved={refreshCredentials}
      />
    </>
  );
}
