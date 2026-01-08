import { useState } from 'react';
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
import type { HelmActionRequest } from '../types/helm';

interface HelmInstallModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  namespace?: string;
  onSuccess?: () => void;
}

const defaultValues = `# Add your custom values here
# Example:
# replicaCount: 2
# image:
#   repository: nginx
#   tag: "1.21"
`;

export const HelmInstallModal = ({
  open,
  onOpenChange,
  cluster,
  namespace = '',
  onSuccess,
}: HelmInstallModalProps) => {
  const [releaseName, setReleaseName] = useState('');
  const [releaseNamespace, setReleaseNamespace] = useState(namespace);
  const [chartRef, setChartRef] = useState('');
  const [version, setVersion] = useState('');
  const [valuesYaml, setValuesYaml] = useState(defaultValues);
  const [force, setForce] = useState(false);
  const [dryRun, setDryRun] = useState(false);
  const [isExecuting, setIsExecuting] = useState(false);
  const [operationLogs, setOperationLogs] = useState<string[]>([]);
  const [operationStatus, setOperationStatus] = useState<'idle' | 'running' | 'success' | 'error'>('idle');

  const { executeOperation, streamOperation } = useHelmOperation();

  const handleInstall = async () => {
    if (!releaseName || !chartRef) {
      toast.error('Release name and chart reference are required');
      return;
    }

    setIsExecuting(true);
    setOperationStatus('running');
    setOperationLogs([]);

    const request: HelmActionRequest = {
      namespace: releaseNamespace || 'default',
      releaseName,
      action: 'install',
      chartRef,
      version: version || undefined,
      valuesYaml: valuesYaml !== defaultValues ? valuesYaml : undefined,
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
          toast.success(dryRun ? 'Dry run completed successfully' : 'Install completed successfully');
          setTimeout(() => {
            onSuccess?.();
            onOpenChange(false);
            resetForm();
          }, 2000);
        } else if (event.phase === 'failed') {
          setOperationStatus('error');
          toast.error('Install failed');
        }
      });

      return () => cleanup();
    } catch (error) {
      setOperationStatus('error');
      const message = error instanceof Error ? error.message : 'Unknown error';
      setOperationLogs((prev) => [...prev, `Error: ${message}`]);
      toast.error(`Failed to install release: ${message}`);
    } finally {
      setIsExecuting(false);
    }
  };

  const resetForm = () => {
    setReleaseName('');
    setReleaseNamespace(namespace);
    setChartRef('');
    setVersion('');
    setValuesYaml(defaultValues);
    setForce(false);
    setDryRun(false);
    setOperationStatus('idle');
    setOperationLogs([]);
  };

  const handleClose = () => {
    if (!isExecuting) {
      onOpenChange(false);
      setTimeout(resetForm, 300);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle>Install Helm Chart</DialogTitle>
          <DialogDescription>
            Install a new Helm release in the cluster
          </DialogDescription>
        </DialogHeader>

        <Tabs defaultValue="settings" className="flex-1 flex flex-col min-h-0">
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="settings">Settings</TabsTrigger>
            <TabsTrigger value="values">Values</TabsTrigger>
          </TabsList>

          <TabsContent value="settings" className="space-y-4 mt-4 overflow-auto">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="releaseName">
                  Release Name <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="releaseName"
                  value={releaseName}
                  onChange={(e) => setReleaseName(e.target.value)}
                  placeholder="my-release"
                  disabled={isExecuting}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="namespace">Namespace</Label>
                <Input
                  id="namespace"
                  value={releaseNamespace}
                  onChange={(e) => setReleaseNamespace(e.target.value)}
                  placeholder="default"
                  disabled={isExecuting}
                />
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="chart">
                Chart Reference <span className="text-destructive">*</span>
              </Label>
              <Input
                id="chart"
                value={chartRef}
                onChange={(e) => setChartRef(e.target.value)}
                placeholder="e.g., stable/nginx-ingress or ./my-chart"
                disabled={isExecuting}
              />
              <p className="text-xs text-muted-foreground">
                Can be a chart URL, repo/chartname, or local path
              </p>
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
                  Force install
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
                  Dry run (simulate install without applying)
                </Label>
              </div>
            </div>
          </TabsContent>

          <TabsContent value="values" className="flex-1 overflow-hidden mt-4">
            <div className="space-y-2 h-full flex flex-col">
              <Label>YAML Values (optional)</Label>
              <div className="flex-1 border rounded-md overflow-hidden">
                <MonacoYamlEditor
                  value={valuesYaml}
                  onChange={setValuesYaml}
                  height="100%"
                  readOnly={isExecuting}
                />
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
          <Button onClick={handleInstall} disabled={isExecuting || !releaseName || !chartRef}>
            {isExecuting ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                {dryRun ? 'Running dry run...' : 'Installing...'}
              </>
            ) : (
              dryRun ? 'Dry Run' : 'Install'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
