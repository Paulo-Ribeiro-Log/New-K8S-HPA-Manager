import { useState, useEffect, useMemo } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';
import { Button } from './ui/button';
import { Alert, AlertDescription } from './ui/alert';
import { Badge } from './ui/badge';
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs';
import {
  Loader2,
  AlertTriangle,
  CheckCircle2,
  FileCode,
  GitCompare,
  Upload,
} from 'lucide-react';
import { useHelmOperation } from '../hooks/useHelm.ts';
import { MonacoYamlEditor } from './MonacoYamlEditor';
import * as Diff from 'diff';

interface ApplyValuesModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  releaseName: string;
  namespace: string;
  cluster: string;
  originalValues: string;
  newValues: string;
  onSuccess?: () => void;
}

export const ApplyValuesModal = ({
  open,
  onOpenChange,
  releaseName,
  namespace,
  cluster,
  originalValues,
  newValues,
  onSuccess,
}: ApplyValuesModalProps) => {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [operationId, setOperationId] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [isComplete, setIsComplete] = useState(false);
  const [activeTab, setActiveTab] = useState<'diff' | 'preview'>('diff');

  const { executeOperation, streamOperation } = useHelmOperation();

  // Calculate diff
  const diffLines = useMemo(() => {
    const changes = Diff.diffLines(originalValues, newValues);
    return changes;
  }, [originalValues, newValues]);

  const stats = useMemo(() => {
    let added = 0;
    let removed = 0;
    let unchanged = 0;

    diffLines.forEach((part) => {
      const count = part.count || 0;
      if (part.added) {
        added += count;
      } else if (part.removed) {
        removed += count;
      } else {
        unchanged += count;
      }
    });

    return { added, removed, unchanged, total: added + removed + unchanged };
  }, [diffLines]);

  useEffect(() => {
    if (open) {
      setLoading(false);
      setError(null);
      setOperationId(null);
      setLogs([]);
      setIsComplete(false);
      setActiveTab('diff');
    }
  }, [open]);

  const handleApply = async () => {
    setLoading(true);
    setError(null);
    setLogs([]);

    try {
      const opId = await executeOperation(cluster, {
        action: 'upgrade',
        releaseName,
        namespace,
        valuesYaml: newValues,
        dryRun: false,
        force: false,
      });

      setOperationId(opId);

      const cleanup = streamOperation(opId, (event) => {
        setLogs((prev) => [...prev, `[${event.phase}] ${event.message}`]);

        if (event.phase === 'completed') {
          setIsComplete(true);
          setLoading(false);
          setTimeout(() => {
            onOpenChange(false);
            onSuccess?.();
          }, 2000);
        } else if (event.phase === 'failed') {
          setError(event.message);
          setLoading(false);
        }
      });

      return () => cleanup?.();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to apply values';
      setError(message);
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Upload className="h-5 w-5" />
            Aplicar Alterações
          </DialogTitle>
          <DialogDescription>
            Confirme as alterações antes de aplicar ao release <strong>{releaseName}</strong> no namespace <strong>{namespace}</strong>
          </DialogDescription>
        </DialogHeader>

        {!operationId ? (
          <>
            {/* Stats */}
            <div className="flex items-center gap-3 py-3 border-y">
              <Badge variant="outline" className="gap-1">
                <FileCode className="h-3 w-3" />
                Total: {stats.total} linhas
              </Badge>
              <Badge variant="outline" className="gap-1 text-green-500">
                +{stats.added} adicionadas
              </Badge>
              <Badge variant="outline" className="gap-1 text-red-500">
                -{stats.removed} removidas
              </Badge>
              <Badge variant="outline" className="gap-1">
                {stats.unchanged} inalteradas
              </Badge>
            </div>

            {/* Tabs */}
            <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as any)} className="flex-1 flex flex-col min-h-0">
              <TabsList className="grid w-full grid-cols-2">
                <TabsTrigger value="diff" className="gap-2">
                  <GitCompare className="h-4 w-4" />
                  Diff Visual
                </TabsTrigger>
                <TabsTrigger value="preview" className="gap-2">
                  <FileCode className="h-4 w-4" />
                  Preview Completo
                </TabsTrigger>
              </TabsList>

              <TabsContent value="diff" className="flex-1 overflow-auto mt-4">
                <div className="border rounded-lg p-4 bg-muted/30 font-mono text-xs space-y-0 overflow-auto max-h-[400px]">
                  {diffLines.map((part, index) => {
                    const lines = part.value.split('\n').filter(line => line !== '');
                    return lines.map((line, lineIndex) => (
                      <div
                        key={`${index}-${lineIndex}`}
                        className={
                          part.added
                            ? 'bg-green-500/20 text-green-300 px-2 py-0.5'
                            : part.removed
                            ? 'bg-red-500/20 text-red-300 px-2 py-0.5'
                            : 'px-2 py-0.5 text-muted-foreground'
                        }
                      >
                        <span className="select-none mr-2">
                          {part.added ? '+' : part.removed ? '-' : ' '}
                        </span>
                        {line}
                      </div>
                    ));
                  })}
                </div>
              </TabsContent>

              <TabsContent value="preview" className="flex-1 overflow-auto mt-4">
                <div className="border rounded-lg overflow-hidden" style={{ height: '400px' }}>
                  <MonacoYamlEditor
                    value={newValues}
                    readOnly={true}
                  />
                </div>
              </TabsContent>
            </Tabs>

            {error && (
              <Alert variant="destructive">
                <AlertTriangle className="h-4 w-4" />
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
          </>
        ) : (
          <>
            {/* Operation Progress */}
            <div className="flex-1 overflow-auto space-y-3">
              <div className="flex items-center gap-2 p-3 bg-muted/30 rounded-lg border">
                {loading ? (
                  <Loader2 className="h-4 w-4 animate-spin text-primary" />
                ) : isComplete ? (
                  <CheckCircle2 className="h-4 w-4 text-green-500" />
                ) : error ? (
                  <AlertTriangle className="h-4 w-4 text-destructive" />
                ) : null}
                <span className="text-sm font-medium">
                  {loading ? 'Aplicando alterações...' : isComplete ? 'Alterações aplicadas com sucesso!' : 'Operação concluída'}
                </span>
              </div>

              {/* Logs */}
              <div className="border rounded-lg p-4 bg-muted/30 font-mono text-xs space-y-1 overflow-auto max-h-[350px]">
                {logs.length === 0 ? (
                  <p className="text-muted-foreground">Aguardando logs...</p>
                ) : (
                  logs.map((log, index) => (
                    <div key={index} className="text-muted-foreground">
                      {log}
                    </div>
                  ))
                )}
              </div>

              {error && (
                <Alert variant="destructive">
                  <AlertTriangle className="h-4 w-4" />
                  <AlertDescription>{error}</AlertDescription>
                </Alert>
              )}
            </div>
          </>
        )}

        <DialogFooter>
          {!operationId ? (
            <>
              <Button
                variant="outline"
                onClick={() => onOpenChange(false)}
                disabled={loading}
              >
                Cancelar
              </Button>
              <Button
                onClick={handleApply}
                disabled={loading}
              >
                {loading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                Confirmar e Aplicar
              </Button>
            </>
          ) : (
            <Button
              onClick={() => onOpenChange(false)}
              disabled={loading}
            >
              {isComplete ? 'Fechar' : 'Fechar'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
