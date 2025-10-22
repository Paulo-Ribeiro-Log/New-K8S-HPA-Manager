import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from '@/components/ui/dialog';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Separator } from '@/components/ui/separator';
import { AlertTriangle, CheckCircle, XCircle, Clock, Play, Save, FileDown } from 'lucide-react';
import { useTabManager } from '@/contexts/TabContext';
import { TabState } from '@/types/tabs';

// Interface para representar uma operação em lote
interface BatchOperation {
  tabId: string;
  tabName: string;
  clusterContext: string;
  changes: {
    total: number;
    hpas: number;
    nodePools: number;
  };
  selected: boolean;
  status: 'pending' | 'applying' | 'success' | 'error';
  error?: string;
}

export const BatchOperationsPanel = () => {
  const { state, getTabsWithChanges, dispatch } = useTabManager();
  const [isOpen, setIsOpen] = useState(false);
  const [operations, setOperations] = useState<BatchOperation[]>([]);
  const [isApplying, setIsApplying] = useState(false);
  
  const tabsWithChanges = getTabsWithChanges();
  
  // Inicializar operações quando o panel abre
  const initializeOperations = () => {
    const ops: BatchOperation[] = tabsWithChanges.map(tab => ({
      tabId: tab.id,
      tabName: tab.name,
      clusterContext: tab.clusterContext,
      changes: tab.pendingChanges,
      selected: true,
      status: 'pending' as const,
    }));
    setOperations(ops);
  };
  
  // Toggle seleção de uma operação
  const toggleOperation = (tabId: string) => {
    setOperations(ops => 
      ops.map(op => 
        op.tabId === tabId ? { ...op, selected: !op.selected } : op
      )
    );
  };
  
  // Selecionar/desselecionar todas
  const toggleAll = () => {
    const allSelected = operations.every(op => op.selected);
    setOperations(ops => 
      ops.map(op => ({ ...op, selected: !allSelected }))
    );
  };
  
  // Aplicar mudanças em lote
  const applyBatchChanges = async () => {
    const selectedOps = operations.filter(op => op.selected);
    if (selectedOps.length === 0) return;
    
    setIsApplying(true);
    
    // Simular aplicação das mudanças
    for (const op of selectedOps) {
      setOperations(ops => 
        ops.map(o => 
          o.tabId === op.tabId ? { ...o, status: 'applying' } : o
        )
      );
      
      try {
        // TODO: Implementar chamada real para API
        await new Promise(resolve => setTimeout(resolve, 2000));
        
        // Simular sucesso/erro
        const success = Math.random() > 0.2; // 80% de sucesso
        
        setOperations(ops => 
          ops.map(o => 
            o.tabId === op.tabId ? { 
              ...o, 
              status: success ? 'success' : 'error',
              error: success ? undefined : 'Erro simulado na aplicação'
            } : o
          )
        );
        
        // Se sucesso, limpar changes da aba
        if (success) {
          dispatch({ 
            type: 'UPDATE_TAB_CHANGES', 
            payload: { 
              index: state.tabManager.tabs.findIndex(t => t.id === op.tabId),
              changes: { total: 0, hpas: 0, nodePools: 0 }
            }
          });
        }
        
      } catch (error) {
        setOperations(ops => 
          ops.map(o => 
            o.tabId === op.tabId ? { 
              ...o, 
              status: 'error',
              error: error instanceof Error ? error.message : 'Erro desconhecido'
            } : o
          )
        );
      }
    }
    
    setIsApplying(false);
  };
  
  // Salvar configuração como sessão
  const saveAsSession = () => {
    // TODO: Implementar salvamento de sessão multi-cluster
    console.log('Salvando sessão multi-cluster:', operations.filter(op => op.selected));
  };
  
  // Exportar configuração
  const exportConfig = () => {
    const selectedOps = operations.filter(op => op.selected);
    const config = {
      timestamp: new Date().toISOString(),
      operations: selectedOps.map(op => ({
        cluster: op.clusterContext,
        changes: op.changes,
      })),
    };
    
    const blob = new Blob([JSON.stringify(config, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `multi-cluster-config-${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };
  
  if (tabsWithChanges.length === 0) {
    return null;
  }
  
  const selectedCount = operations.filter(op => op.selected).length;
  const totalSelectedChanges = operations
    .filter(op => op.selected)
    .reduce((sum, op) => sum + op.changes.total, 0);
  
  return (
    <Dialog open={isOpen} onOpenChange={setIsOpen}>
      <DialogTrigger asChild>
        <Button 
          variant="outline" 
          className="relative"
          onClick={initializeOperations}
        >
          <AlertTriangle className="h-4 w-4 mr-2" />
          Operações em Lote
          <Badge variant="secondary" className="ml-2">
            {tabsWithChanges.length}
          </Badge>
        </Button>
      </DialogTrigger>
      
      <DialogContent className="max-w-4xl max-h-[80vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>Operações em Lote Multi-Cluster</DialogTitle>
          <DialogDescription>
            Aplique mudanças simultaneamente em múltiplos clusters. 
            {tabsWithChanges.length} aba{tabsWithChanges.length > 1 ? 's' : ''} com mudanças pendentes.
          </DialogDescription>
        </DialogHeader>
        
        <div className="flex-1 flex flex-col gap-4 overflow-hidden">
          {/* Controles */}
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Checkbox
                checked={operations.length > 0 && operations.every(op => op.selected)}
                onCheckedChange={toggleAll}
              />
              <span className="text-sm">
                Selecionar todas ({selectedCount}/{operations.length})
              </span>
              {totalSelectedChanges > 0 && (
                <Badge variant="secondary">
                  {totalSelectedChanges} mudanças
                </Badge>
              )}
            </div>
            
            <div className="flex items-center gap-2">
              <Button variant="outline" size="sm" onClick={exportConfig} disabled={selectedCount === 0}>
                <FileDown className="h-4 w-4 mr-2" />
                Exportar
              </Button>
              <Button variant="outline" size="sm" onClick={saveAsSession} disabled={selectedCount === 0}>
                <Save className="h-4 w-4 mr-2" />
                Salvar Sessão
              </Button>
            </div>
          </div>
          
          <Separator />
          
          {/* Lista de operações */}
          <ScrollArea className="flex-1">
            <div className="space-y-3">
              {operations.map((operation) => (
                <Card 
                  key={operation.tabId} 
                  className={`transition-all ${
                    operation.selected ? 'ring-2 ring-primary' : ''
                  }`}
                >
                  <CardHeader className="pb-3">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <Checkbox
                          checked={operation.selected}
                          onCheckedChange={() => toggleOperation(operation.tabId)}
                        />
                        <div>
                          <CardTitle className="text-sm">{operation.tabName}</CardTitle>
                          <CardDescription className="text-xs">
                            📍 {operation.clusterContext}
                          </CardDescription>
                        </div>
                      </div>
                      
                      <div className="flex items-center gap-2">
                        <Badge variant="outline">
                          {operation.changes.total} mudanças
                        </Badge>
                        
                        {operation.status === 'pending' && (
                          <Clock className="h-4 w-4 text-muted-foreground" />
                        )}
                        {operation.status === 'applying' && (
                          <div className="animate-spin h-4 w-4 border-2 border-primary border-t-transparent rounded-full" />
                        )}
                        {operation.status === 'success' && (
                          <CheckCircle className="h-4 w-4 text-green-500" />
                        )}
                        {operation.status === 'error' && (
                          <XCircle className="h-4 w-4 text-destructive" />
                        )}
                      </div>
                    </div>
                  </CardHeader>
                  
                  <CardContent className="pt-0">
                    <div className="grid grid-cols-2 gap-4 text-sm">
                      {operation.changes.hpas > 0 && (
                        <div>
                          <div className="font-medium text-blue-600 dark:text-blue-400">
                            HPAs ({operation.changes.hpas})
                          </div>
                          <div className="text-muted-foreground text-xs mt-1">
                            Alterações em recursos HPA
                          </div>
                        </div>
                      )}
                      
                      {operation.changes.nodePools > 0 && (
                        <div>
                          <div className="font-medium text-green-600 dark:text-green-400">
                            Node Pools ({operation.changes.nodePools})
                          </div>
                          <div className="text-muted-foreground text-xs mt-1">
                            Alterações em node pools
                          </div>
                        </div>
                      )}
                    </div>
                    
                    {operation.status === 'error' && operation.error && (
                      <div className="mt-3 p-2 bg-destructive/10 border border-destructive/20 rounded text-sm text-destructive">
                        ❌ {operation.error}
                      </div>
                    )}
                    
                    {operation.status === 'success' && (
                      <div className="mt-3 p-2 bg-green-100 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded text-sm text-green-700 dark:text-green-300">
                        ✅ Mudanças aplicadas com sucesso
                      </div>
                    )}
                  </CardContent>
                </Card>
              ))}
            </div>
          </ScrollArea>
        </div>
        
        <DialogFooter>
          <Button variant="outline" onClick={() => setIsOpen(false)}>
            Fechar
          </Button>
          <Button 
            onClick={applyBatchChanges} 
            disabled={selectedCount === 0 || isApplying}
          >
            {isApplying ? (
              <div className="animate-spin h-4 w-4 border-2 border-white border-t-transparent rounded-full mr-2" />
            ) : (
              <Play className="h-4 w-4 mr-2" />
            )}
            Aplicar Mudanças ({selectedCount})
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default BatchOperationsPanel;