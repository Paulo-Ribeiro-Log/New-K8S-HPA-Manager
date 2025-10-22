import React, { useState, useEffect } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Loader2, FolderOpen, Calendar, User, Info, FileText } from 'lucide-react';
import { useSessionTemplates } from '@/hooks/useSessions';
import { useStaging } from '@/contexts/StagingContext';
import type { Session } from '@/lib/api/types';
import { apiClient } from '@/lib/api/client';

interface LoadSessionModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSessionLoaded?: () => void;
}

// Mock para agora - posteriormente integrar com API real
const mockSessions: Session[] = [
  {
    name: "upscale-producao-18-10-2025",
    created_at: "2025-10-18T15:30:00Z",
    created_by: "admin@k8s.local",
    description: "Scale up para pico de tráfego",
    template_used: "{action}_{env}_{date}",
    changes: [
      {
        cluster: "akspriv-faturamento-prd-admin",
        namespace: "ingress-nginx",
        hpa_name: "nginx-ingress-controller",
        original_values: { min_replicas: 1, max_replicas: 8, target_cpu: 60 },
        new_values: { min_replicas: 2, max_replicas: 12, target_cpu: 70 },
        applied: false,
        rollout_triggered: false,
        daemonset_rollout_triggered: false,
        statefulset_rollout_triggered: false,
      }
    ],
    node_pool_changes: [],
    resource_changes: [],
  },
  {
    name: "nodepool-stress-test-18-10",
    created_at: "2025-10-18T14:15:00Z",
    created_by: "admin@k8s.local",
    description: "Teste de stress com node pools",
    template_used: "{action}_{cluster}_{date}",
    changes: [],
    node_pool_changes: [
      {
        cluster: "akspriv-faturamento-prd",
        resource_group: "rg-faturamento-app-prd",
        subscription: "PRD - ONLINE 2",
        node_pool_name: "monitoring",
        original_values: { node_count: 3, min_node_count: 2, max_node_count: 5, autoscaling_enabled: true },
        new_values: { node_count: 0, min_node_count: 0, max_node_count: 1, autoscaling_enabled: false },
        applied: false,
        sequence_order: 1,
        sequence_status: "",
      }
    ],
    resource_changes: [],
  },
];

