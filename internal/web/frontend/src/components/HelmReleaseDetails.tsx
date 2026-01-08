import { useState } from 'react';
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
} from 'lucide-react';
import { cn } from '../lib/utils';
import { MonacoYamlEditor } from './MonacoYamlEditor';
import { HelmUpgradeModal } from './HelmUpgradeModal.tsx';
import { HelmRollbackModal, HelmUninstallModal } from './HelmActionModals.tsx';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from './ui/dropdown-menu';

interface HelmReleaseDetailsProps {
  cluster: string;
  release: string | null;
  onInstallClick?: () => void;
}

export const HelmReleaseDetails = ({ cluster, release, onInstallClick }: HelmReleaseDetailsProps) => {
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
          // Trigger refresh
          window.location.reload();
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
          // Trigger refresh
          window.location.reload();
        }}
      />

      <HelmUninstallModal
        open={showUninstallModal}
        onOpenChange={setShowUninstallModal}
        releaseName={releaseDetail.name}
        namespace={releaseDetail.namespace}
        cluster={cluster}
        onSuccess={() => {
          // Trigger refresh and deselect
          window.location.reload();
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
          <ManifestTab manifest={releaseDetail.manifest} notes={releaseDetail.notes} />
        </TabsContent>
      </Tabs>
    </div>
  );
};

// Values Tab Component
const ValuesTab = ({ valuesRaw, valuesRendered }: { valuesRaw: string; valuesRendered: string }) => {
  const [showRendered, setShowRendered] = useState(false);
  const [editedValue, setEditedValue] = useState(valuesRaw);

  // Sync with props when changed
  useState(() => {
    setEditedValue(valuesRaw);
  });

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-semibold">Valores do Release</h4>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant={!showRendered ? 'default' : 'outline'}
            onClick={() => setShowRendered(false)}
          >
            Raw
          </Button>
          <Button
            size="sm"
            variant={showRendered ? 'default' : 'outline'}
            onClick={() => setShowRendered(true)}
          >
            Renderizado
          </Button>
        </div>
      </div>

      <div className="border rounded-lg overflow-hidden" style={{ height: 'calc(100vh - 400px)' }}>
        <MonacoYamlEditor
          value={showRendered ? valuesRendered || '# Nenhum valor renderizado' : editedValue || '# Nenhum valor customizado'}
          readOnly={showRendered}
          onChange={(value) => {
            if (!showRendered) {
              setEditedValue(value || '');
            }
          }}
        />
      </div>
    </div>
  );
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
const ManifestTab = ({ manifest, notes }: { manifest: string; notes: string }) => {
  const [showNotes, setShowNotes] = useState(true);
  const [editedManifest, setEditedManifest] = useState(manifest);

  // Sync with props when changed
  useState(() => {
    setEditedManifest(manifest);
  });

  return (
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

      <div>
        <h4 className="text-sm font-semibold mb-2">Kubernetes Manifest</h4>
        <div className="border rounded-lg overflow-hidden" style={{ height: 'calc(100vh - 480px)' }}>
          <MonacoYamlEditor
            value={editedManifest || '# Nenhum manifest disponível'}
            readOnly={false}
            onChange={(value) => setEditedManifest(value || '')}
          />
        </div>
      </div>
    </div>
  );
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
