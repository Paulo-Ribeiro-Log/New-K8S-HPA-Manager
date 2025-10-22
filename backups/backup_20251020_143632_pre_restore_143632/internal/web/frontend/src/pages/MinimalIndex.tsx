import React, { useState, useEffect } from "react";
import { useClusters } from "@/hooks/useAPI";

interface MinimalIndexProps {
  onLogout?: () => void;
}

const MinimalIndex = ({ onLogout }: MinimalIndexProps) => {
  console.log('[MinimalIndex] Rendering component');
  
  // Estados básicos
  const [selectedCluster, setSelectedCluster] = useState("");
  
  // Hook básico de clusters
  const { clusters, loading: clustersLoading } = useClusters();
  
  console.log('[MinimalIndex] Clusters:', clusters.length);
  
  // Early return se carregando
  if (clustersLoading) {
    return (
      <div style={{ 
        display: 'flex', 
        alignItems: 'center', 
        justifyContent: 'center', 
        minHeight: '100vh',
        fontFamily: 'system-ui, -apple-system, sans-serif'
      }}>
        <div style={{ textAlign: 'center' }}>
          <div style={{ 
            width: '32px', 
            height: '32px', 
            border: '2px solid #e5e7eb',
            borderTop: '2px solid #3b82f6',
            borderRadius: '50%',
            animation: 'spin 1s linear infinite',
            margin: '0 auto 16px'
          }}></div>
          <p>Carregando clusters...</p>
        </div>
      </div>
    );
  }

  // Auto-select first cluster
  useEffect(() => {
    if (clusters.length > 0 && !selectedCluster) {
      console.log('[MinimalIndex] Auto-selecting cluster:', clusters[0].context);
      setSelectedCluster(clusters[0].context);
    }
  }, [clusters, selectedCluster]);

  return (
    <div style={{ 
      minHeight: '100vh', 
      backgroundColor: '#f9fafb', 
      fontFamily: 'system-ui, -apple-system, sans-serif'
    }}>
      {/* Header simples */}
      <header style={{ 
        backgroundColor: 'white', 
        borderBottom: '1px solid #e5e7eb', 
        padding: '1rem 2rem'
      }}>
        <div style={{ 
          maxWidth: '1200px', 
          margin: '0 auto', 
          display: 'flex', 
          justifyContent: 'space-between', 
          alignItems: 'center'
        }}>
          <h1 style={{ margin: 0, fontSize: '1.5rem', fontWeight: 'bold', color: '#111827' }}>
            k8s HPA Manager
          </h1>
          <div style={{ display: 'flex', gap: '1rem', alignItems: 'center' }}>
            <select 
              value={selectedCluster}
              onChange={(e) => setSelectedCluster(e.target.value)}
              style={{ 
                padding: '0.5rem', 
                border: '1px solid #d1d5db', 
                borderRadius: '0.375rem',
                backgroundColor: 'white'
              }}
            >
              <option value="">Selecionar Cluster</option>
              {clusters.map((cluster) => (
                <option key={cluster.context} value={cluster.context}>
                  {cluster.name}
                </option>
              ))}
            </select>
            <button
              onClick={onLogout}
              style={{
                padding: '0.5rem 1rem',
                backgroundColor: '#ef4444',
                color: 'white',
                border: 'none',
                borderRadius: '0.375rem',
                cursor: 'pointer'
              }}
            >
              Logout
            </button>
          </div>
        </div>
      </header>

      {/* Conteúdo principal */}
      <main style={{ maxWidth: '1200px', margin: '0 auto', padding: '2rem' }}>
        <div style={{ 
          backgroundColor: 'white', 
          borderRadius: '0.5rem', 
          padding: '2rem',
          boxShadow: '0 1px 3px rgba(0, 0, 0, 0.1)'
        }}>
          <h2 style={{ marginTop: 0, marginBottom: '1rem', color: '#374151' }}>
            Dashboard
          </h2>
          
          <div style={{ marginBottom: '2rem' }}>
            <h3 style={{ fontSize: '1.125rem', marginBottom: '0.5rem', color: '#4b5563' }}>
              Status do Sistema
            </h3>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))', gap: '1rem' }}>
              <div style={{ 
                padding: '1rem', 
                backgroundColor: '#f3f4f6', 
                borderRadius: '0.375rem',
                textAlign: 'center'
              }}>
                <div style={{ fontSize: '2rem', fontWeight: 'bold', color: '#059669' }}>
                  {clusters.length}
                </div>
                <div style={{ fontSize: '0.875rem', color: '#6b7280' }}>
                  Clusters Disponíveis
                </div>
              </div>
              
              <div style={{ 
                padding: '1rem', 
                backgroundColor: '#f3f4f6', 
                borderRadius: '0.375rem',
                textAlign: 'center'
              }}>
                <div style={{ fontSize: '2rem', fontWeight: 'bold', color: selectedCluster ? '#059669' : '#d97706' }}>
                  {selectedCluster ? '✓' : '-'}
                </div>
                <div style={{ fontSize: '0.875rem', color: '#6b7280' }}>
                  Cluster Selecionado
                </div>
              </div>
            </div>
          </div>

          {selectedCluster && (
            <div>
              <h3 style={{ fontSize: '1.125rem', marginBottom: '0.5rem', color: '#4b5563' }}>
                Cluster Ativo
              </h3>
              <p style={{ 
                padding: '1rem', 
                backgroundColor: '#eff6ff', 
                borderRadius: '0.375rem',
                margin: 0,
                color: '#1e40af',
                fontSize: '0.875rem'
              }}>
                <strong>Contexto:</strong> {selectedCluster}
              </p>
            </div>
          )}
        </div>
      </main>

      <style dangerouslySetInnerHTML={{
        __html: `
          @keyframes spin {
            from { transform: rotate(0deg); }
            to { transform: rotate(360deg); }
          }
        `
      }} />
    </div>
  );
};

export default MinimalIndex;