export function LoadSessionModal({ open, onOpenChange, onSessionLoaded }: LoadSessionModalProps) {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedSession, setSelectedSession] = useState<Session | null>(null);
  const [loadingSession, setLoadingSession] = useState(false);

  const staging = useStaging();

  // Carregar sessões quando modal abrir
  useEffect(() => {
    if (open) {
      loadSessions();
    }
  }, [open]);

  const loadSessions = async () => {
    setLoading(true);
    setError(null);
    try {
      // Carregar todas as sessões de todas as pastas usando o endpoint real
      const response = await fetch('/api/v1/sessions', {
        headers: {
          'Authorization': `Bearer poc-token-123`,
          'Content-Type': 'application/json',
        },
      });
      
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }
      
      const data = await response.json();
      
      if (data.success && data.data?.sessions) {
        setSessions(data.data.sessions.map((sessionSummary: any) => ({
          name: sessionSummary.name,
          created_at: sessionSummary.created_at,
          created_by: sessionSummary.created_by,
          description: sessionSummary.description || '',
          template_used: sessionSummary.template_used || 'custom',
          changes: [], // Will be loaded when session is selected
          node_pool_changes: [],
          resource_changes: [],
          metadata: sessionSummary.metadata,
          folder: sessionSummary.folder, // Adicionar informação da pasta
        })));
      } else {
        throw new Error(data.error?.message || 'Formato de resposta inválido');
      }
      
      setLoading(false);
    } catch (err) {
      console.error('Erro ao carregar sessões:', err);
      setError(`Erro ao carregar sessões: ${err instanceof Error ? err.message : 'Erro desconhecido'}`);
      setLoading(false);
    }
  };

  const handleLoadSession = async () => {
    if (!selectedSession) return;

    setLoadingSession(true);
    try {
      // Carregar sessão no staging context
      staging.loadFromSession(selectedSession);
      
      // Fechar modal
      onOpenChange(false);
      onSessionLoaded?.();
      
      // Limpar seleção
      setSelectedSession(null);
    } catch (err) {
      setError('Erro ao carregar sessão');
    } finally {
      setLoadingSession(false);
    }
  };

  const getSessionTypeColor = (session: Session) => {
    if (session.changes.length > 0 && session.node_pool_changes.length > 0) {
      return 'bg-purple-100 text-purple-800 border-purple-200';
    }
    if (session.changes.length > 0) {
      return 'bg-blue-100 text-blue-800 border-blue-200';
    }
    if (session.node_pool_changes.length > 0) {
      return 'bg-green-100 text-green-800 border-green-200';
    }
    return 'bg-gray-100 text-gray-800 border-gray-200';
  };

  const getSessionType = (session: Session) => {
    if (session.changes.length > 0 && session.node_pool_changes.length > 0) {
      return 'Mixed';
    }
    if (session.changes.length > 0) {
      return 'HPAs';
    }
    if (session.node_pool_changes.length > 0) {
      return 'Node Pools';
    }
    return 'Empty';
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl max-h-[80vh]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FolderOpen className="h-5 w-5" />
            Carregar Sessão
          </DialogTitle>
          <DialogDescription>
            Selecione uma sessão salva para carregar as alterações na staging area.
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 min-h-[400px]">
          {/* Lista de Sessões */}
          <div className="space-y-4">
            <h3 className="text-sm font-medium">Sessões Disponíveis</h3>
            
            {loading ? (
              <div className="flex items-center justify-center h-64">
                <Loader2 className="h-6 w-6 animate-spin" />
                <span className="ml-2">Carregando sessões...</span>
              </div>
            ) : error ? (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : (
              <ScrollArea className="h-[350px]">
                <div className="space-y-2">
                  {sessions.map((session) => (
                    <Card
                      key={session.name}
                      className={`cursor-pointer transition-colors ${
                        selectedSession?.name === session.name
                          ? 'border-primary bg-accent'
                          : 'hover:bg-accent/50'
                      }`}
                      onClick={() => setSelectedSession(session)}
                    >
                      <CardHeader className="pb-2">
                        <div className="flex items-start justify-between">
                          <CardTitle className="text-sm">{session.name}</CardTitle>
                          <Badge className={getSessionTypeColor(session)}>
                            {getSessionType(session)}
                          </Badge>
                        </div>
                      </CardHeader>
                      <CardContent className="pt-0">
                        <div className="space-y-1 text-xs text-muted-foreground">
                          <div className="flex items-center gap-1">
                            <Calendar className="h-3 w-3" />
                            {new Date(session.created_at).toLocaleString('pt-BR')}
                          </div>
                          <div className="flex items-center gap-1">
                            <User className="h-3 w-3" />
                            {session.created_by}
                          </div>
                          {session.description && (
                            <div className="flex items-start gap-1">
                              <FileText className="h-3 w-3 mt-0.5" />
                              <span className="text-xs">{session.description}</span>
                            </div>
                          )}
                        </div>
                        <div className="flex gap-2 mt-2">
                          {session.changes.length > 0 && (
                            <Badge variant="outline" className="text-xs">
                              {session.changes.length} HPAs
                            </Badge>
                          )}
                          {session.node_pool_changes.length > 0 && (
                            <Badge variant="outline" className="text-xs">
                              {session.node_pool_changes.length} Node Pools
                            </Badge>
                          )}
                        </div>
                      </CardContent>
                    </Card>
                  ))}
                </div>
              </ScrollArea>
            )}
          </div>

          {/* Preview da Sessão Selecionada */}
          <div className="space-y-4">
            <h3 className="text-sm font-medium">Preview da Sessão</h3>
            
            {!selectedSession ? (
              <div className="flex items-center justify-center h-[350px] text-muted-foreground border rounded-lg">
                <div className="text-center">
                  <Info className="h-8 w-8 mx-auto mb-2 opacity-50" />
                  <p>Selecione uma sessão para ver o preview</p>
                </div>
              </div>
            ) : (
              <ScrollArea className="h-[350px] border rounded-lg p-4">
                <div className="space-y-4">
                  {/* Informações da Sessão */}
                  <div className="space-y-2">
                    <h4 className="font-medium">{selectedSession.name}</h4>
                    <p className="text-sm text-muted-foreground">{selectedSession.description}</p>
                    <div className="text-xs text-muted-foreground">
                      <p>Criado por: {selectedSession.created_by}</p>
                      <p>Data: {new Date(selectedSession.created_at).toLocaleString('pt-BR')}</p>
                      <p>Template: {selectedSession.template_used}</p>
                    </div>
                  </div>

                  {/* HPAs */}
                  {selectedSession.changes.length > 0 && (
                    <div className="space-y-2">
                      <h5 className="text-sm font-medium">HPAs ({selectedSession.changes.length})</h5>
                      {selectedSession.changes.map((change, index) => (
                        <div key={index} className="p-2 bg-muted rounded text-xs">
                          <div className="font-medium">{change.namespace}/{change.hpa_name}</div>
                          <div className="text-muted-foreground">
                            Min: {change.original_values?.min_replicas} → {change.new_values?.min_replicas}
                            {", "}
                            Max: {change.original_values?.max_replicas} → {change.new_values?.max_replicas}
                            {change.new_values?.target_cpu && (
                              <>, CPU: {change.original_values?.target_cpu}% → {change.new_values?.target_cpu}%</>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}

                  {/* Node Pools */}
                  {selectedSession.node_pool_changes.length > 0 && (
                    <div className="space-y-2">
                      <h5 className="text-sm font-medium">Node Pools ({selectedSession.node_pool_changes.length})</h5>
                      {selectedSession.node_pool_changes.map((change, index) => (
                        <div key={index} className="p-2 bg-muted rounded text-xs">
                          <div className="font-medium">{change.node_pool_name}</div>
                          <div className="text-muted-foreground">
                            Count: {change.original_values.node_count} → {change.new_values.node_count}
                            {", "}
                            Autoscaling: {change.original_values.autoscaling_enabled ? 'ON' : 'OFF'} → {change.new_values.autoscaling_enabled ? 'ON' : 'OFF'}
                            {change.sequence_order > 0 && (
                              <>, Sequence: *{change.sequence_order}</>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </ScrollArea>
            )}
          </div>
        </div>

        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loadingSession}>
            Cancelar
          </Button>
          <Button 
            onClick={handleLoadSession} 
            disabled={!selectedSession || loadingSession}
          >
            {loadingSession && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            Carregar Sessão
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}