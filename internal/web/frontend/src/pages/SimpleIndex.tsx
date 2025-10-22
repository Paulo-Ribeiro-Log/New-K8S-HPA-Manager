import React, { useState, useEffect } from 'react';
import { apiClient } from '@/lib/api/client';

interface SimpleIndexProps {
  onLogout?: () => void;
}

const SimpleIndex = ({ onLogout }: SimpleIndexProps) => {
  console.log('SimpleIndex component rendering');
  const [clusters, setClusters] = useState<any[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const testAPI = async () => {
      try {
        console.log('Testing API connection...');
        const data = await apiClient.getClusters();
        console.log('API Response:', data);
        setClusters(data);
        setError(null);
      } catch (err) {
        console.error('API Error:', err);
        setError(err instanceof Error ? err.message : 'API Error');
      } finally {
        setLoading(false);
      }
    };
    testAPI();
  }, []);
  
  return (
    <div style={{ 
      minHeight: '100vh', 
      backgroundColor: '#f3f4f6', 
      padding: '20px',
      fontFamily: 'system-ui, -apple-system, sans-serif'
    }}>
      <div style={{ maxWidth: '800px', margin: '0 auto' }}>
        <h1 style={{ 
          fontSize: '2rem', 
          fontWeight: 'bold', 
          color: '#111827', 
          marginBottom: '2rem'
        }}>
          k8s HPA Manager - Debug Version
        </h1>
        <div style={{ 
          backgroundColor: 'white', 
          borderRadius: '8px', 
          boxShadow: '0 1px 3px rgba(0, 0, 0, 0.1)', 
          padding: '1.5rem'
        }}>
          <p style={{ color: '#6b7280', marginBottom: '1rem' }}>
            ✅ Interface carregada com sucesso! 🎉
          </p>
          <p style={{ fontSize: '0.875rem', color: '#9ca3af', marginBottom: '1rem' }}>
            Diagnóstico: Frontend funcionando corretamente
          </p>
          <button
            onClick={() => {
              console.log('Button clicked!');
              alert('Botão funcionando!');
            }}
            style={{
              padding: '8px 16px',
              backgroundColor: '#2563eb',
              color: 'white',
              border: 'none',
              borderRadius: '4px',
              cursor: 'pointer'
            }}
          >
            Testar Interação
          </button>
          <div style={{ marginTop: '1rem', fontSize: '0.875rem', color: '#6b7280' }}>
            <p>Data: {new Date().toLocaleString()}</p>
            <p>Status: Component mounted successfully</p>
            <p>API Status: {loading ? 'Loading...' : error ? `Error: ${error}` : `✅ ${clusters.length} clusters found`}</p>
            {clusters.length > 0 && (
              <div style={{ marginTop: '1rem' }}>
                <p><strong>Clusters encontrados:</strong></p>
                <ul style={{ margin: '0.5rem 0', paddingLeft: '1rem' }}>
                  {clusters.slice(0, 3).map((cluster, i) => (
                    <li key={i} style={{ marginBottom: '0.25rem' }}>
                      {cluster.name} ({cluster.context})
                    </li>
                  ))}
                  {clusters.length > 3 && <li>... e mais {clusters.length - 3} clusters</li>}
                </ul>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default SimpleIndex;