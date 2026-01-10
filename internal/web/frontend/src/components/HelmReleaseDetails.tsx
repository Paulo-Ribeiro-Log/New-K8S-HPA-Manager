import { useState, useEffect } from 'react';
import { useHelmStore } from '../store/helmStore.tsx';
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs';
import { Card } from './ui/card';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import {
  Loader2,
  FileCode,
  History,
  Settings,
  AlertCircle,
  Package,
  Download,
  Upload,
  RotateCcw,
  Trash2,
  MoreVertical,
  Plus,
  CheckCircle2,
  RotateCw,
  AlertTriangle,
  Undo2,
  Redo2,
  Maximize2,
  GitCompare,
  X,
} from 'lucide-react';
import { cn } from '../lib/utils';
import { MonacoYamlEditor } from './MonacoYamlEditor';
import { HelmUpgradeModal } from './HelmUpgradeModal.tsx';
import { HelmRollbackModal, HelmUninstallModal } from './HelmActionModals.tsx';
import { ApplyValuesModal } from './ApplyValuesModal';
import yaml from 'js-yaml';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from './ui/dropdown-menu';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';

interface HelmReleaseDetailsProps {
  cluster: string;
  release: string | null;
  onInstallClick?: () => void;
  onRefreshNeeded?: () => void;
}

