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

  // Status do ServiceNow (sessao Playwright)
  const [serviceNowStatus, setServiceNowStatus] = useState<{
    valid: boolean;
    status: string;
  } | null>(null);
  const [serviceNowLoading, setServiceNowLoading] = useState(true);

  // Status do AWX
  const [awxStatus, setAwxStatus] = useState<{ configured: boolean; reachable: boolean } | null>(null);
  const [awxLoading, setAwxLoading] = useState(true);

  // Status do Dynatrace
  const [dynatraceStatus, setDynatraceStatus] = useState<{ enabled: boolean; has_token: boolean } | null>(null);
  const [dynatraceLoading, setDynatraceLoading] = useState(true);

  // Verificar status do Nexus, ServiceNow, AWX e Dynatrace ao montar
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

    const checkServiceNow = async () => {
      try {
        const status = await apiClient.get<{ valid: boolean; status: string }>('/servicenow/session-status');
        setServiceNowStatus(status);
      } catch {
        setServiceNowStatus(null);
      } finally {
        setServiceNowLoading(false);
      }
    };

    const checkAWX = async () => {
      try {
        const status = await apiClient.getAWXStatus();
        setAwxStatus(status);
      } catch {
        setAwxStatus(null);
      } finally {
        setAwxLoading(false);
      }
    };

    const checkDynatrace = async () => {
      try {
        const status = await apiClient.getDynatraceConfig();
        setDynatraceStatus(status);
      } catch {
        setDynatraceStatus(null);
      } finally {
        setDynatraceLoading(false);
      }
    };

    checkNexus();
    checkServiceNow();
    checkAWX();
    checkDynatrace();
  }, []);

  // Funcao para refresh manual do status
  const refreshCredentials = useCallback(async () => {
    setNexusLoading(true);
    setServiceNowLoading(true);
    setAwxLoading(true);
    setDynatraceLoading(true);

    try {
      const [nexus, servicenow, awx, dynatrace] = await Promise.allSettled([
        apiClient.get<NexusStatus>('/nexus/status'),
        apiClient.get<{ valid: boolean; status: string }>('/servicenow/session-status'),
        apiClient.getAWXStatus(),
        apiClient.getDynatraceConfig(),
      ]);

      if (nexus.status === 'fulfilled') {
        setNexusStatus(nexus.value);
      } else {
        setNexusStatus(null);
      }

      if (servicenow.status === 'fulfilled') {
        setServiceNowStatus(servicenow.value);
      } else {
        setServiceNowStatus(null);
      }

      if (awx.status === 'fulfilled') {
        setAwxStatus(awx.value);
      } else {
        setAwxStatus(null);
      }

      if (dynatrace.status === 'fulfilled') {
        setDynatraceStatus(dynatrace.value);
      } else {
        setDynatraceStatus(null);
      }
    } finally {
      setNexusLoading(false);
      setServiceNowLoading(false);
      setAwxLoading(false);
      setDynatraceLoading(false);
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

    // Determinar status do ServiceNow
    let serviceNowCredStatus: CredentialStatus = 'not_configured';
    if (serviceNowLoading) {
      serviceNowCredStatus = 'validating';
    } else if (serviceNowStatus?.valid) {
      serviceNowCredStatus = 'configured';
    } else if (serviceNowStatus?.status === 'expired') {
      serviceNowCredStatus = 'error'; // Sessao expirada
    }

    // Determinar status do AWX
    let awxCredStatus: CredentialStatus = 'not_configured';
    if (awxLoading) {
      awxCredStatus = 'validating';
    } else if (awxStatus?.configured && awxStatus?.reachable) {
      awxCredStatus = 'configured';
    } else if (awxStatus?.configured && !awxStatus?.reachable) {
      awxCredStatus = 'error';
    }

    // Determinar status do Dynatrace
    let dynatraceCredStatus: CredentialStatus = 'not_configured';
    if (dynatraceLoading) {
      dynatraceCredStatus = 'validating';
    } else if (dynatraceStatus?.enabled) {
      dynatraceCredStatus = 'configured';
    } else if (dynatraceStatus?.has_token) {
      // Token salvo mas URL ausente (ou vice-versa) — configuração incompleta.
      dynatraceCredStatus = 'error';
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
      servicenow: {
        id: 'servicenow',
        name: 'ServiceNow',
        description: 'Sessao Azure AD para extracao de CHGs',
        status: serviceNowCredStatus,
        lastChecked: new Date(),
      },
      awx: {
        id: 'awx',
        name: 'AWX / Ansible Tower',
        description: 'Credenciais SSO para gerenciamento de certificados TLS',
        status: awxCredStatus,
        lastChecked: new Date(),
      },
      dynatrace: {
        id: 'dynatrace',
        name: 'Dynatrace',
        description: 'Token individual para análise de problems com AI',
        status: dynatraceCredStatus,
        lastChecked: new Date(),
      },
    };
  }, [githubStatus, nexusStatus, nexusLoading, serviceNowStatus, serviceNowLoading, awxStatus, awxLoading, dynatraceStatus, dynatraceLoading]);

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
