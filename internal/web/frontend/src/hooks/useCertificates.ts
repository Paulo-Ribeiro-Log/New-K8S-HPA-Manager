import { useState, useCallback } from 'react';
import type { ScanRequest, ScanResult, CertificateInfo, CopyRequest, UploadRequest, ChainValidationResult } from '../types/certificates';

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
    const response = await fetch(`${API_BASE}/${cluster}/${namespace}/${name}`, {
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
    const response = await fetch(`${API_BASE}/${cluster}/${namespace}/${name}/validate-chain`, {
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
    getReport,
    setScanResult,
  };
}