export const HelmReleaseDetails = ({ cluster, release, onInstallClick, onRefreshNeeded }: HelmReleaseDetailsProps) => {
  const {
    releaseDetail,
    releaseDetailLoading,
    releaseDetailError,
    revisions,
    revisionsLoading,
  } = useHelmStore();

  const [activeTab, setActiveTab] = useState('values');
  const [showUpgradeModal, setShowUpgradeModal] = useState(false);
  const [showRollbackModal, setShowRollbackModal] = useState(false);
  const [showUninstallModal, setShowUninstallModal] = useState(false);

  if (!release) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="flex flex-col items-center gap-2 text-center">
          <Package className="h-12 w-12 text-muted-foreground/50" />
          <p className="text-sm font-medium text-muted-foreground">
            Selecione um release
          </p>
          <p className="text-xs text-muted-foreground">
            Escolha um release da lista para visualizar os detalhes
          </p>
        </div>
      </div>
    );
  }

  if (releaseDetailLoading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="flex flex-col items-center gap-2">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
          <p className="text-sm text-muted-foreground">Carregando detalhes...</p>
        </div>
      </div>
    );
  }

  if (releaseDetailError) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="flex flex-col items-center gap-3 text-center max-w-md">
          <AlertCircle className="h-12 w-12 text-destructive" />
          <div>
            <p className="text-sm font-medium text-destructive mb-1">Failed to load release</p>
            <p className="text-xs text-muted-foreground">{releaseDetailError}</p>
          </div>
          {releaseDetailError.includes('not found') && (
            <p className="text-xs text-muted-foreground mt-2">
              This release may have been uninstalled. Try refreshing the list.
            </p>
          )}
        </div>
      </div>
    );
  }

  if (!releaseDetail) {
    return null;
  }

  return (
    <div className="flex flex-col h-full gap-4">
      {/* Header with actions menu */}
      <div className="flex items-center justify-between gap-2 pb-2 border-b">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <h3 className="text-lg font-semibold truncate">{releaseDetail.name}</h3>
            <Badge className={getStatusColor(releaseDetail.status)}>
              {releaseDetail.status}
            </Badge>
          </div>
          <div className="flex flex-wrap items-center gap-3 mt-2 text-xs text-muted-foreground">
            <div className="flex items-center gap-1">
              <Package className="h-3.5 w-3.5" />
              <span className="font-medium">Chart:</span>
              <span className="text-foreground">{releaseDetail.chart}</span>
            </div>
            <div className="flex items-center gap-1">
              <span className="font-medium">App:</span>
              <span className="text-foreground">{releaseDetail.appVersion}</span>
            </div>
            <div className="flex items-center gap-1">
              <span className="font-medium">Revisão:</span>
              <span className="text-foreground">#{releaseDetail.revision}</span>
            </div>
            <div className="flex items-center gap-1">
              <span className="font-medium">Namespace:</span>
              <span className="text-foreground">{releaseDetail.namespace}</span>
            </div>
            {releaseDetail.chartMetadata?.sources && releaseDetail.chartMetadata.sources.length > 0 && (
              <div className="flex items-center gap-1">
                <span className="font-medium">Source:</span>
                <a 
                  href={releaseDetail.chartMetadata.sources[0]} 
                  target="_blank" 
                  rel="noopener noreferrer"
                  className="text-blue-500 hover:underline"
                >
                  {new URL(releaseDetail.chartMetadata.sources[0]).hostname}
                </a>
              </div>
            )}
            {releaseDetail.chartMetadata?.home && (
              <div className="flex items-center gap-1">
                <span className="font-medium">Home:</span>
                <a 
                  href={releaseDetail.chartMetadata.home} 
                  target="_blank" 
                  rel="noopener noreferrer"
                  className="text-blue-500 hover:underline"
                >
                  {new URL(releaseDetail.chartMetadata.home).hostname}
                </a>
              </div>
            )}
          </div>
        </div>
        
        {/* Actions Menu */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="h-8 w-8">
              <MoreVertical className="h-4 w-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuLabel>Ações</DropdownMenuLabel>
            <DropdownMenuSeparator />
            {onInstallClick && (
              <>
                <DropdownMenuItem onClick={onInstallClick}>
                  <Plus className="h-4 w-4 mr-2" />
                  Install
                </DropdownMenuItem>
                <DropdownMenuSeparator />
              </>
            )}
            <DropdownMenuItem onClick={() => setShowUpgradeModal(true)}>
              <Upload className="h-4 w-4 mr-2" />
              Upgrade
            </DropdownMenuItem>
            <DropdownMenuItem 
              onClick={() => setShowRollbackModal(true)}
              disabled={!revisions || revisions.length <= 1}
            >
              <RotateCcw className="h-4 w-4 mr-2" />
              Rollback
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem 
              onClick={() => setShowUninstallModal(true)}
              className="text-destructive focus:text-destructive"
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Uninstall
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Export Values Button */}
      <div className="flex items-center gap-2">
        <Button 
          size="sm" 
          variant="outline" 
          className="gap-2"
          onClick={() => {
            const blob = new Blob([releaseDetail.valuesRaw], { type: 'text/yaml' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `${releaseDetail.name}-values.yaml`;
            a.click();
            URL.revokeObjectURL(url);
          }}
        >
          <Download className="h-4 w-4" />
          Export Values
        </Button>
      </div>

      {/* Modals */}
      <HelmUpgradeModal
        open={showUpgradeModal}
        onOpenChange={setShowUpgradeModal}
        release={releaseDetail}
        cluster={cluster}
        onSuccess={() => {
          // Trigger refresh without reloading page
          onRefreshNeeded?.();
        }}
      />

      <HelmRollbackModal
        open={showRollbackModal}
        onOpenChange={setShowRollbackModal}
        releaseName={releaseDetail.name}
        namespace={releaseDetail.namespace}
        cluster={cluster}
        revisions={revisions}
        currentRevision={releaseDetail.revision}
        onSuccess={() => {
          // Trigger refresh without reloading page
          onRefreshNeeded?.();
        }}
      />

      <HelmUninstallModal
        open={showUninstallModal}
        onOpenChange={setShowUninstallModal}
        releaseName={releaseDetail.name}
        namespace={releaseDetail.namespace}
        cluster={cluster}
        onSuccess={() => {
          // Trigger refresh and deselect without reloading page
          onRefreshNeeded?.();
        }}
      />

      {/* Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab} className="flex-1 flex flex-col min-h-0">
        <TabsList className="grid w-full grid-cols-3">
          <TabsTrigger value="values" className="gap-2">
            <Settings className="h-4 w-4" />
            Valores
          </TabsTrigger>
          <TabsTrigger value="history" className="gap-2">
            <History className="h-4 w-4" />
            Histórico
          </TabsTrigger>
          <TabsTrigger value="manifest" className="gap-2">
            <FileCode className="h-4 w-4" />
            Manifest
          </TabsTrigger>
        </TabsList>

        <TabsContent value="values" className="flex-1 overflow-auto mt-4">
          <ValuesTab
            valuesRaw={releaseDetail.valuesRaw}
            valuesRendered={releaseDetail.valuesRendered}
            releaseName={releaseDetail.name}
            namespace={releaseDetail.namespace}
            cluster={cluster}
            chart={releaseDetail.chart}
            onApplySuccess={() => {
              // Trigger refresh without reloading page
              onRefreshNeeded?.();
            }}
          />
        </TabsContent>

        <TabsContent value="history" className="flex-1 overflow-auto mt-4">
          <HistoryTab
            revisions={revisions}
            loading={revisionsLoading}
            currentRevision={releaseDetail.revision}
          />
        </TabsContent>

        <TabsContent value="manifest" className="flex-1 overflow-auto mt-4">
          <ManifestTab
            manifest={releaseDetail.manifest}
            notes={releaseDetail.notes}
            releaseName={releaseDetail.name}
            namespace={releaseDetail.namespace}
            cluster={cluster}
            chart={releaseDetail.chart}
            onApplySuccess={() => {
              // Trigger refresh without reloading page
              onRefreshNeeded?.();
            }}
          />
        </TabsContent>
      </Tabs>
    </div>
  );
};

// Values Tab Component
const ValuesTab = ({ 
  valuesRaw, 
  valuesRendered,
  releaseName,
  namespace,
  cluster,
  chart,
  onApplySuccess,
}: { 
  valuesRaw: string; 
  valuesRendered: string;
  releaseName: string;
  namespace: string;
  cluster: string;
  chart: string;
  onApplySuccess: () => void;
}) => {
  const [showRendered, setShowRendered] = useState(false);
  const [editedValue, setEditedValue] = useState(valuesRaw);
  const [originalValue, setOriginalValue] = useState(valuesRaw);
  const [savedRawValue, setSavedRawValue] = useState(valuesRaw);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [isValidating, setIsValidating] = useState(false);
  const [validationSuccess, setValidationSuccess] = useState(false);
  const [showApplyModal, setShowApplyModal] = useState(false);
  const [viewMode, setViewMode] = useState<'editor' | 'diff'>('editor');
  const [editorFullScreen, setEditorFullScreen] = useState(false);
  
  // History management
  const [history, setHistory] = useState<string[]>([valuesRaw]);
  const [historyIndex, setHistoryIndex] = useState(0);

  // Sync with props when changed
  useEffect(() => {
    setEditedValue(valuesRaw);
    setOriginalValue(valuesRaw);
    setSavedRawValue(valuesRaw);
    setHistory([valuesRaw]);
    setHistoryIndex(0);
    setValidationError(null);
  }, [valuesRaw]);

  const hasChanges = editedValue !== originalValue;
  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < history.length - 1;

  // Debug log
  useEffect(() => {
    console.log('🔍 Values Debug:', {
      hasChanges,
      editedLength: editedValue.length,
      originalLength: originalValue.length,
      areEqual: editedValue === originalValue
    });
  }, [editedValue, originalValue, hasChanges]);

  const handleEditorChange = (value: string | undefined) => {
    if (showRendered) return;
    
    const newValue = value || '';
    setEditedValue(newValue);
    setValidationError(null);

    // Add to history
    setHistoryIndex((currentIndex) => {
      setHistory((currentHistory) => {
        const newHistory = currentHistory.slice(0, currentIndex + 1);
        if (newHistory[newHistory.length - 1] !== newValue) {
          newHistory.push(newValue);
          if (newHistory.length > 50) {
            newHistory.shift();
            return newHistory;
          }
        }
        return newHistory;
      });
      return Math.min(currentIndex + 1, 49);
    });
  };

  const handleUndo = () => {
    if (historyIndex > 0) {
      const newIndex = historyIndex - 1;
      setHistoryIndex(newIndex);
      setEditedValue(history[newIndex]);
    }
  };

  const handleRedo = () => {
    if (historyIndex < history.length - 1) {
      const newIndex = historyIndex + 1;
      setHistoryIndex(newIndex);
      setEditedValue(history[newIndex]);
    }
  };

  const handleToggleView = (mode: 'editor' | 'diff') => {
    if (mode === 'diff' && !hasChanges) return;
    setViewMode(mode);
  };

  const validateYaml = () => {
    console.log('🔍 validateYaml chamado!');
    setIsValidating(true);
    setValidationError(null);
    setValidationSuccess(false);

    try {
      yaml.load(editedValue);
      setValidationError(null);
      console.log('✅ YAML válido!');
      setValidationSuccess(true);
      // Reset after 2 seconds
      setTimeout(() => {
        setValidationSuccess(false);
      }, 2000);
      return true;
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Invalid YAML';
      console.log('❌ YAML inválido:', message);
      setValidationError(message);
      return false;
    } finally {
      setIsValidating(false);
    }
  };

  const handleApply = () => {
    if (validateYaml()) {
      setShowApplyModal(true);
    }
  };

  const handleCancel = () => {
    setEditedValue(originalValue);
    setHistory([originalValue]);
    setHistoryIndex(0);
    setValidationError(null);
    setViewMode('editor');
  };

  const editorContent = (
    <div className="space-y-3">
      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <p className="text-sm font-medium">Valores do Release</p>
          <div className="flex items-center gap-2">
            {!showRendered && (
              <>
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={handleUndo}
                    disabled={!canUndo}
                    className={`px-2 py-1 text-xs font-medium ${
                      canUndo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                    }`}
                    title="Desfazer (Ctrl+Z)"
                  >
                    <Undo2 className="w-3.5 h-3.5" />
                  </button>
                  <button
                    type="button"
                    onClick={handleRedo}
                    disabled={!canRedo}
                    className={`px-2 py-1 text-xs font-medium border-l border-border/50 ${
                      canRedo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                    }`}
                    title="Refazer (Ctrl+Y)"
                  >
                    <Redo2 className="w-3.5 h-3.5" />
                  </button>
                </div>
                <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                  <button
                    type="button"
                    onClick={() => handleToggleView('editor')}
                    className={`px-3 py-1 text-xs font-medium ${
                      viewMode === 'editor' ? "bg-primary text-white" : "bg-background text-muted-foreground"
                    }`}
                  >
                    Editor
                  </button>
                  <button
                    type="button"
                    onClick={() => handleToggleView('diff')}
                    className={`px-3 py-1 text-xs font-medium ${
                      viewMode === 'diff' ? "bg-primary text-white" : "bg-background text-muted-foreground"
                    } ${hasChanges ? "" : "opacity-50 cursor-not-allowed"}`}
                    disabled={!hasChanges}
                  >
                    Diff
                  </button>
                </div>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setEditorFullScreen(true)}
                  title="Abrir editor em tela cheia"
                >
                  <Maximize2 className="w-3.5 h-3.5" />
                </Button>
              </>
            )}
            <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
              <button
                type="button"
                onClick={() => {
                  setSavedRawValue(editedValue);
                  setShowRendered(false);
                  setEditedValue(savedRawValue);
                }}
                className={`px-3 py-1 text-xs font-medium ${
                  !showRendered ? "bg-primary text-white" : "bg-background text-muted-foreground"
                }`}
              >
                Raw
              </button>
              <button
                type="button"
                onClick={() => {
                  setSavedRawValue(editedValue);
                  setShowRendered(true);
                }}
                className={`px-3 py-1 text-xs font-medium ${
                  showRendered ? "bg-primary text-white" : "bg-background text-muted-foreground"
                }`}
              >
                Renderizado
              </button>
            </div>
          </div>
        </div>

        {/* Validation Error */}
        {validationError && (
          <div className="flex items-start gap-2 p-3 bg-destructive/10 border border-destructive/20 rounded-lg">
            <AlertTriangle className="h-4 w-4 text-destructive mt-0.5" />
            <div className="flex-1">
              <p className="text-sm font-medium text-destructive">Erro de validação YAML</p>
              <p className="text-xs text-destructive/80 mt-1">{validationError}</p>
            </div>
          </div>
        )}

        {viewMode === 'editor' && (
          <div className="border rounded-lg overflow-hidden" style={{ height: editorFullScreen ? 'calc(100vh - 200px)' : '520px' }}>
            <MonacoYamlEditor
              value={showRendered ? valuesRendered || '# Nenhum valor renderizado' : editedValue || '# Nenhum valor customizado'}
              readOnly={showRendered}
              onChange={showRendered ? undefined : handleEditorChange}
              height={editorFullScreen ? window.innerHeight - 200 : 520}
            />
          </div>
        )}
        {viewMode === 'diff' && (
          <div className="border rounded-lg overflow-hidden" style={{ height: editorFullScreen ? 'calc(100vh - 200px)' : '520px' }}>
            <MonacoYamlEditor
              mode="diff"
              originalValue={originalValue}
              value={editedValue}
              height={editorFullScreen ? window.innerHeight - 200 : 520}
              readOnly
            />
          </div>
        )}
      </div>

      {/* Action Buttons */}
      {!showRendered && (
        <div className="flex flex-wrap gap-2">
          <Button
            size="sm"
            variant={validationSuccess ? "default" : (hasChanges ? "default" : "outline")}
            onClick={() => {
              console.log('🖱️ Botão Validar clicado!');
              validateYaml();
            }}
            disabled={isValidating || validationSuccess}
            className={`gap-1 transition-all duration-300 ${
              validationSuccess 
                ? 'bg-green-600 hover:bg-green-600 text-white' 
                : hasChanges 
                  ? 'bg-blue-600 hover:bg-blue-700 text-white' 
                  : ''
            }`}
          >
            {isValidating ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : validationSuccess ? (
              <CheckCircle2 className="h-3 w-3" />
            ) : (
              <CheckCircle2 className="h-3 w-3" />
            )}
            {validationSuccess ? 'Válido ✓' : `Validar${hasChanges ? ' ●' : ''}`}
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={handleCancel}
            disabled={!hasChanges}
            className="gap-1"
          >
            <X className="h-3 w-3" />
            Cancelar
          </Button>
          <Button
            size="sm"
            onClick={handleApply}
            disabled={!hasChanges}
            className="gap-1"
          >
            <Upload className="h-3 w-3" />
            Aplicar
          </Button>
        </div>
      )}

      {/* Apply Confirmation Modal */}
      <ApplyValuesModal
        open={showApplyModal}
        onOpenChange={setShowApplyModal}
        releaseName={releaseName}
        namespace={namespace}
        cluster={cluster}
        chart={chart}
        originalValues={originalValue}
        newValues={editedValue}
        onSuccess={() => {
          onApplySuccess();
          setShowApplyModal(false);
        }}
      />
    </div>
  );

  // Fullscreen Dialog
  if (editorFullScreen) {
    return (
      <Dialog open={editorFullScreen} onOpenChange={setEditorFullScreen}>
        <DialogContent className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none">
          <DialogHeader className="border-b border-border pb-4">
            <DialogTitle className="text-xl font-semibold text-primary">
              Editor de Valores - {releaseName}
            </DialogTitle>
            <DialogDescription className="text-sm text-muted-foreground">
              {namespace} • Modo tela cheia
            </DialogDescription>
          </DialogHeader>
          <div className="flex-1 overflow-auto">
            {editorContent}
          </div>
        </DialogContent>
      </Dialog>
    );
  }

  return editorContent;
};

// History Tab Component
const HistoryTab = ({
  revisions,
  loading,
  currentRevision,
}: {
  revisions: any[];
  loading: boolean;
  currentRevision: number;
}) => {
  if (loading) {
    return (
      <div className="flex items-center justify-center py-8">
        <Loader2 className="h-6 w-6 animate-spin text-primary" />
      </div>
    );
  }

  if (!revisions || revisions.length === 0) {
    return (
      <div className="flex items-center justify-center py-8 text-sm text-muted-foreground">
        Nenhuma revisão encontrada
      </div>
    );
  }

  return (
    <div className="space-y-2">
      {revisions.map((revision) => (
        <Card
          key={revision.revision}
          className={cn(
            'p-3',
            revision.revision === currentRevision && 'border-primary bg-primary/5'
          )}
        >
          <div className="flex items-start justify-between gap-2">
            <div className="flex-1">
              <div className="flex items-center gap-2 mb-1">
                <span className="text-sm font-semibold">Revisão {revision.revision}</span>
                {revision.revision === currentRevision && (
                  <Badge variant="outline" className="text-xs">Atual</Badge>
                )}
                <Badge className={cn('text-xs', getStatusColor(revision.status))}>
                  {revision.status}
                </Badge>
              </div>
              <p className="text-xs text-muted-foreground mb-2">
                {revision.description || 'Sem descrição'}
              </p>
              <p className="text-xs text-muted-foreground">
                {formatDate(revision.updatedAt)}
              </p>
            </div>
            {revision.revision !== currentRevision && (
              <Button size="sm" variant="outline" className="gap-1">
                <RotateCcw className="h-3 w-3" />
                Rollback
              </Button>
            )}
          </div>
        </Card>
      ))}
    </div>
  );
};

// Manifest Tab Component
const ManifestTab = ({
  manifest,
  notes,
  releaseName,
  namespace,
  cluster,
  chart,
  onApplySuccess,
}: {
  manifest: string;
  notes: string;
  releaseName: string;
  namespace: string;
  cluster: string;
  chart: string;
  onApplySuccess: () => void;
}) => {
  const [showNotes, setShowNotes] = useState(true);
  const [editedManifest, setEditedManifest] = useState(manifest);
  const [originalManifest, setOriginalManifest] = useState(manifest);
  const [manifestFullScreen, setManifestFullScreen] = useState(false);
  const [validationError, setValidationError] = useState<string | null>(null);
  const [isValidating, setIsValidating] = useState(false);
  const [validationSuccess, setValidationSuccess] = useState(false);
  const [showApplyModal, setShowApplyModal] = useState(false);
  const [viewMode, setViewMode] = useState<'editor' | 'diff'>('editor');

  // History management
  const [history, setHistory] = useState<string[]>([manifest]);
  const [historyIndex, setHistoryIndex] = useState(0);

  const canUndo = historyIndex > 0;
  const canRedo = historyIndex < history.length - 1;
  const hasChanges = editedManifest !== originalManifest;

  // Sync with props when changed
  useEffect(() => {
    setEditedManifest(manifest);
    setOriginalManifest(manifest);
    setHistory([manifest]);
    setHistoryIndex(0);
    setValidationError(null);
    setValidationSuccess(false);
  }, [manifest]);

  const handleEditorChange = (value: string | undefined) => {
    const newValue = value || '';
    setEditedManifest(newValue);

    // Add to history
    setHistoryIndex((currentIndex) => {
      setHistory((currentHistory) => {
        const newHistory = currentHistory.slice(0, currentIndex + 1);
        if (newHistory[newHistory.length - 1] !== newValue) {
          newHistory.push(newValue);
          if (newHistory.length > 50) {
            newHistory.shift();
            return newHistory;
          }
        }
        return newHistory;
      });
      return Math.min(currentIndex + 1, 49);
    });
  };

  const handleUndo = () => {
    if (historyIndex > 0) {
      const newIndex = historyIndex - 1;
      setHistoryIndex(newIndex);
      setEditedManifest(history[newIndex]);
    }
  };

  const handleRedo = () => {
    if (historyIndex < history.length - 1) {
      const newIndex = historyIndex + 1;
      setHistoryIndex(newIndex);
      setEditedManifest(history[newIndex]);
    }
  };

  const handleToggleView = (mode: 'editor' | 'diff') => {
    if (mode === 'diff' && !hasChanges) return;
    setViewMode(mode);
  };

  const validateYaml = () => {
    setIsValidating(true);
    setValidationError(null);
    setValidationSuccess(false);

    try {
      yaml.load(editedManifest);
      setValidationError(null);
      setValidationSuccess(true);
      setTimeout(() => {
        setValidationSuccess(false);
      }, 2000);
      return true;
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Invalid YAML';
      setValidationError(message);
      return false;
    } finally {
      setIsValidating(false);
    }
  };

  const handleApply = () => {
    if (validateYaml()) {
      setShowApplyModal(true);
    }
  };

  const handleCancel = () => {
    setEditedManifest(originalManifest);
    setHistory([originalManifest]);
    setHistoryIndex(0);
    setValidationError(null);
    setViewMode('editor');
  };

  const manifestContent = (
    <div className="space-y-3">
      {notes && (
        <div>
          <div className="flex items-center justify-between mb-2">
            <h4 className="text-sm font-semibold">Release Notes</h4>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => setShowNotes(!showNotes)}
            >
              {showNotes ? 'Ocultar' : 'Mostrar'}
            </Button>
          </div>
          {showNotes && (
            <Card className="p-4 bg-blue-500/5 border-blue-500/20">
              <pre className="text-xs font-mono whitespace-pre-wrap break-words text-blue-200">
                {notes}
              </pre>
            </Card>
          )}
        </div>
      )}

      <div className="space-y-3">
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <p className="text-sm font-medium">Manifest do Release</p>
            <div className="flex items-center gap-2">
              <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                <button
                  type="button"
                  onClick={handleUndo}
                  disabled={!canUndo}
                  className={`px-2 py-1 text-xs font-medium ${
                    canUndo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                  }`}
                  title="Desfazer (Ctrl+Z)"
                >
                  <Undo2 className="w-3.5 h-3.5" />
                </button>
                <button
                  type="button"
                  onClick={handleRedo}
                  disabled={!canRedo}
                  className={`px-2 py-1 text-xs font-medium border-l border-border/50 ${
                    canRedo ? "bg-background text-muted-foreground hover:bg-secondary" : "bg-background text-muted-foreground/30 cursor-not-allowed"
                  }`}
                  title="Refazer (Ctrl+Y)"
                >
                  <Redo2 className="w-3.5 h-3.5" />
                </button>
              </div>
              <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
                <button
                  type="button"
                  onClick={() => handleToggleView('editor')}
                  className={`px-3 py-1 text-xs font-medium ${
                    viewMode === 'editor' ? "bg-primary text-white" : "bg-background text-muted-foreground"
                  }`}
                >
                  Editor
                </button>
                <button
                  type="button"
                  onClick={() => handleToggleView('diff')}
                  className={`px-3 py-1 text-xs font-medium ${
                    viewMode === 'diff' ? "bg-primary text-white" : "bg-background text-muted-foreground"
                  } ${hasChanges ? "" : "opacity-50 cursor-not-allowed"}`}
                  disabled={!hasChanges}
                >
                  Diff
                </button>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setManifestFullScreen(true)}
                title="Abrir editor em tela cheia"
              >
                <Maximize2 className="w-3.5 h-3.5" />
              </Button>
            </div>
          </div>

          {/* Validation Error */}
          {validationError && (
            <div className="flex items-start gap-2 p-3 bg-destructive/10 border border-destructive/20 rounded-lg">
              <AlertTriangle className="h-4 w-4 text-destructive mt-0.5" />
              <div className="flex-1">
                <p className="text-sm font-medium text-destructive">Erro de validação YAML</p>
                <p className="text-xs text-destructive/80 mt-1">{validationError}</p>
              </div>
            </div>
          )}

          {viewMode === 'editor' && (
            <div className="border rounded-lg overflow-hidden" style={{ height: manifestFullScreen ? 'calc(100vh - 200px)' : '600px' }}>
              <MonacoYamlEditor
                value={editedManifest || '# Nenhum manifest disponível'}
                readOnly={false}
                onChange={handleEditorChange}
                height={manifestFullScreen ? window.innerHeight - 200 : 600}
              />
            </div>
          )}
          {viewMode === 'diff' && (
            <div className="border rounded-lg overflow-hidden" style={{ height: manifestFullScreen ? 'calc(100vh - 200px)' : '600px' }}>
              <MonacoYamlEditor
                mode="diff"
                originalValue={originalManifest}
                value={editedManifest}
                height={manifestFullScreen ? window.innerHeight - 200 : 600}
                readOnly
              />
            </div>
          )}
        </div>

        {/* Action Buttons */}
        <div className="flex flex-wrap gap-2">
          <Button
            size="sm"
            variant={validationSuccess ? "default" : (hasChanges ? "default" : "outline")}
            onClick={validateYaml}
            disabled={isValidating || validationSuccess}
            className={`gap-1 transition-all duration-300 ${
              validationSuccess
                ? 'bg-green-600 hover:bg-green-600 text-white'
                : hasChanges
                  ? 'bg-blue-600 hover:bg-blue-700 text-white'
                  : ''
            }`}
          >
            {isValidating ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : validationSuccess ? (
              <CheckCircle2 className="h-3 w-3" />
            ) : (
              <CheckCircle2 className="h-3 w-3" />
            )}
            {validationSuccess ? 'Válido ✓' : `Validar${hasChanges ? ' ●' : ''}`}
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={handleCancel}
            disabled={!hasChanges}
            className="gap-1"
          >
            <X className="h-3 w-3" />
            Cancelar
          </Button>
          <Button
            size="sm"
            onClick={handleApply}
            disabled={!hasChanges}
            className="gap-1"
          >
            <Upload className="h-3 w-3" />
            Aplicar
          </Button>
        </div>

        {/* Apply Confirmation Modal */}
        <ApplyValuesModal
          open={showApplyModal}
          onOpenChange={setShowApplyModal}
          releaseName={releaseName}
          namespace={namespace}
          cluster={cluster}
          chart={chart}
          originalValues={originalManifest}
          newValues={editedManifest}
          onSuccess={() => {
            onApplySuccess();
            setShowApplyModal(false);
          }}
        />
      </div>
    </div>
  );

  // Fullscreen Dialog
  if (manifestFullScreen) {
    return (
      <Dialog open={manifestFullScreen} onOpenChange={setManifestFullScreen}>
        <DialogContent className="w-screen h-screen max-w-none max-h-none sm:max-w-none sm:max-h-none rounded-none">
          <DialogHeader className="border-b border-border pb-4">
            <DialogTitle className="text-xl font-semibold text-primary">
              Editor de Manifest - {releaseName}
            </DialogTitle>
            <DialogDescription className="text-sm text-muted-foreground">
              {namespace} • Modo tela cheia
            </DialogDescription>
          </DialogHeader>
          <div className="flex-1 overflow-auto">
            {manifestContent}
          </div>
        </DialogContent>
      </Dialog>
    );
  }

  return manifestContent;
};

// Helper functions
const getStatusColor = (status: string) => {
  const statusLower = status.toLowerCase();
  if (statusLower === 'deployed') return 'bg-green-500/10 text-green-500 border-green-500/20';
  if (statusLower === 'failed') return 'bg-red-500/10 text-red-500 border-red-500/20';
  if (statusLower.includes('pending')) return 'bg-yellow-500/10 text-yellow-500 border-yellow-500/20';
  return 'bg-blue-500/10 text-blue-500 border-blue-500/20';
};

const formatDate = (dateStr: string) => {
  if (!dateStr) return 'N/A';
  try {
    const date = new Date(dateStr);
    return date.toLocaleString('pt-BR', {
      day: '2-digit',
      month: '2-digit',
      year: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  } catch {
    return dateStr;
  }
};
