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
import { Loader2, FolderOpen, Calendar, User, Info, FileText, Folder, Edit2, Trash2, MoreVertical } from 'lucide-react';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { useSessionTemplates } from '@/hooks/useSessions';
import { useStaging } from '@/contexts/StagingContext';
import type { Session } from '@/lib/api/types';
import { apiClient } from '@/lib/api/client';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu';
import { EditSessionModal } from './EditSessionModal';
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Input } from '@/components/ui/input';
import { toast } from 'sonner';

interface LoadSessionModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSessionLoaded?: (clusterName: string) => void;
}

// Pastas disponíveis (mesmas do Save Session)
const SESSION_FOLDERS = [
  { value: 'all', label: 'Todas as Pastas', icon: '📁', description: 'Exibir todas as sessões' },
  { value: 'HPA-Upscale', label: 'HPA-Upscale', icon: '📈', description: 'HPA scale up sessions' },
  { value: 'HPA-Downscale', label: 'HPA-Downscale', icon: '📉', description: 'HPA scale down sessions' },
  { value: 'Node-Upscale', label: 'Node-Upscale', icon: '🚀', description: 'Node pool scale up sessions' },
  { value: 'Node-Downscale', label: 'Node-Downscale', icon: '⬇️', description: 'Node pool scale down sessions' },
  { value: 'Rollback', label: 'Rollback', icon: '⏪', description: 'Rollback sessions' },
];

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
  const [selectedSessionDetails, setSelectedSessionDetails] = useState<Session | null>(null);
  const [loadingDetails, setLoadingDetails] = useState(false);
  const [loadingSession, setLoadingSession] = useState(false);
  const [selectedFolder, setSelectedFolder] = useState<string>('all');

  // Estados para gerenciamento de sessões
  const [sessionToDelete, setSessionToDelete] = useState<Session | null>(null);
  const [sessionToRename, setSessionToRename] = useState<Session | null>(null);
  const [sessionToEdit, setSessionToEdit] = useState<Session | null>(null);
  const [newSessionName, setNewSessionName] = useState('');
  const [isDeleting, setIsDeleting] = useState(false);
  const [isRenaming, setIsRenaming] = useState(false);

  // Estados removidos: selectedHPAs, selectedNodePools, applyingDirectly, currentProcessing, recoveryProgress (Apply Directly feature removida)

  const staging = useStaging();

  // Carregar sessões quando modal abrir ou pasta mudar
  useEffect(() => {
    if (open) {
      loadSessions();
      setSelectedSession(null);
      setSelectedSessionDetails(null);
    }
  }, [open, selectedFolder]);

  // Carregar detalhes completos da sessão selecionada
  const loadSessionDetails = async (sessionName: string, folder: string) => {
    setLoadingDetails(true);
    setError(null);
    try {
      const folderQuery = folder ? `?folder=${encodeURIComponent(folder)}` : '';
      const response = await fetch(`/api/v1/sessions/${encodeURIComponent(sessionName)}${folderQuery}`, {
        headers: {
          'Authorization': `Bearer poc-token-123`,
          'Content-Type': 'application/json',
        },
      });
      
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
      }
      
      const data = await response.json();
      
      if (data.success && data.data) {
        setSelectedSessionDetails(data.data);
      } else {
        throw new Error(data.error?.message || 'Formato de resposta inválido');
      }
    } catch (err) {
      console.error('Erro ao carregar detalhes da sessão:', err);
      setError(`Erro ao carregar detalhes: ${err instanceof Error ? err.message : 'Erro desconhecido'}`);
      setSelectedSessionDetails(null);
    } finally {
      setLoadingDetails(false);
    }
  };

  const loadSessions = async (folder?: string) => {
    setLoading(true);
    setError(null);
    try {
      // Determinar endpoint baseado na pasta selecionada
      const folderFilter = folder || selectedFolder;
      const endpoint = folderFilter === 'all' 
        ? '/api/v1/sessions'
        : `/api/v1/sessions/folders/${folderFilter}`;
      
      const response = await fetch(endpoint, {
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
    if (!selectedSessionDetails) return;

    setLoadingSession(true);
    setError(null);

    try {
      // Extrair clusters únicos das mudanças
      const clusters = new Set<string>();

      selectedSessionDetails.changes?.forEach(change => {
        if (change.cluster) clusters.add(change.cluster);
      });

      selectedSessionDetails.node_pool_changes?.forEach(change => {
        if (change.cluster) clusters.add(change.cluster);
      });

      // Se houver clusters, tentar trocar contexto para o primeiro
      if (clusters.size > 0) {
        const clusterName = Array.from(clusters)[0];
        // Adicionar sufixo -admin ao nome do cluster para o contexto do Kubernetes
        const contextName = `${clusterName}-admin`;
        
        try {
          // Buscar configuração do cluster
          const clusterConfigResponse = await fetch(`/api/v1/clusters/${encodeURIComponent(clusterName)}/config`, {
            headers: {
              'Authorization': `Bearer poc-token-123`,
              'Content-Type': 'application/json',
            },
          });
          
          if (clusterConfigResponse.ok) {
            const clusterConfig = await clusterConfigResponse.json();
            
            // Trocar subscription do Azure se necessário
            if (clusterConfig.subscription) {
              await fetch(`/api/v1/azure/subscription`, {
                method: 'POST',
                headers: {
                  'Authorization': `Bearer poc-token-123`,
                  'Content-Type': 'application/json',
                },
                body: JSON.stringify({ subscription: clusterConfig.subscription }),
              });
            }
            
            // Trocar contexto do Kubernetes usando o nome com -admin
            await fetch(`/api/v1/clusters/switch-context`, {
              method: 'POST',
              headers: {
                'Authorization': `Bearer poc-token-123`,
                'Content-Type': 'application/json',
              },
              body: JSON.stringify({ context: contextName }),
            });
          }
        } catch (contextErr) {
          console.warn('Não foi possível trocar contexto automaticamente:', contextErr);
          // Continuar mesmo se falhar a troca de contexto
        }
      }
      
      // Carregar sessão no staging context
      staging.loadFromSession(selectedSessionDetails);
      
      // Fechar modal
      onOpenChange(false);
      
      // Notificar que a sessão foi carregada, passando o cluster
      if (clusters.size > 0) {
        const clusterName = Array.from(clusters)[0];
        onSessionLoaded?.(clusterName);
      } else {
        onSessionLoaded?.('');
      }
      
      // Limpar seleção
      setSelectedSession(null);
      setSelectedSessionDetails(null);
    } catch (err) {
      console.error('Erro ao carregar sessão:', err);
      setError(`Erro ao carregar sessão: ${err instanceof Error ? err.message : 'Erro desconhecido'}`);
    } finally {
      setLoadingSession(false);
    }
  };

  const handleDeleteSession = async () => {
    if (!sessionToDelete) return;
    
    setIsDeleting(true);
    try {
      const folderQuery = sessionToDelete.folder ? `?folder=${encodeURIComponent(sessionToDelete.folder)}` : '';
      const response = await fetch(`/api/v1/sessions/${encodeURIComponent(sessionToDelete.name)}${folderQuery}`, {
        method: 'DELETE',
        headers: {
          'Authorization': `Bearer poc-token-123`,
        },
      });

      if (!response.ok) {
        throw new Error(`Erro ao deletar: ${response.statusText}`);
      }

      toast.success(`Sessão "${sessionToDelete.name}" removida com sucesso`);
      setSessionToDelete(null);
      
      // Se a sessão deletada estava selecionada, limpar seleção
      if (selectedSession?.name === sessionToDelete.name) {
        setSelectedSession(null);
        setSelectedSessionDetails(null);
      }
      
      // Recarregar lista
      loadSessions();
    } catch (err) {
      toast.error(`Erro ao deletar sessão: ${err instanceof Error ? err.message : 'Erro desconhecido'}`);
    } finally {
      setIsDeleting(false);
    }
  };

  const handleRenameSession = async () => {
    if (!sessionToRename || !newSessionName.trim()) return;
    
    setIsRenaming(true);
    try {
      const folderQuery = sessionToRename.folder ? `?folder=${encodeURIComponent(sessionToRename.folder)}` : '';
      const response = await fetch(`/api/v1/sessions/${encodeURIComponent(sessionToRename.name)}/rename${folderQuery}`, {
        method: 'PUT',
        headers: {
          'Authorization': `Bearer poc-token-123`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ new_name: newSessionName.trim() }),
      });

      if (!response.ok) {
        throw new Error(`Erro ao renomear: ${response.statusText}`);
      }

      toast.success(`Sessão renomeada para "${newSessionName.trim()}"`);
      setSessionToRename(null);
      setNewSessionName('');
      
      // Recarregar lista
      loadSessions();
    } catch (err) {
      toast.error(`Erro ao renomear sessão: ${err instanceof Error ? err.message : 'Erro desconhecido'}`);
    } finally {
      setIsRenaming(false);
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

        {/* Seletor de Pasta */}
        <div className="space-y-2">
          <label className="text-sm font-medium flex items-center gap-2">
            <Folder className="h-4 w-4" />
            Filtrar por Pasta
          </label>
          <Select value={selectedFolder} onValueChange={setSelectedFolder}>
            <SelectTrigger>
              <SelectValue placeholder="Selecione uma pasta..." />
            </SelectTrigger>
            <SelectContent>
              {SESSION_FOLDERS.map((folder) => (
                <SelectItem key={folder.value} value={folder.value}>
                  <div className="flex items-center gap-2">
                    <span>{folder.icon}</span>
                    <div>
                      <div className="font-medium">{folder.label}</div>
                      <div className="text-xs text-muted-foreground">
                        {folder.description}
                      </div>
                    </div>
                  </div>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

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
                      className={`transition-colors ${
                        selectedSession?.name === session.name
                          ? 'border-primary bg-accent'
                          : 'hover:bg-accent/50'
                      }`}
                    >
                      <CardHeader className="pb-2">
                        <div className="flex items-start justify-between gap-2">
                          <div className="flex-1 cursor-pointer" onClick={() => {
                            setSelectedSession(session);
                            loadSessionDetails(session.name, session.folder || '');
                          }}>
                            <CardTitle className="text-sm">{session.name}</CardTitle>
                            {session.folder && (
                              <div className="text-xs text-muted-foreground mt-0.5">
                                📁 {session.folder}
                              </div>
                            )}
                          </div>
                          <div className="flex items-center gap-1">
                            <Badge className={getSessionTypeColor(session)}>
                              {getSessionType(session)}
                            </Badge>
                            <DropdownMenu>
                              <DropdownMenuTrigger asChild onClick={(e) => e.stopPropagation()}>
                                <Button variant="ghost" size="icon" className="h-6 w-6 cursor-pointer hover:bg-accent">
                                  <MoreVertical className="h-4 w-4" />
                                </Button>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end">
                                <DropdownMenuItem 
                                  onClick={async (e) => {
                                    e.stopPropagation();
                                    // Carregar detalhes completos da sessão antes de editar
                                    try {
                                      const folderQuery = session.folder ? `?folder=${encodeURIComponent(session.folder)}` : '';
                                      const response = await fetch(
                                        `/api/v1/sessions/${encodeURIComponent(session.name)}${folderQuery}`,
                                        {
                                          headers: {
                                            'Authorization': `Bearer poc-token-123`,
                                            'Content-Type': 'application/json',
                                          },
                                        }
                                      );
                                      
                                      if (!response.ok) {
                                        throw new Error(`HTTP ${response.status}`);
                                      }
                                      
                                      const data = await response.json();
                                      
                                      if (data.success && data.data) {
                                        setSessionToEdit(data.data);
                                      } else {
                                        throw new Error('Formato inválido');
                                      }
                                    } catch (err) {
                                      console.error('Erro ao carregar sessão:', err);
                                      toast.error('Erro ao carregar sessão para edição');
                                    }
                                  }}
                                >
                                  <Edit2 className="h-4 w-4 mr-2" />
                                  Editar Conteúdo
                                </DropdownMenuItem>
                                <DropdownMenuItem 
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    setSessionToRename(session);
                                    setNewSessionName(session.name);
                                  }}
                                >
                                  <FileText className="h-4 w-4 mr-2" />
                                  Renomear
                                </DropdownMenuItem>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem 
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    setSessionToDelete(session);
                                  }}
                                  className="text-destructive focus:text-destructive"
                                >
                                  <Trash2 className="h-4 w-4 mr-2" />
                                  Deletar
                                </DropdownMenuItem>
                              </DropdownMenuContent>
                            </DropdownMenu>
                          </div>
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
            ) : loadingDetails ? (
              <div className="flex items-center justify-center h-[350px] border rounded-lg">
                <div className="text-center">
                  <Loader2 className="h-6 w-6 mx-auto mb-2 animate-spin" />
                  <p className="text-sm text-muted-foreground">Carregando detalhes...</p>
                </div>
              </div>
            ) : selectedSessionDetails ? (
              <ScrollArea className="h-[350px] border rounded-lg p-4">
                <div className="space-y-4">
                  {/* Informação de Origem da Sessão */}
                  <div className="p-3 bg-blue-50 dark:bg-blue-950 border border-blue-200 dark:border-blue-800 rounded-lg">
                    <div className="text-sm font-semibold text-blue-900 dark:text-blue-100 mb-1">
                      📂 Sessão Carregada:
                    </div>
                    <div className="text-base font-bold text-blue-700 dark:text-blue-300">
                      {selectedSessionDetails.name}
                    </div>
                    {(() => {
                      const clusters = new Set<string>();
                      selectedSessionDetails.changes?.forEach(change => {
                        if (change.cluster) clusters.add(change.cluster);
                      });
                      selectedSessionDetails.node_pool_changes?.forEach(change => {
                        if (change.cluster) clusters.add(change.cluster);
                      });
                      return clusters.size > 0 && (
                        <div className="text-sm font-semibold text-blue-600 dark:text-blue-400 mt-1">
                          🎯 Cluster: <span className="font-bold">{Array.from(clusters).join(', ')}</span>
                        </div>
                      );
                    })()}
                  </div>

                  {/* Informações da Sessão */}
                  <div className="space-y-2">
                    <h4 className="font-medium">{selectedSessionDetails.name}</h4>
                    <p className="text-sm text-muted-foreground">{selectedSessionDetails.description}</p>
                    <div className="text-xs text-muted-foreground">
                      <p>Criado por: {selectedSessionDetails.created_by}</p>
                      <p>Data: {new Date(selectedSessionDetails.created_at).toLocaleString('pt-BR')}</p>
                      <p>Template: {selectedSessionDetails.template_used}</p>
                    </div>
                  </div>

                  {/* HPAs */}
                  {selectedSessionDetails.changes && selectedSessionDetails.changes.length > 0 && (
                    <div className="space-y-2">
                      <h5 className="text-sm font-medium">📊 HPAs ({selectedSessionDetails.changes.length})</h5>
                      {selectedSessionDetails.changes.map((change, index) => (
                        <div key={index} className="p-3 rounded-md text-xs space-y-1 bg-muted border">
                          <div className="font-medium text-sm">{change.namespace}/{change.hpa_name}</div>
                          <div className="text-muted-foreground space-y-0.5 mt-1">
                            <div>🔹 Min Replicas: <span className="text-red-500">{change.original_values?.min_replicas}</span> → <span className="text-green-500">{change.new_values?.min_replicas}</span></div>
                            <div>🔹 Max Replicas: <span className="text-red-500">{change.original_values?.max_replicas}</span> → <span className="text-green-500">{change.new_values?.max_replicas}</span></div>
                            {change.new_values?.target_cpu && (
                              <div>🔹 Target CPU: <span className="text-red-500">{change.original_values?.target_cpu}%</span> → <span className="text-green-500">{change.new_values?.target_cpu}%</span></div>
                            )}
                            {change.new_values?.target_memory && (
                              <div>🔹 Target Memory: <span className="text-red-500">{change.original_values?.target_memory}%</span> → <span className="text-green-500">{change.new_values?.target_memory}%</span></div>
                            )}
                            {(change.new_values?.perform_rollout || change.new_values?.perform_daemonset_rollout || change.new_values?.perform_statefulset_rollout) && (
                              <div className="text-orange-600 font-medium mt-1">
                                🔄 Rollout: {[
                                  change.new_values?.perform_rollout && 'Deployment',
                                  change.new_values?.perform_daemonset_rollout && 'DaemonSet',
                                  change.new_values?.perform_statefulset_rollout && 'StatefulSet'
                                ].filter(Boolean).join(', ')}
                              </div>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}

                  {/* Node Pools */}
                  {selectedSessionDetails.node_pool_changes && selectedSessionDetails.node_pool_changes.length > 0 && (
                    <div className="space-y-2">
                      <h5 className="text-sm font-medium">🛠️ Node Pools ({selectedSessionDetails.node_pool_changes.length})</h5>
                      {selectedSessionDetails.node_pool_changes.map((change, index) => (
                        <div key={index} className="p-3 rounded-md text-xs space-y-1 bg-muted border">
                          <div className="font-medium text-sm">{change.node_pool_name}</div>
                          <div className="text-muted-foreground space-y-0.5 mt-1">
                            <div>🔹 Node Count: <span className="text-red-500">{change.original_values.node_count}</span> → <span className="text-green-500">{change.new_values.node_count}</span></div>
                            <div>🔹 Autoscaling: <span className="text-red-500">{change.original_values.autoscaling_enabled ? 'ON' : 'OFF'}</span> → <span className="text-green-500">{change.new_values.autoscaling_enabled ? 'ON' : 'OFF'}</span></div>
                            {change.new_values.autoscaling_enabled && (
                              <div>🔹 Min/Max: <span className="text-green-500">{change.new_values.min_node_count}/{change.new_values.max_node_count}</span></div>
                            )}
                            {change.sequence_order && change.sequence_order > 0 && (
                              <div className="text-blue-600 font-medium">🔢 Sequence Order: {change.sequence_order}</div>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </ScrollArea>
            ) : (
              <div className="flex items-center justify-center h-[350px] text-muted-foreground border rounded-lg">
                <div className="text-center">
                  <Info className="h-8 w-8 mx-auto mb-2 opacity-50" />
                  <p>Erro ao carregar detalhes da sessão</p>
                </div>
              </div>
            )}
          </div>
        </div>

        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        {/* Progress Indicator removido (Apply Directly feature removida) */}

        <DialogFooter className="flex gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loadingSession}>
            Cancelar
          </Button>
          <Button
            onClick={handleLoadSession}
            disabled={!selectedSessionDetails || loadingSession || loadingDetails}
          >
            {loadingSession && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            💾 Carregar no Staging
          </Button>
        </DialogFooter>
      </DialogContent>
      
      {/* Dialog de Confirmação de Deleção */}
      <AlertDialog open={!!sessionToDelete} onOpenChange={(open) => !open && setSessionToDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Confirmar Exclusão</AlertDialogTitle>
            <AlertDialogDescription>
              Tem certeza que deseja deletar a sessão <strong>"{sessionToDelete?.name}"</strong>?
              Esta ação não pode ser desfeita.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isDeleting}>Cancelar</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDeleteSession}
              disabled={isDeleting}
              className="bg-destructive hover:bg-destructive/90"
            >
              {isDeleting && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Deletar
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      
      {/* Dialog de Renomear */}
      <Dialog open={!!sessionToRename} onOpenChange={(open) => !open && setSessionToRename(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Renomear Sessão</DialogTitle>
            <DialogDescription>
              Digite o novo nome para a sessão "{sessionToRename?.name}"
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Input
              value={newSessionName}
              onChange={(e) => setNewSessionName(e.target.value)}
              placeholder="Nome da sessão"
              disabled={isRenaming}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setSessionToRename(null)} disabled={isRenaming}>
              Cancelar
            </Button>
            <Button
              onClick={handleRenameSession}
              disabled={!newSessionName.trim() || isRenaming || newSessionName === sessionToRename?.name}
            >
              {isRenaming && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              Renomear
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      
      {/* Modal de Edição de Conteúdo */}
      <EditSessionModal
        session={sessionToEdit}
        open={!!sessionToEdit}
        onOpenChange={(open) => {
          if (!open) setSessionToEdit(null);
        }}
        onSave={() => {
          loadSessions(); // Recarregar lista após salvar
          setSessionToEdit(null);
        }}
      />
    </Dialog>
  );
}