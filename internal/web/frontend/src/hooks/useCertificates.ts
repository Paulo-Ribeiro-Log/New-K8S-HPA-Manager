import { useState, useCallback } from 'react';
import type { ScanRequest, ScanResult, CertificateInfo, CopyRequest, UploadRequest } from '../types/certificates';

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
    getReport,
    setScanResult,
  };
}
