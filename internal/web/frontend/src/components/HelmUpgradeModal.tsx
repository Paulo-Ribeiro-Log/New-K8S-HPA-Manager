import { useState, useEffect } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';
import { Checkbox } from './ui/checkbox';
import { MonacoYamlEditor } from './MonacoYamlEditor';
import { Tabs, TabsContent, TabsList, TabsTrigger } from './ui/tabs';
import { Loader2, AlertCircle, CheckCircle } from 'lucide-react';
import { useHelmOperation } from '../hooks/useHelm';
import { toast } from 'sonner';
import type { ReleaseDetail, HelmActionRequest } from '../types/helm';

interface HelmUpgradeModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  release: ReleaseDetail | null;
  cluster: string;
  onSuccess?: () => void;
}

export const HelmUpgradeModal = ({
  open,
  onOpenChange,
  release,
  cluster,
  onSuccess,
}: HelmUpgradeModalProps) => {
  const [chartRef, setChartRef] = useState('');
  const [version, setVersion] = useState('');
  const [valuesYaml, setValuesYaml] = useState('');
  const [force, setForce] = useState(false);
  const [dryRun, setDryRun] = useState(false);
  const [isExecuting, setIsExecuting] = useState(false);
  const [operationLogs, setOperationLogs] = useState<string[]>([]);
  const [operationStatus, setOperationStatus] = useState<'idle' | 'running' | 'success' | 'error'>('idle');

  const { executeOperation, streamOperation } = useHelmOperation();

  // Initialize values when release changes
  useEffect(() => {
    if (release && open) {
      // Leave chartRef empty - user must provide full repo/chart format
      setChartRef('');
      setVersion('');
      setValuesYaml(release.valuesRaw || '');
      setOperationStatus('idle');
      setOperationLogs([]);
    }
  }, [release, open]);

  const handleUpgrade = async () => {
    if (!release) return;

    setIsExecuting(true);
    setOperationStatus('running');
    setOperationLogs([]);

    const request: HelmActionRequest = {
      namespace: release.namespace,
      releaseName: release.name,
      action: 'upgrade',
      chartRef,
      version: version || undefined,
      valuesYaml: valuesYaml || undefined,
      force,
      dryRun,
    };

    try {
      const operationId = await executeOperation(cluster, request);

      // Stream operation logs
      const cleanup = streamOperation(operationId, (event) => {
        setOperationLogs((prev) => [...prev, event.message]);

        if (event.phase === 'succeeded') {
          setOperationStatus('success');
          toast.success(dryRun ? 'Dry run completed successfully' : 'Upgrade completed successfully');
          setTimeout(() => {
            onSuccess?.();
            onOpenChange(false);
          }, 2000);
        } else if (event.phase === 'failed') {
          setOperationStatus('error');
          toast.error('Upgrade failed');
        }
      });

      return () => cleanup();
    } catch (error) {
      setOperationStatus('error');
      const message = error instanceof Error ? error.message : 'Unknown error';
      setOperationLogs((prev) => [...prev, `Error: ${message}`]);
      toast.error(`Failed to upgrade release: ${message}`);
    } finally {
      setIsExecuting(false);
    }
  };

  const handleClose = () => {
    if (!isExecuting) {
      onOpenChange(false);
      setTimeout(() => {
        setOperationStatus('idle');
        setOperationLogs([]);
        setForce(false);
        setDryRun(false);
      }, 300);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle>Upgrade Release: {release?.name}</DialogTitle>
          <DialogDescription>
            Update the release with new chart version or values
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="values" className="flex-1 flex flex-col min-h-0">
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="values">Values</TabsTrigger>
            <TabsTrigger value="settings">Settings</TabsTrigger>
          </TabsList>

          <TabsContent value="values" className="flex-1 overflow-hidden mt-4">
            <div className="space-y-2 h-full flex flex-col min-h-[500px]">
              <Label>YAML Values</Label>
              <div className="flex-1 border rounded-md overflow-hidden min-h-[450px]">
                <MonacoYamlEditor
                  value={valuesYaml}
                  onChange={setValuesYaml}
                  height={450}
                  readOnly={isExecuting}
                />
              </div>
            </div>
          </TabsContent>

          <TabsContent value="settings" className="space-y-4 mt-4 overflow-auto">
            <div className="space-y-2">
              <Label htmlFor="chart">
                Chart Reference <span className="text-muted-foreground text-xs">(opcional)</span>
              </Label>
              <Input
                id="chart"
                value={chartRef}
                onChange={(e) => setChartRef(e.target.value)}
                placeholder="Deixe vazio para manter o chart atual"
                disabled={isExecuting}
              />
              <div className="text-xs text-muted-foreground space-y-1">
                <p>💡 <strong>Vazio:</strong> mantém o chart <code className="bg-muted px-1 rounded">{release?.chart}</code> e atualiza apenas os values</p>
                <p>💡 <strong>Preenchido:</strong> muda para outro chart (formato: <code className="bg-muted px-1 rounded">repositorio/chart</code>)</p>
                <p>Exemplos: <code className="bg-muted px-1 rounded">bitnami/nginx</code>, <code className="bg-muted px-1 rounded">stable/mysql</code></p>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="version">Chart Version (optional)</Label>
              <Input
                id="version"
                value={version}
                onChange={(e) => setVersion(e.target.value)}
                placeholder="e.g., 1.2.3"
                disabled={isExecuting}
              />
            </div>

            <div className="space-y-3 pt-2">
              <div className="flex items-center space-x-2">
                <Checkbox
                  id="force"
                  checked={force}
                  onCheckedChange={(checked) => setForce(checked as boolean)}
                  disabled={isExecuting}
                />
                <Label htmlFor="force" className="cursor-pointer">
                  Force upgrade (recreate resources if needed)
                </Label>
              </div>

              <div className="flex items-center space-x-2">
                <Checkbox
                  id="dryRun"
                  checked={dryRun}
                  onCheckedChange={(checked) => setDryRun(checked as boolean)}
                  disabled={isExecuting}
                />
                <Label htmlFor="dryRun" className="cursor-pointer">
                  Dry run (simulate upgrade without applying)
                </Label>
              </div>
            </div>
          </TabsContent>
        </Tabs>

        {/* Operation Logs */}
        {operationLogs.length > 0 && (
          <div className="mt-4 border rounded-md p-3 bg-muted/30 max-h-32 overflow-auto">
            <div className="flex items-center gap-2 mb-2">
              {operationStatus === 'running' && <Loader2 className="h-4 w-4 animate-spin" />}
              {operationStatus === 'success' && <CheckCircle className="h-4 w-4 text-green-500" />}
              {operationStatus === 'error' && <AlertCircle className="h-4 w-4 text-destructive" />}
              <span className="text-sm font-medium">Operation Logs</span>
            </div>
            <pre className="text-xs font-mono whitespace-pre-wrap">
              {operationLogs.join('\n')}
            </pre>
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={handleClose} disabled={isExecuting}>
            Cancel
          </Button>
          <Button onClick={handleUpgrade} disabled={isExecuting}>
            {isExecuting ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                {dryRun ? 'Running dry run...' : 'Upgrading...'}
              </>
            ) : (
              dryRun ? 'Dry Run' : 'Upgrade'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
