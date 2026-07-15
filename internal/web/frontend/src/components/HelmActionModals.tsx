import { useEffect, useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';
import { Button } from './ui/button';
import { Label } from './ui/label';
import { RadioGroup, RadioGroupItem } from './ui/radio-group';
import { Checkbox } from './ui/checkbox';
import { Loader2, AlertCircle, CheckCircle, RotateCcw } from 'lucide-react';
import { useHelmOperation } from '../hooks/useHelm';
import { toast } from 'sonner';
import type { RevisionEntry, HelmActionRequest } from '../types/helm';

interface HelmRollbackModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  releaseName: string;
  namespace: string;
  cluster: string;
  revisions: RevisionEntry[];
  currentRevision: number;
  initialRevision?: number;
  onSuccess?: () => void;
}

export const HelmRollbackModal = ({
  open,
  onOpenChange,
  releaseName,
  namespace,
  cluster,
  revisions,
  currentRevision,
  initialRevision,
  onSuccess,
}: HelmRollbackModalProps) => {
  const [targetRevision, setTargetRevision] = useState<number>(0);
  const [force, setForce] = useState(false);
  const [isExecuting, setIsExecuting] = useState(false);
  const [operationLogs, setOperationLogs] = useState<string[]>([]);
  const [operationStatus, setOperationStatus] = useState<'idle' | 'running' | 'success' | 'error'>('idle');

  const { executeOperation, streamOperation } = useHelmOperation();

  useEffect(() => {
    if (open) setTargetRevision(initialRevision ?? 0);
  }, [open, initialRevision]);

  const handleRollback = async () => {
    if (!targetRevision || targetRevision === currentRevision) {
      toast.error('Please select a different revision to rollback to');
      return;
    }

    setIsExecuting(true);
    setOperationStatus('running');
    setOperationLogs([]);

    const request: HelmActionRequest = {
      namespace,
      releaseName,
      action: 'rollback',
      targetRevision,
      force,
    };

    try {
      const operationId = await executeOperation(cluster, request);

      // Stream operation logs
      const cleanup = streamOperation(operationId, (event) => {
        setOperationLogs((prev) => [...prev, event.message]);

        if (event.phase === 'succeeded') {
          setOperationStatus('success');
          toast.success(`Rolled back to revision ${targetRevision} successfully`);
          setTimeout(() => {
            onSuccess?.();
            onOpenChange(false);
          }, 2000);
        } else if (event.phase === 'failed') {
          setOperationStatus('error');
          toast.error('Rollback failed');
        }
      });

      return () => cleanup();
    } catch (error) {
      setOperationStatus('error');
      const message = error instanceof Error ? error.message : 'Unknown error';
      setOperationLogs((prev) => [...prev, `Error: ${message}`]);
      toast.error(`Failed to rollback release: ${message}`);
    } finally {
      setIsExecuting(false);
    }
  };

  const handleClose = () => {
    if (!isExecuting) {
      onOpenChange(false);
      setTimeout(() => {
        setTargetRevision(0);
        setForce(false);
        setOperationStatus('idle');
        setOperationLogs([]);
      }, 300);
    }
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

  // Filter out current revision and sort by revision number descending
  const availableRevisions = revisions
    .filter((r) => r.revision !== currentRevision)
    .sort((a, b) => b.revision - a.revision);

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-2xl max-h-[90vh] overflow-hidden flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <RotateCcw className="h-5 w-5" />
            Rollback Release: {releaseName}
          </DialogTitle>
          <DialogDescription>
            Select a previous revision to rollback to. Current revision: {currentRevision}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-auto space-y-4">
          {availableRevisions.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              No previous revisions available for rollback
            </div>
          ) : (
            <>
              <RadioGroup
                value={targetRevision.toString()}
                onValueChange={(value) => setTargetRevision(parseInt(value))}
                disabled={isExecuting}
                className="space-y-2"
              >
                {availableRevisions.map((revision) => (
                  <div
                    key={revision.revision}
                    className="flex items-start space-x-3 p-3 border rounded-lg hover:bg-accent transition-colors"
                  >
                    <RadioGroupItem
                      value={revision.revision.toString()}
                      id={`revision-${revision.revision}`}
                      className="mt-1"
                    />
                    <Label
                      htmlFor={`revision-${revision.revision}`}
                      className="flex-1 cursor-pointer"
                    >
                      <div className="flex items-center justify-between mb-1">
                        <span className="font-semibold">Revision {revision.revision}</span>
                        <span className="text-xs text-muted-foreground">
                          {formatDate(revision.updatedAt)}
                        </span>
                      </div>
                      <div className="text-sm text-muted-foreground">
                        {revision.description || 'No description'}
                      </div>
                      <div className="text-xs text-muted-foreground mt-1">
                        Status: {revision.status}
                      </div>
                    </Label>
                  </div>
                ))}
              </RadioGroup>

              <div className="flex items-center space-x-2 pt-2">
                <Checkbox
                  id="force"
                  checked={force}
                  onCheckedChange={(checked) => setForce(checked as boolean)}
                  disabled={isExecuting}
                />
                <Label htmlFor="force" className="cursor-pointer text-sm">
                  Force rollback (recreate resources if needed)
                </Label>
              </div>
            </>
          )}
        </div>

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
          <Button
            onClick={handleRollback}
            disabled={isExecuting || !targetRevision || availableRevisions.length === 0}
          >
            {isExecuting ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Rolling back...
              </>
            ) : (
              <>
                <RotateCcw className="mr-2 h-4 w-4" />
                Rollback
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

// Uninstall Modal
interface HelmUninstallModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  releaseName: string;
  namespace: string;
  cluster: string;
  onSuccess?: () => void;
}

export const HelmUninstallModal = ({
  open,
  onOpenChange,
  releaseName,
  namespace,
  cluster,
  onSuccess,
}: HelmUninstallModalProps) => {
  const [isExecuting, setIsExecuting] = useState(false);
  const [operationLogs, setOperationLogs] = useState<string[]>([]);
  const [operationStatus, setOperationStatus] = useState<'idle' | 'running' | 'success' | 'error'>('idle');

  const { executeOperation, streamOperation } = useHelmOperation();

  const handleUninstall = async () => {
    setIsExecuting(true);
    setOperationStatus('running');
    setOperationLogs([]);

    const request: HelmActionRequest = {
      namespace,
      releaseName,
      action: 'uninstall',
    };

    try {
      const operationId = await executeOperation(cluster, request);

      // Stream operation logs
      const cleanup = streamOperation(operationId, (event) => {
        setOperationLogs((prev) => [...prev, event.message]);

        if (event.phase === 'succeeded') {
          setOperationStatus('success');
          toast.success('Release uninstalled successfully');
          setTimeout(() => {
            onSuccess?.();
            onOpenChange(false);
          }, 2000);
        } else if (event.phase === 'failed') {
          setOperationStatus('error');
          toast.error('Uninstall failed');
        }
      });

      return () => cleanup();
    } catch (error) {
      setOperationStatus('error');
      const message = error instanceof Error ? error.message : 'Unknown error';
      setOperationLogs((prev) => [...prev, `Error: ${message}`]);
      toast.error(`Failed to uninstall release: ${message}`);
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
      }, 300);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Uninstall Release</DialogTitle>
          <DialogDescription>
            This action will permanently remove the release and all its resources.
          </DialogDescription>
        </DialogHeader>

        <div className="py-4">
          <div className="space-y-2 text-sm">
            <div>
              <span className="font-medium">Release:</span> {releaseName}
            </div>
            <div>
              <span className="font-medium">Namespace:</span> {namespace}
            </div>
          </div>

          <div className="mt-4 p-3 bg-destructive/10 border border-destructive/20 rounded-md">
            <p className="text-sm text-destructive font-medium">
              ⚠️ Warning: This action cannot be undone!
            </p>
          </div>
        </div>

        {/* Operation Logs */}
        {operationLogs.length > 0 && (
          <div className="border rounded-md p-3 bg-muted/30 max-h-32 overflow-auto">
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
          <Button variant="destructive" onClick={handleUninstall} disabled={isExecuting}>
            {isExecuting ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Uninstalling...
              </>
            ) : (
              'Uninstall'
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
