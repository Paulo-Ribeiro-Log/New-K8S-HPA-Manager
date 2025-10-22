import React from 'react';

interface TestIndexProps {
  onLogout?: () => void;
}

const TestIndex = ({ onLogout }: TestIndexProps) => {
  return (
    <div className="min-h-screen bg-background text-foreground p-8">
      <div className="max-w-4xl mx-auto">
        <h1 className="text-4xl font-bold mb-6">🚀 k8s HPA Manager</h1>
        <div className="space-y-4">
          <p className="text-xl">✅ React está funcionando!</p>
          <p className="text-lg">✅ Tailwind CSS está funcionando!</p>
          <p className="text-base">✅ Build do frontend concluído com sucesso!</p>
          
          <div className="bg-card p-6 rounded-lg border">
            <h2 className="text-2xl font-semibold mb-4">Status do Sistema</h2>
            <ul className="space-y-2">
              <li>✅ Frontend: Carregado</li>
              <li>✅ Backend: Rodando</li>
              <li>✅ API: Funcionando</li>
              <li>⚠️ Interface: Em teste</li>
            </ul>
          </div>
          
          <button 
            onClick={() => window.location.href = '/api/v1/clusters'}
            className="bg-primary text-primary-foreground px-4 py-2 rounded hover:bg-primary/90"
          >
            Testar API diretamente
          </button>

          {onLogout && (
            <button 
              onClick={onLogout}
              className="bg-destructive text-destructive-foreground px-4 py-2 rounded hover:bg-destructive/90 ml-4"
            >
              Logout
            </button>
          )}
        </div>
      </div>
    </div>
  );
};

export default TestIndex;