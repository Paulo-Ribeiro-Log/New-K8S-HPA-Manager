// Tipos para o Menu de Perfil e Configuracoes

export interface UserProfile {
  email: string;
  displayName: string;
  initials: string;
  isSRE: boolean;
  groups: ADGroup[];
}

export interface ADGroup {
  id: string;
  displayName: string;
}

// Status de uma credencial/integracao
export type CredentialStatus = 'configured' | 'not_configured' | 'error' | 'validating';

export interface CredentialInfo {
  id: string;
  name: string;
  description: string;
  status: CredentialStatus;
  lastChecked?: Date;
  errorMessage?: string;
}

// Credenciais disponiveis
export interface CredentialsState {
  nexus: CredentialInfo;
  github: CredentialInfo;
  servicenow?: CredentialInfo;
}

// Tema da aplicacao
export type ThemeMode = 'light' | 'dark' | 'system';

// Estado do perfil (agregado)
export interface ProfileState {
  user: UserProfile | null;
  isLoading: boolean;
  credentials: CredentialsState;
  theme: ThemeMode;
}

// Props comuns para modais de credenciais
export interface CredentialModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved?: () => void;
}
