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
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Loader2, Save, Folder, FileText, Hash } from 'lucide-react';
import { toast } from 'sonner';

import { useSessionTemplates, useSaveSession } from '@/hooks/useSessions';
import { useStaging } from '@/contexts/StagingContext';
import { useTabManager } from '@/contexts/TabContext';
import type { SessionTemplate, HPA, NodePool } from '@/lib/api/types';

interface SaveSessionModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSuccess?: () => void;
}

// Folders disponíveis (compatível com TUI)
const SESSION_FOLDERS = [
  {
    name: 'HPA-Upscale',
    description: 'HPA scale up sessions',
    icon: '📈',
  },
  {
    name: 'HPA-Downscale', 
    description: 'HPA scale down sessions',
    icon: '📉',
  },
  {
    name: 'Node-Upscale',
    description: 'Node pool scale up sessions', 
    icon: '🚀',
  },
  {
    name: 'Node-Downscale',
    description: 'Node pool scale down sessions',
    icon: '⬇️',
  },
  {
    name: 'Rollback',
    description: 'Rollback sessions',
    icon: '⏪',
  },
];

export function SaveSessionModal({ open, onOpenChange, onSuccess }: SaveSessionModalProps) {
  const [selectedFolder, setSelectedFolder] = useState<string>('');
  const [selectedTemplate, setSelectedTemplate] = useState<string>('');
  const [sessionName, setSessionName] = useState<string>('');
  const [description, setDescription] = useState<string>('');
  const [customAction, setCustomAction] = useState<string>('');
  const [saveMode, setSaveMode] = useState<'staging' | 'snapshot'>('staging');
  const [allowCustomName, setAllowCustomName] = useState<boolean>(false);
  const [capturingSnapshot, setCapturingSnapshot] = useState<boolean>(false);

  const { data: templates = [], isLoading: loadingTemplates } = useSessionTemplates();
  const { mutate: saveSession, isPending: saving, error: saveError } = useSaveSession();
  const staging = useStaging();
  const { getActiveTab } = useTabManager();
  const activeTab = getActiveTab();
  // Para snapshot, APENAS usar pageState.selectedCluster (não usar clusterContext "default" como fallback)
  const selectedCluster = activeTab?.pageState?.selectedCluster || '';
  const selectedNamespace = activeTab?.pageState?.selectedNamespace || '';

  const changesCount = staging.getChangesCount();
  const hasChanges = changesCount.total > 0;

  // Debug: Log do estado da aba quando modal abrir
  useEffect(() => {
    if (open) {
      console.log('[SaveSessionModal] Modal aberto - Estado da aba:', {
        activeTab: activeTab?.id,
        selectedCluster,
        selectedNamespace,
        pageState: activeTab?.pageState,
        clusterContext: activeTab?.clusterContext,
        hasChanges,
      });
    }
  }, [open, activeTab, selectedCluster, selectedNamespace, hasChanges]);

  // Detectar modo automaticamente baseado nas alterações
  useEffect(() => {
    if (open) {
      setSaveMode(hasChanges ? 'staging' : 'snapshot');
    }
  }, [open, hasChanges]);

  // Resetar form quando modal abrir
  useEffect(() => {
    if (open) {
      setSelectedFolder('');
      setSelectedTemplate('');
      setSessionName('');
      setDescription('');
      setCustomAction('');
      setAllowCustomName(false);
    }
  }, [open]);

  // Gerar nome da sessão automaticamente quando template mudar (mas permitir edição)
  useEffect(() => {
    if (selectedTemplate && !allowCustomName) {
      const template = templates.find(t => t.pattern === selectedTemplate);
      if (template) {
        const generatedName = generateSessionName(template, customAction, changesCount, saveMode);
        setSessionName(generatedName);
      }
    }
  }, [selectedTemplate, customAction, templates, changesCount, saveMode, allowCustomName]);

  const handleSave = async () => {
    if (!sessionName.trim() || !selectedFolder) {
      return;
    }

    let sessionData;

    if (saveMode === 'staging' && hasChanges) {
      // Modo staging: salvar alterações pendentes
      sessionData = staging.getSessionData();
    } else {
      // Modo snapshot: capturar estado atual para rollback (buscar dados frescos do cluster)
      const snapshotData = await fetchClusterDataForSnapshot();
      if (!snapshotData) {
        return; // Erro já tratado em fetchClusterDataForSnapshot
      }
      sessionData = snapshotData;
    }
    
    saveSession({
      name: sessionName.trim(),
      folder: selectedFolder,
      description: description.trim(),
      template: selectedTemplate || 'custom',
      changes: sessionData.changes,
      node_pool_changes: sessionData.node_pool_changes,
    }, {
      onSuccess: () => {
        onOpenChange(false);
        onSuccess?.();
      },
    });
  };

  // Função para buscar dados frescos do cluster para snapshot
  const fetchClusterDataForSnapshot = async () => {
    // Validar se há cluster selecionado e se não é o placeholder "default"
    if (!selectedCluster || selectedCluster === 'default') {
      console.error('[fetchClusterDataForSnapshot] Cluster inválido:', {
        selectedCluster,
        activeTab,
        pageState: activeTab?.pageState,
        clusterContext: activeTab?.clusterContext
      });
      toast.error('Por favor, selecione um cluster válido antes de capturar o snapshot');
      return null;
    }

    console.log('[fetchClusterDataForSnapshot] Capturando snapshot do cluster:', selectedCluster);
    setCapturingSnapshot(true);

    try {
      // Buscar HPAs de TODOS os namespaces (snapshot deve capturar tudo)
      const hpaUrl = `/api/v1/hpas?cluster=${encodeURIComponent(selectedCluster)}`;
      console.log('[fetchClusterDataForSnapshot] Buscando HPAs:', hpaUrl);

      const hpaResponse = await fetch(hpaUrl, {
        headers: { 'Authorization': `Bearer ${localStorage.getItem("auth_token") || ""}` }
      });

      if (!hpaResponse.ok) {
        throw new Error(`Erro ao buscar HPAs: ${hpaResponse.statusText}`);
      }

      const hpaData = await hpaResponse.json();
      const hpas: HPA[] = hpaData.data || [];

      // Buscar Node Pools
      const npUrl = `/api/v1/nodepools?cluster=${encodeURIComponent(selectedCluster)}`;
      const npResponse = await fetch(npUrl, {
        headers: { 'Authorization': `Bearer ${localStorage.getItem("auth_token") || ""}` }
      });

      if (!npResponse.ok) {
        throw new Error(`Erro ao buscar Node Pools: ${npResponse.statusText}`);
      }

      const npData = await npResponse.json();
      const nodePools: NodePool[] = npData.data || [];

      // Transformar HPAs para formato de sessão
      const hpaChanges = hpas.map(hpa => ({
        cluster: hpa.cluster,
        namespace: hpa.namespace,
        hpa_name: hpa.name,
        original_values: {
          min_replicas: hpa.min_replicas ?? undefined,
          max_replicas: hpa.max_replicas,
          target_cpu: hpa.target_cpu ?? undefined,
          target_memory: hpa.target_memory ?? undefined,
          cpu_request: hpa.target_cpu_request,
          cpu_limit: hpa.target_cpu_limit,
          memory_request: hpa.target_memory_request,
          memory_limit: hpa.target_memory_limit,
        },
        new_values: {
          min_replicas: hpa.min_replicas ?? undefined,
          max_replicas: hpa.max_replicas,
          target_cpu: hpa.target_cpu ?? undefined,
          target_memory: hpa.target_memory ?? undefined,
          cpu_request: hpa.target_cpu_request,
          cpu_limit: hpa.target_cpu_limit,
          memory_request: hpa.target_memory_request,
          memory_limit: hpa.target_memory_limit,
          perform_rollout: false,
          perform_daemonset_rollout: false,
          perform_statefulset_rollout: false,
        },
      }));

      // Transformar Node Pools para formato de sessão
      const nodePoolChanges = nodePools.map(nodePool => ({
        cluster: nodePool.cluster_name,
        node_pool_name: nodePool.name,
        resource_group: nodePool.resource_group,
        subscription: nodePool.subscription,
        original_values: {
          node_count: nodePool.node_count,
          autoscaling_enabled: nodePool.autoscaling_enabled,
          min_node_count: nodePool.min_node_count,
          max_node_count: nodePool.max_node_count,
        },
        new_values: {
          node_count: nodePool.node_count,
          autoscaling_enabled: nodePool.autoscaling_enabled,
          min_node_count: nodePool.min_node_count,
          max_node_count: nodePool.max_node_count,
        },
      }));

      toast.success(`Snapshot capturado: ${hpas.length} HPAs, ${nodePools.length} Node Pools`);

      return {
        changes: hpaChanges,
        node_pool_changes: nodePoolChanges,
      };
    } catch (error) {
      console.error('Erro ao capturar snapshot:', error);
      toast.error(error instanceof Error ? error.message : 'Erro ao capturar snapshot do cluster');
      return null;
    } finally {
      setCapturingSnapshot(false);
    }
  };

  // Obter preview das alterações que serão salvas
  const getChangesPreview = () => {
    if (saveMode === 'staging' && hasChanges) {
      const sessionData = staging.getSessionData();
      return {
        changes: sessionData.changes,
        node_pool_changes: sessionData.node_pool_changes
      };
    }
    return null;
  };

  const changesPreview = getChangesPreview();

  // Validação mais flexível - só requer nome e pasta
  const isValid = sessionName.trim() && selectedFolder;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Save className="h-5 w-5" />
            Salvar Sessão
          </DialogTitle>
          <DialogDescription>
            {saveMode === 'staging' ? (
              <>
                Salve suas alterações pendentes em uma sessão.
                {changesCount.total > 0 && (
                  <span className="block mt-1 text-sm font-medium text-blue-600">
                    📊 {changesCount.hpas} HPAs + {changesCount.nodePools} Node Pools = {changesCount.total} alterações pendentes
                  </span>
                )}
              </>
            ) : (
              <>
                Capture o estado atual como snapshot para rollback futuro.
                <span className="block mt-1 text-sm font-medium text-green-600">
                  📸 Salvando configurações atuais como backup
                </span>
              </>
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4">
          {/* ✅ MANTER: Preview das alterações que serão salvas */}
          {changesPreview && (
            <Alert className="bg-muted/50 dark:bg-muted/20 border-primary/20">
              <AlertDescription>
                <strong>📋 Alterações Pendentes:</strong><br/>
                {changesPreview.changes.length > 0 && (
                  <>
                    <strong>HPAs ({changesPreview.changes.length}):</strong><br/>
                    {changesPreview.changes.map((change, i) => (
                      <div key={i} className="ml-2 text-xs">
                        • {change.namespace}/{change.hpa_name}: min {change.original_values?.min_replicas} → {change.new_values?.min_replicas}, max {change.original_values?.max_replicas} → {change.new_values?.max_replicas}
                        {/* Exibir informações de rollout quando ativo */}
                        {(change.new_values?.perform_rollout || 
                          change.new_values?.perform_daemonset_rollout || 
                          change.new_values?.perform_statefulset_rollout) && (
                          <div className="ml-2 text-xs text-orange-600 font-medium">
                            🔄 Rollout: {[
                              change.new_values?.perform_rollout && 'Deployment',
                              change.new_values?.perform_daemonset_rollout && 'DaemonSet', 
                              change.new_values?.perform_statefulset_rollout && 'StatefulSet'
                            ].filter(Boolean).join(', ')}
                            {change.new_values?.deployment_name && ` (${change.new_values.deployment_name})`}
                          </div>
                        )}
                      </div>
                    ))}
                  </>
                )}
                {changesPreview.node_pool_changes.length > 0 && (
                  <>
                    <strong>Node Pools ({changesPreview.node_pool_changes.length}):</strong><br/>
                    {changesPreview.node_pool_changes.map((change, i) => (
                      <div key={i} className="ml-2 text-xs">
                        • {change.node_pool_name}: count {change.original_values.node_count} → {change.new_values.node_count}
                      </div>
                    ))}
                  </>
                )}
              </AlertDescription>
            </Alert>
          )}

          {/* Seletor de Modo */}
          <div className="space-y-2">
            <Label className="text-sm font-medium">Tipo de Sessão</Label>
            <div className="grid grid-cols-2 gap-2">
              <Button
                variant={saveMode === 'staging' ? 'default' : 'outline'}
                size="sm"
                onClick={() => setSaveMode('staging')}
                disabled={!hasChanges}
                className="justify-start"
              >
                📝 Alterações Pendentes
                {hasChanges && <span className="ml-1 text-xs">({changesCount.total})</span>}
              </Button>
              <Button
                variant={saveMode === 'snapshot' ? 'default' : 'outline'}
                size="sm"
                onClick={() => setSaveMode('snapshot')}
                className="justify-start"
              >
                📸 Snapshot/Rollback
              </Button>
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {/* Seleção de Pasta */}
            <div className="space-y-2">
              <Label htmlFor="folder" className="flex items-center gap-2">
                <Folder className="h-4 w-4" />
                Pasta de Destino *
              </Label>
              <Select value={selectedFolder} onValueChange={setSelectedFolder}>
                <SelectTrigger>
                  <SelectValue placeholder="Selecione a pasta..." />
                </SelectTrigger>
                <SelectContent>
                  {SESSION_FOLDERS.map((folder) => (
                    <SelectItem key={folder.name} value={folder.name}>
                      <div className="flex items-center gap-2">
                        <span>{folder.icon}</span>
                        <div>
                          <div className="font-medium">{folder.name}</div>
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

            {/* Seleção de Template (OPCIONAL) */}
            <div className="space-y-2">
              <Label htmlFor="template" className="flex items-center gap-2">
                <FileText className="h-4 w-4" />
                Template de Nome (Opcional)
              </Label>
              <Select 
                value={selectedTemplate} 
                onValueChange={(value) => {
                  setSelectedTemplate(value);
                  setAllowCustomName(false); // Reset custom name flag
                }}
                disabled={loadingTemplates}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Escolha um template ou digite nome customizado abaixo" />
                </SelectTrigger>
                <SelectContent>
                  {templates.map((template) => (
                    <SelectItem key={template.pattern} value={template.pattern}>
                      <div>
                        <div className="font-medium">{template.name}</div>
                        <div className="text-xs text-muted-foreground">
                          {template.example}
                        </div>
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          {/* Ação Customizada (se template usar {action}) */}
          {selectedTemplate?.includes('{action}') && (
            <div className="space-y-2">
              <Label htmlFor="action" className="flex items-center gap-2">
                <Hash className="h-4 w-4" />
                Ação Customizada
              </Label>
              <Input
                id="action"
                value={customAction}
                onChange={(e) => setCustomAction(e.target.value)}
                placeholder={saveMode === 'snapshot' ? "Ex: backup-pre-change, rollback-point" : "Ex: Emergency-scale, Stress-test"}
              />
            </div>
          )}

          {/* Nome da Sessão (SEMPRE EDITÁVEL) */}
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <Label htmlFor="name">Nome da Sessão *</Label>
              {selectedTemplate && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setAllowCustomName(true);
                    setSessionName('');
                  }}
                >
                  ✏️ Nome Customizado
                </Button>
              )}
            </div>
            <Input
              id="name"
              value={sessionName}
              onChange={(e) => {
                setSessionName(e.target.value);
                setAllowCustomName(true);
              }}
              placeholder="Digite o nome da sessão..."
              className="font-mono"
            />
            {selectedTemplate && !allowCustomName && (
              <p className="text-xs text-muted-foreground">
                💡 Nome gerado pelo template. Clique "Nome Customizado" para editar livremente.
              </p>
            )}
          </div>

          {/* Descrição Opcional */}
          <div className="space-y-2">
            <Label htmlFor="description">Descrição (Opcional)</Label>
            <Textarea
              id="description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={
                saveMode === 'snapshot' 
                  ? "Ex: Backup antes de mudanças de produção..."
                  : "Descreva o propósito desta sessão..."
              }
              rows={2}
            />
          </div>

          {/* Preview */}
          <div className="p-3 bg-muted rounded-md">
            <div className="text-sm font-medium mb-1">📂 Destino Final:</div>
            <div className="text-sm text-muted-foreground font-mono">
              ~/.k8s-hpa-manager/sessions/{selectedFolder || '[pasta]'}/{sessionName || '[nome]'}.json
            </div>
            <div className="text-xs text-muted-foreground mt-1">
              Modo: {saveMode === 'snapshot' ? '📸 Snapshot/Rollback' : '📝 Alterações Pendentes'}
            </div>
          </div>
        </div>

        {saveError && (
          <Alert variant="destructive">
            <AlertDescription>
              Erro ao salvar sessão: {saveError.message}
            </AlertDescription>
          </Alert>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving || capturingSnapshot}>
            Cancelar
          </Button>
          <Button onClick={handleSave} disabled={!isValid || saving || capturingSnapshot}>
            {(saving || capturingSnapshot) && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {saveMode === 'snapshot' ? 'Capturar Snapshot' : 'Salvar Sessão'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// Função para gerar nome da sessão (compatível com TUI)
function generateSessionName(
  template: SessionTemplate,
  customAction: string,
  changesCount: { hpas: number; nodePools: number; total: number },
  saveMode: 'staging' | 'snapshot'
): string {
  let name = template.pattern;

  // Substituir variáveis (MESMA lógica do TUI)
  const now = new Date();
  const timestamp = formatDate(now, 'dd-mm-yy_hh:mm:ss');
  const date = formatDate(now, 'dd-mm-yy');
  const time = formatDate(now, 'hh:mm:ss');
  const user = 'web-user'; // Usuário web

  name = name.replace('{action}', customAction || 'Web-session');
  name = name.replace('{timestamp}', timestamp);
  name = name.replace('{date}', date);
  name = name.replace('{time}', time);
  name = name.replace('{user}', user);
  name = name.replace('{hpa_count}', changesCount.hpas.toString());
  name = name.replace('{cluster}', 'multi-cluster'); // Simplificação para web
  name = name.replace('{env}', 'web'); // Ambiente web

  return name;
}

function formatDate(date: Date, format: string): string {
  const day = date.getDate().toString().padStart(2, '0');
  const month = (date.getMonth() + 1).toString().padStart(2, '0');
  const year = date.getFullYear().toString().slice(-2);
  const hours = date.getHours().toString().padStart(2, '0');
  const minutes = date.getMinutes().toString().padStart(2, '0');
  const seconds = date.getSeconds().toString().padStart(2, '0');

  return format
    .replace('dd', day)
    .replace('mm', month)
    .replace('yy', year)
    .replace('hh', hours)
    .replace('mm', minutes)
    .replace('ss', seconds);
}
