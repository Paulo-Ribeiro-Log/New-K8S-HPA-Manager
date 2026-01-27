import { useMemo, useState, useEffect, useCallback } from 'react';
import { useUserPermissions } from './useUserPermissions';
import { useGitHubTokenStatus } from './useGitHubReleases';
import { apiClient } from '@/lib/api/client';
import type { UserProfile, CredentialsState, CredentialStatus } from '@/types/profile';
import type { NexusStatus } from '@/types/nexus';

/**
 * Hook agregador que combina dados do usuario e status de credenciais
 * Usado pelo UserProfileMenu para exibir informacoes consolidadas
 */
export function useUserProfile() {
  // Dados do usuario (Azure AD)
  const {
    data: permissions,
    isLoading: permissionsLoading,
    error: permissionsError,
  } = useUserPermissions();

  // Status do Nexus - verificacao direta
  const [nexusStatus, setNexusStatus] = useState<NexusStatus | null>(null);
  const [nexusLoading, setNexusLoading] = useState(true);

  // Status do GitHub
  const {
    data: githubStatus,
    isLoading: githubLoading,
  } = useGitHubTokenStatus();

  // Verificar status do Nexus ao montar
  useEffect(() => {
    const checkNexus = async () => {
      try {
        const status = await apiClient.get<NexusStatus>('/nexus/status');
        setNexusStatus(status);
      } catch {
        setNexusStatus(null);
      } finally {
        setNexusLoading(false);
      }
    };
    checkNexus();
  }, []);

  // Funcao para refresh manual do status
  const refreshCredentials = useCallback(async () => {
    setNexusLoading(true);
    try {
      const status = await apiClient.get<NexusStatus>('/nexus/status');
      setNexusStatus(status);
    } catch {
      setNexusStatus(null);
    } finally {
      setNexusLoading(false);
    }
  }, []);

  // Construir perfil do usuario
  const user = useMemo<UserProfile | null>(() => {
    if (!permissions) return null;

    const email = permissions.email || 'usuario@desconhecido';
    const displayName = extractDisplayName(email);
    const initials = extractInitials(displayName);

    return {
      email,
      displayName,
      initials,
      isSRE: permissions.isSRE,
      groups: permissions.groups || [],
    };
  }, [permissions]);

  // Construir estado das credenciais
  const credentials = useMemo<CredentialsState>(() => {
    // Determinar status do Nexus
    let nexusCredStatus: CredentialStatus = 'not_configured';
    if (nexusLoading) {
      nexusCredStatus = 'validating';
    } else if (nexusStatus?.configured) {
      nexusCredStatus = 'configured';
    }

    return {
      nexus: {
        id: 'nexus',
        name: 'Nexus Repository',
        description: 'Gerenciador de artefatos e values.yaml',
        status: nexusCredStatus,
        lastChecked: new Date(),
      },
      github: {
        id: 'github',
        name: 'GitHub',
        description: 'Token para repositorios privados',
        status: githubStatus?.valid ? 'configured' : 'not_configured',
        lastChecked: new Date(),
      },
    };
  }, [githubStatus, nexusStatus, nexusLoading]);

  const isLoading = permissionsLoading || githubLoading;

  return {
    user,
    isLoading,
    error: permissionsError,
    credentials,
    // Refresh de credenciais
    refreshCredentials,
  };
}

/**
 * Extrai nome de exibicao do email
 * admin@k8s.local -> Admin
 * paulo.ribeiro@empresa.com -> Paulo Ribeiro
 */
function extractDisplayName(email: string): string {
  const localPart = email.split('@')[0];

  // Se tem ponto, assumir formato nome.sobrenome
  if (localPart.includes('.')) {
    return localPart
      .split('.')
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1).toLowerCase())
      .join(' ');
  }

  // Senao, capitalizar primeira letra
  return localPart.charAt(0).toUpperCase() + localPart.slice(1).toLowerCase();
}

/**
 * Extrai iniciais do nome
 * Paulo Ribeiro -> PR
 * Admin -> AD
 */
function extractInitials(displayName: string): string {
  const parts = displayName.split(' ').filter(Boolean);

  if (parts.length >= 2) {
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
  }

  return displayName.substring(0, 2).toUpperCase();
}

/**
 * Hook para verificar status de uma credencial especifica
 */
export function useCredentialStatus(credentialId: 'nexus' | 'github') {
  const { credentials, isLoading } = useUserProfile();

  return {
    ...credentials[credentialId],
    isLoading,
  };
}
