import { useState } from 'react';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
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
} from 'lucide-react';
import { useTheme } from '@/components/theme-provider';
import { useUserProfile } from '@/hooks/useUserProfile';
import { NexusCredentialModal } from '@/components/profile/NexusCredentialModal';
import { GitHubCredentialModal } from '@/components/profile/GitHubCredentialModal';
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
      <DropdownMenu>
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
              <span className="flex-1">GitHub Token</span>
              {renderStatusIcon(credentials.github.status)}
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
    </>
  );
}
