import { useState, useCallback } from 'react';
import type { ScanRequest, ScanResult, CertificateInfo, CopyRequest, UploadRequest, ChainValidationResult, RollbackBackupInfo, ManualBackupInfo, PFXExtractInfo, BackendTLSCheckResult } from '../types/certificates';

const API_BASE = '/api/v1/certificates';

export function useCertificates() {
  const [scanResult, setScanResult] = useState<ScanResult | null>(null);
  const [scanning, setScanning] = useState(false);
  const [scanError, setScanError] = useState<string | null>(null);

  const scanCertificates = useCallback(async (req: ScanRequest) => {
    setScanning(true);
    setScanError(null);
    try {
      const response = await fetch(`${API_BASE}/scan`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
        },
        body: JSON.stringify(req),
      });

      const data = await response.json();
      if (!response.ok || !data.success) {
        throw new Error(data.error?.message || `HTTP ${response.status}`);
      }

      setScanResult(data.data);
      return data.data as ScanResult;
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Erro desconhecido';
      setScanError(msg);
      throw err;
    } finally {
      setScanning(false);
    }
  }, []);

  const getCertificateDetails = useCallback(async (cluster: string, namespace: string, name: string): Promise<CertificateInfo> => {
    const response = await fetch(`${API_BASE}/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.data;
  }, []);

  const copyCertificate = useCallback(async (req: CopyRequest): Promise<number> => {
    const response = await fetch(`${API_BASE}/copy`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
      body: JSON.stringify(req),
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.copied;
  }, []);

  const uploadCertificate = useCallback(async (req: UploadRequest): Promise<number> => {
    const response = await fetch(`${API_BASE}/upload`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
      body: JSON.stringify(req),
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.uploaded;
  }, []);

  // uploadCertificateWithValidation — mesma chamada de uploadCertificate, mas devolve também o
  // resultado de validação de cadeia que o backend já calcula automaticamente pós-instalação
  // (campo "validation" da resposta de /upload). uploadCertificate (acima) continua devolvendo só
  // o número, sem mudar assinatura, pra não quebrar os call sites existentes em CertificatesTab.tsx.
  const uploadCertificateWithValidation = useCallback(async (req: UploadRequest): Promise<{ uploaded: number; validation: ChainValidationResult | null }> => {
    const response = await fetch(`${API_BASE}/upload`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
      body: JSON.stringify(req),
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return { uploaded: data.uploaded, validation: data.validation ?? null };
  }, []);

  // validateChainPEM — validação ad-hoc de um par cert+key (ex: antes de instalar).
  const validateChainPEM = useCallback(async (certPem: string, keyPem: string): Promise<ChainValidationResult> => {
    const response = await fetch(`${API_BASE}/validate-chain`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
      body: JSON.stringify({ cert_pem: certPem, key_pem: keyPem }),
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.data as ChainValidationResult;
  }, []);

  // validateInstalledChain — valida a cadeia de um certificado já instalado no cluster (disparo
  // manual, ex: botão "Validar Cadeia" no detalhe de um cert existente).
  const validateInstalledChain = useCallback(async (cluster: string, namespace: string, name: string): Promise<ChainValidationResult> => {
    const response = await fetch(`${API_BASE}/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/validate-chain`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.data as ChainValidationResult;
  }, []);

  // checkBackendTLS — "Diagnóstico Avançado" (Fase 8), disparo manual e explícito (mais custoso
  // que validateInstalledChain — lê logs de todos os pods do ingress-controller). Nunca confirma
  // "sem erro": Checked=true + signals vazio só significa que nenhum sinal foi encontrado na
  // janela de log analisada.
  const checkBackendTLS = useCallback(async (cluster: string, namespace: string, name: string): Promise<BackendTLSCheckResult> => {
    const response = await fetch(`${API_BASE}/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/backend-tls-check`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.data as BackendTLSCheckResult;
  }, []);

  // backupCertificate — salva o conteúdo ATUAL de um Secret TLS já instalado, sem sobrescrever
  // nada. Usado pelo caminho AWX (Fase 2): a renovação via playbook Ansible acontece fora do
  // controle deste backend, então o backup precisa ser disparado explicitamente antes do job
  // rodar (upload manual já dispara o backup sozinho, dentro do próprio endpoint /upload).
  const backupCertificate = useCallback(async (cluster: string, namespace: string, name: string): Promise<RollbackBackupInfo> => {
    const response = await fetch(`${API_BASE}/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/backup`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.data as RollbackBackupInfo;
  }, []);

  // listRollbacks — lista os backups disponíveis para um Secret, mais recente primeiro.
  const listRollbacks = useCallback(async (cluster: string, namespace: string, name: string): Promise<RollbackBackupInfo[]> => {
    const response = await fetch(`${API_BASE}/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/rollback`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return (data.data ?? []) as RollbackBackupInfo[];
  }, []);

  // rollbackCertificate — restaura um backup por cima do Secret atual.
  const rollbackCertificate = useCallback(async (cluster: string, namespace: string, name: string, backupId: string): Promise<{ validation: ChainValidationResult | null }> => {
    const response = await fetch(`${API_BASE}/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/rollback`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
      body: JSON.stringify({ backup_id: backupId }),
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return { validation: data.validation ?? null };
  }, []);

  // getRollbackContent — PEM bruto de um backup do RollbackStore, pra pré-popular os campos de
  // texto do fluxo manual sem precisar copiar/colar (parte do picker de fonte, ver
  // CertificateSourcePickerModal.tsx).
  const getRollbackContent = useCallback(async (cluster: string, namespace: string, name: string, backupId: string): Promise<{ tls_crt: string; tls_key: string }> => {
    const response = await fetch(`${API_BASE}/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/rollback/${encodeURIComponent(backupId)}/content`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.data as { tls_crt: string; tls_key: string };
  }, []);

  // saveManualBackup — salva o conteúdo ATUAL de um Secret TLS num backup separado do Rollback,
  // disparado sob demanda pelo usuário (botão "Copiar para Backup"), com comentário opcional.
  const saveManualBackup = useCallback(async (cluster: string, namespace: string, name: string, comment?: string): Promise<ManualBackupInfo> => {
    const response = await fetch(`${API_BASE}/${encodeURIComponent(cluster)}/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/manual-backup`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
      body: JSON.stringify({ comment: comment || undefined }),
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.data as ManualBackupInfo;
  }, []);

  // listManualBackupSecrets — nomes de secret que têm ao menos 1 backup manual salvo, pra
  // navegação "entre pastas" do picker de instalação (não fica restrita ao secret atual).
  const listManualBackupSecrets = useCallback(async (): Promise<string[]> => {
    const response = await fetch(`${API_BASE}/manual-backups`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return (data.data ?? []) as string[];
  }, []);

  // listManualBackups — backups manuais de um secret específico, mais recente primeiro.
  const listManualBackups = useCallback(async (secretName: string): Promise<ManualBackupInfo[]> => {
    const response = await fetch(`${API_BASE}/manual-backups/${encodeURIComponent(secretName)}`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return (data.data ?? []) as ManualBackupInfo[];
  }, []);

  // getManualBackupContent — PEM bruto de um backup manual específico.
  const getManualBackupContent = useCallback(async (secretName: string, backupId: string): Promise<{ tls_crt: string; tls_key: string }> => {
    const response = await fetch(`${API_BASE}/manual-backups/${encodeURIComponent(secretName)}/${encodeURIComponent(backupId)}/content`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.data as { tls_crt: string; tls_key: string };
  }, []);

  // updateManualBackupComment — edita só o comentário de um backup já salvo (não afeta o PEM).
  const updateManualBackupComment = useCallback(async (secretName: string, backupId: string, comment: string): Promise<void> => {
    const response = await fetch(`${API_BASE}/manual-backups/${encodeURIComponent(secretName)}/${encodeURIComponent(backupId)}/comment`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
      body: JSON.stringify({ comment }),
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }
  }, []);

  // deleteManualBackup — remove por completo um backup manual (tls.crt/tls.key/metadata.json).
  // Ação destrutiva e irreversível — o chamador (CertificateSourcePickerModal.tsx) sempre confirma
  // antes de invocar.
  const deleteManualBackup = useCallback(async (secretName: string, backupId: string): Promise<void> => {
    const response = await fetch(`${API_BASE}/manual-backups/${encodeURIComponent(secretName)}/${encodeURIComponent(backupId)}`, {
      method: 'DELETE',
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }
  }, []);

  // extractPFX — decodifica um .pfx/.p12 no backend e salva o resultado (tls.crt/tls.key) em
  // PFXExtractStore (ver PFX-CERT-EXTRACTION-PLAN.md). multipart/form-data (não JSON) porque
  // carrega um arquivo binário; a senha vai junto no mesmo form, nunca fica em localStorage/state
  // além do tempo de vida do formulário. Devolve os PEMs completos junto com a metadata, pra já
  // poder popular tls.crt/tls.key sem precisar de um segundo round-trip pelo picker.
  const extractPFX = useCallback(async (file: File, password: string, name: string, comment?: string): Promise<{ info: PFXExtractInfo; tls_crt: string; tls_key: string }> => {
    const form = new FormData();
    form.append('file', file);
    form.append('password', password);
    form.append('name', name);
    if (comment) form.append('comment', comment);

    const response = await fetch(`${API_BASE}/pfx/extract`, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
      body: form,
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.data as { info: PFXExtractInfo; tls_crt: string; tls_key: string };
  }, []);

  // listPFXNames — nomes que têm ao menos 1 extração de .pfx salva, pra navegação "entre pastas"
  // do picker de instalação — mesmo papel de listManualBackupSecrets.
  const listPFXNames = useCallback(async (): Promise<string[]> => {
    const response = await fetch(`${API_BASE}/pfx/names`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return (data.data ?? []) as string[];
  }, []);

  // listPFXExtracts — extrações de um nome específico, mais recente primeiro.
  const listPFXExtracts = useCallback(async (name: string): Promise<PFXExtractInfo[]> => {
    const response = await fetch(`${API_BASE}/pfx/${encodeURIComponent(name)}`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return (data.data ?? []) as PFXExtractInfo[];
  }, []);

  // getPFXContent — PEM bruto (tls.crt/tls.key) de uma extração específica.
  const getPFXContent = useCallback(async (name: string, extractId: string): Promise<{ tls_crt: string; tls_key: string }> => {
    const response = await fetch(`${API_BASE}/pfx/${encodeURIComponent(name)}/${encodeURIComponent(extractId)}/content`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return data.data as { tls_crt: string; tls_key: string };
  }, []);

  // updatePFXComment — edita só o comentário de uma extração já salva (não afeta o PEM).
  const updatePFXComment = useCallback(async (name: string, extractId: string, comment: string): Promise<void> => {
    const response = await fetch(`${API_BASE}/pfx/${encodeURIComponent(name)}/${encodeURIComponent(extractId)}/comment`, {
      method: 'PUT',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
      body: JSON.stringify({ comment }),
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }
  }, []);

  // deletePFX — remove por completo uma extração (tls.crt/tls.key/metadata.json). Ação destrutiva
  // e irreversível — o chamador (CertificateSourcePickerModal.tsx) sempre confirma antes de invocar.
  const deletePFX = useCallback(async (name: string, extractId: string): Promise<void> => {
    const response = await fetch(`${API_BASE}/pfx/${encodeURIComponent(name)}/${encodeURIComponent(extractId)}`, {
      method: 'DELETE',
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }
  }, []);

  const getReport = useCallback(async (clusters: string[], filter?: string, statusFilter?: string[]): Promise<{ data: ScanResult; markdown: string }> => {
    const params = new URLSearchParams();
    params.set('clusters', clusters.join(','));
    if (filter) params.set('filter', filter);
    if (statusFilter) {
      statusFilter.forEach(s => params.append('status', s));
    }

    const response = await fetch(`${API_BASE}/report?${params}`, {
      headers: {
        Authorization: `Bearer ${localStorage.getItem('auth_token')}`,
      },
    });

    const data = await response.json();
    if (!response.ok || !data.success) {
      throw new Error(data.error?.message || `HTTP ${response.status}`);
    }

    return { data: data.data, markdown: data.markdown };
  }, []);

  return {
    scanResult,
    scanning,
    scanError,
    scanCertificates,
    getCertificateDetails,
    copyCertificate,
    uploadCertificate,
    uploadCertificateWithValidation,
    validateChainPEM,
    validateInstalledChain,
    checkBackendTLS,
    backupCertificate,
    listRollbacks,
    rollbackCertificate,
    getRollbackContent,
    saveManualBackup,
    listManualBackupSecrets,
    listManualBackups,
    getManualBackupContent,
    updateManualBackupComment,
    deleteManualBackup,
    extractPFX,
    listPFXNames,
    listPFXExtracts,
    getPFXContent,
    updatePFXComment,
    deletePFX,
    getReport,
    setScanResult,
  };
}
