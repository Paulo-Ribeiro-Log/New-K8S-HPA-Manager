import { useState, useEffect } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Separator } from "@/components/ui/separator";
import { AlertCircle, Shield, Trash2, CheckCircle2, XCircle } from "lucide-react";
import { Alert, AlertDescription } from "@/components/ui/alert";

export interface CordonDrainConfig {
  cordonEnabled: boolean;
  drainEnabled: boolean;
  gracePeriod: number;
  timeout: number;
  forceDelete: boolean;
  ignoreDaemonSets: boolean;
  deleteEmptyDir: boolean;
  chunkSize: number;
}

interface CordonDrainConfigModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (config: CordonDrainConfig) => void;
  nodePoolName?: string;
}

export default function CordonDrainConfigModal({
  open,
  onOpenChange,
  onConfirm,
  nodePoolName = "",
}: CordonDrainConfigModalProps) {
  // Estados para configuração
  const [cordonEnabled, setCordonEnabled] = useState(false);
  const [drainEnabled, setDrainEnabled] = useState(false);
  const [gracePeriod, setGracePeriod] = useState("300");
  const [timeout, setTimeout] = useState("600");
  const [forceDelete, setForceDelete] = useState(false);
  const [ignoreDaemonSets, setIgnoreDaemonSets] = useState(true);
  const [deleteEmptyDir, setDeleteEmptyDir] = useState(false);
  const [chunkSize, setChunkSize] = useState("5");

  // Reset ao abrir modal
  useEffect(() => {
    if (open) {
      setCordonEnabled(false);
      setDrainEnabled(false);
      setGracePeriod("300");
      setTimeout("600");
      setForceDelete(false);
      setIgnoreDaemonSets(true);
      setDeleteEmptyDir(false);
      setChunkSize("5");
    }
  }, [open]);

  const handleConfirm = () => {
    const config: CordonDrainConfig = {
      cordonEnabled,
      drainEnabled,
      gracePeriod: parseInt(gracePeriod) || 300,
      timeout: parseInt(timeout) || 600,
      forceDelete,
      ignoreDaemonSets,
      deleteEmptyDir,
      chunkSize: parseInt(chunkSize) || 5,
    };

    onConfirm(config);
    onOpenChange(false);
  };

  const handleCancel = () => {
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Shield className="w-5 h-5 text-primary" />
            Configuração de Cordon/Drain
          </DialogTitle>
          <DialogDescription>
            {nodePoolName ? (
              <>Configure operações de segurança para o Node Pool <strong>{nodePoolName}</strong></>
            ) : (
              <>Configure operações de segurança antes de aplicar alterações</>
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6 py-4">
          {/* Alerta informativo */}
          <Alert>
            <AlertCircle className="h-4 w-4" />
            <AlertDescription>
              <strong>Cordon</strong> marca nodes como não agendáveis (unschedulable).
              <br />
              <strong>Drain</strong> evacua pods dos nodes antes de operações de manutenção.
            </AlertDescription>
          </Alert>

          {/* CORDON Configuration */}
          <div className="space-y-4">
            <div className="flex items-center gap-3">
              <Checkbox
                id="cordon-enabled"
                checked={cordonEnabled}
                onCheckedChange={(checked) => setCordonEnabled(checked as boolean)}
              />
              <div className="flex-1">
                <Label htmlFor="cordon-enabled" className="text-base font-semibold cursor-pointer flex items-center gap-2">
                  <XCircle className="w-4 h-4 text-orange-500" />
                  Habilitar CORDON
                </Label>
                <p className="text-sm text-muted-foreground mt-1">
                  Marca todos os nodes do pool como unschedulable antes de aplicar mudanças
                </p>
              </div>
            </div>
          </div>

          <Separator />

          {/* DRAIN Configuration */}
          <div className="space-y-4">
            <div className="flex items-center gap-3">
              <Checkbox
                id="drain-enabled"
                checked={drainEnabled}
                onCheckedChange={(checked) => setDrainEnabled(checked as boolean)}
              />
              <div className="flex-1">
                <Label htmlFor="drain-enabled" className="text-base font-semibold cursor-pointer flex items-center gap-2">
                  <Trash2 className="w-4 h-4 text-red-500" />
                  Habilitar DRAIN
                </Label>
                <p className="text-sm text-muted-foreground mt-1">
                  Evacua pods dos nodes antes de aplicar mudanças (requer CORDON)
                </p>
              </div>
            </div>

            {/* Opções de DRAIN (aparecem apenas se DRAIN habilitado) */}
            {drainEnabled && (
              <div className="ml-8 space-y-4 p-4 border rounded-lg bg-muted/30">
                <div className="grid grid-cols-2 gap-4">
                  {/* Grace Period */}
                  <div className="space-y-2">
                    <Label htmlFor="grace-period">Grace Period (segundos)</Label>
                    <Input
                      id="grace-period"
                      type="text"
                      value={gracePeriod}
                      onChange={(e) => {
                        const val = e.target.value;
                        if (val === "" || /^\d+$/.test(val)) {
                          setGracePeriod(val);
                        }
                      }}
                      placeholder="300"
                    />
                    <p className="text-xs text-muted-foreground">
                      Tempo de espera antes de forçar término do pod (padrão: 300s)
                    </p>
                  </div>

                  {/* Timeout */}
                  <div className="space-y-2">
                    <Label htmlFor="timeout">Timeout (segundos)</Label>
                    <Input
                      id="timeout"
                      type="text"
                      value={timeout}
                      onChange={(e) => {
                        const val = e.target.value;
                        if (val === "" || /^\d+$/.test(val)) {
                          setTimeout(val);
                        }
                      }}
                      placeholder="600"
                    />
                    <p className="text-xs text-muted-foreground">
                      Timeout máximo para operação de drain (padrão: 600s)
                    </p>
                  </div>

                  {/* Chunk Size */}
                  <div className="space-y-2">
                    <Label htmlFor="chunk-size">Chunk Size (pods simultâneos)</Label>
                    <Input
                      id="chunk-size"
                      type="text"
                      value={chunkSize}
                      onChange={(e) => {
                        const val = e.target.value;
                        if (val === "" || /^\d+$/.test(val)) {
                          setChunkSize(val);
                        }
                      }}
                      placeholder="5"
                    />
                    <p className="text-xs text-muted-foreground">
                      Número de pods evacuados simultaneamente (padrão: 5)
                    </p>
                  </div>
                </div>

                <Separator />

                {/* Checkboxes de opções avançadas */}
                <div className="space-y-3">
                  <div className="flex items-center gap-3">
                    <Checkbox
                      id="ignore-daemonsets"
                      checked={ignoreDaemonSets}
                      onCheckedChange={(checked) => setIgnoreDaemonSets(checked as boolean)}
                    />
                    <Label htmlFor="ignore-daemonsets" className="cursor-pointer text-sm">
                      Ignorar DaemonSets
                    </Label>
                  </div>

                  <div className="flex items-center gap-3">
                    <Checkbox
                      id="delete-emptydir"
                      checked={deleteEmptyDir}
                      onCheckedChange={(checked) => setDeleteEmptyDir(checked as boolean)}
                    />
                    <Label htmlFor="delete-emptydir" className="cursor-pointer text-sm">
                      Deletar volumes EmptyDir
                    </Label>
                  </div>

                  <div className="flex items-center gap-3">
                    <Checkbox
                      id="force-delete"
                      checked={forceDelete}
                      onCheckedChange={(checked) => setForceDelete(checked as boolean)}
                    />
                    <Label htmlFor="force-delete" className="cursor-pointer text-sm text-destructive">
                      Forçar deleção (⚠️ Ignore PodDisruptionBudget)
                    </Label>
                  </div>
                </div>
              </div>
            )}
          </div>

          {/* Resumo da configuração */}
          {(cordonEnabled || drainEnabled) && (
            <Alert className="bg-primary/5 border-primary/20">
              <CheckCircle2 className="h-4 w-4 text-primary" />
              <AlertDescription>
                <strong>Configuração ativa:</strong>
                <ul className="list-disc list-inside mt-2 space-y-1 text-sm">
                  {cordonEnabled && <li>CORDON será executado antes de aplicar mudanças</li>}
                  {drainEnabled && (
                    <li>
                      DRAIN será executado com grace period de {gracePeriod}s e timeout de {timeout}s
                    </li>
                  )}
                </ul>
              </AlertDescription>
            </Alert>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={handleCancel}>
            Cancelar
          </Button>
          <Button onClick={handleConfirm} className="bg-gradient-primary">
            <CheckCircle2 className="w-4 h-4 mr-2" />
            Confirmar Configuração
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
