import React, { useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from './ui/dialog';
import { Button } from './ui/button';
import { Checkbox } from './ui/checkbox';
import { Input } from './ui/input';
import { Label } from './ui/label';
import { Badge } from './ui/badge';
import { Alert, AlertDescription } from './ui/alert';
import { Info, AlertTriangle } from 'lucide-react';

export interface NodePoolSequence {
  name: string;
  cluster: string;
  resourceGroup: string;
  subscription: string;
  sequenceOrder: number; // 1 ou 2
  preDrainChanges?: {
    autoscaling: boolean;
    nodeCount: number;
    minNodes: number;
    maxNodes: number;
  };
  postDrainChanges?: {
    autoscaling: boolean;
    nodeCount: number;
    minNodes: number;
    maxNodes: number;
  };
}

export interface DrainOptions {
  // Essenciais
  ignoreDaemonsets: boolean;
  deleteEmptyDirData: boolean;
  force: boolean;
  gracePeriod: number; // segundos
  timeout: string; // "5m", "300s", etc.

  // Avançadas
  disableEviction: boolean;
  skipWaitForDeleteTimeout: number; // segundos
  podSelector: string;
  dryRun: boolean;
  chunkSize: number;
}

export interface SequenceConfig {
  cordonEnabled: boolean;
  drainEnabled: boolean;
  drainOptions: DrainOptions;
}

interface Props {
  open: boolean;
  nodePools: NodePoolSequence[];
  onConfirm: (config: SequenceConfig) => void;
  onCancel: () => void;
}

export default function NodePoolSequencingModal({
  open,
  nodePools,
  onConfirm,
  onCancel,
}: Props) {
  // Operações de transição
  const [cordonEnabled, setCordonEnabled] = useState(true);
  const [drainEnabled, setDrainEnabled] = useState(true);

  // Opções essenciais
  const [ignoreDaemonsets, setIgnoreDaemonsets] = useState(true);
  const [deleteEmptyDirData, setDeleteEmptyDirData] = useState(true);
  const [force, setForce] = useState(false);
  const [gracePeriod, setGracePeriod] = useState('30');
  const [timeout, setTimeout] = useState('5m');

  // Opções avançadas
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [disableEviction, setDisableEviction] = useState(false);
  const [skipWaitTimeout, setSkipWaitTimeout] = useState('20');
  const [podSelector, setPodSelector] = useState('');
  const [dryRun, setDryRun] = useState(false);
  const [chunkSize, setChunkSize] = useState('1');

  // Validação
  const [errors, setErrors] = useState<string[]>([]);

  const validate = (): boolean => {
    const newErrors: string[] = [];

    // Drain requer Cordon
    if (drainEnabled && !cordonEnabled) {
      newErrors.push('Drain requer Cordon habilitado');
    }

    // Grace period válido
    const gp = parseInt(gracePeriod);
    if (isNaN(gp) || gp < 0) {
      newErrors.push('Grace period deve ser >= 0');
    }

    // Timeout válido (regex: \d+[smh])
    if (!/^\d+[smh]$/.test(timeout)) {
      newErrors.push("Timeout inválido (use: 5m, 300s, 1h)");
    }

    // Chunk size válido
    const cs = parseInt(chunkSize);
    if (isNaN(cs) || cs < 1) {
      newErrors.push('Chunk size deve ser >= 1');
    }

    // Skip wait timeout válido
    const swt = parseInt(skipWaitTimeout);
    if (isNaN(swt) || swt < 0) {
      newErrors.push('Skip wait timeout deve ser >= 0');
    }

    setErrors(newErrors);
    return newErrors.length === 0;
  };

  const handleConfirm = () => {
    if (!validate()) {
      return;
    }

    const config: SequenceConfig = {
      cordonEnabled,
      drainEnabled,
      drainOptions: {
        ignoreDaemonsets,
        deleteEmptyDirData,
        force,
        gracePeriod: parseInt(gracePeriod),
        timeout,
        disableEviction,
        skipWaitForDeleteTimeout: parseInt(skipWaitTimeout),
        podSelector,
        dryRun,
        chunkSize: parseInt(chunkSize),
      },
    };

    onConfirm(config);
  };

  // Determinar origem e destino
  const origin = nodePools.find((p) => p.sequenceOrder === 1);
  const dest = nodePools.find((p) => p.sequenceOrder === 2);

  return (
    <Dialog open={open} onOpenChange={(open) => !open && onCancel()}>
      <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>⚙️ Node Pool Sequencing - Configuração</DialogTitle>
          <DialogDescription>
            Configure as opções de cordon e drain para transição segura entre
            node pools
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-6">
          {/* Node Pools Selecionados */}
          <section>
            <h3 className="text-sm font-semibold mb-2">
              📋 Node Pools Selecionados
            </h3>
            <div className="space-y-2">
              {nodePools.map((pool) => (
                <div
                  key={pool.name}
                  className="flex items-start gap-2 p-2 bg-muted rounded-md"
                >
                  <Badge variant="outline" className="mt-0.5">
                    *{pool.sequenceOrder}
                  </Badge>
                  <div className="flex-1">
                    <div className="font-medium">{pool.name}</div>
                    {pool.preDrainChanges && (
                      <div className="text-xs text-muted-foreground">
                        PRE-DRAIN: autoscaling=
                        {pool.preDrainChanges.autoscaling ? 'ON' : 'OFF'},
                        count={pool.preDrainChanges.nodeCount}, min=
                        {pool.preDrainChanges.minNodes}, max=
                        {pool.preDrainChanges.maxNodes}
                      </div>
                    )}
                    {pool.postDrainChanges && (
                      <div className="text-xs text-muted-foreground">
                        POST-DRAIN: autoscaling=
                        {pool.postDrainChanges.autoscaling ? 'ON' : 'OFF'},
                        count={pool.postDrainChanges.nodeCount}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </section>

          <hr />

          {/* Operações de Transição */}
          <section>
            <h3 className="text-sm font-semibold mb-3">
              ⚙️ Operações de Transição
            </h3>

            <div className="space-y-4">
              <div className="flex items-start space-x-2">
                <Checkbox
                  id="cordon"
                  checked={cordonEnabled}
                  onCheckedChange={(checked) =>
                    setCordonEnabled(checked as boolean)
                  }
                />
                <div className="space-y-1">
                  <Label htmlFor="cordon" className="cursor-pointer">
                    Habilitar Cordon
                  </Label>
                  <p className="text-xs text-muted-foreground">
                    Marca nodes como unschedulable antes do drain
                  </p>
                </div>
              </div>

              <div className="flex items-start space-x-2">
                <Checkbox
                  id="drain"
                  checked={drainEnabled}
                  onCheckedChange={(checked) =>
                    setDrainEnabled(checked as boolean)
                  }
                />
                <div className="space-y-1">
                  <Label htmlFor="drain" className="cursor-pointer">
                    Habilitar Drain
                  </Label>
                  <p className="text-xs text-muted-foreground">
                    Remove pods gracefully e os migra para destino
                  </p>
                </div>
              </div>
            </div>
          </section>

          <hr />

          {/* Opções de Drain */}
          <section>
            <h3 className="text-sm font-semibold mb-3">🔧 Opções de Drain</h3>

            {/* Essenciais */}
            <div className="space-y-4">
              <h4 className="text-sm font-medium">Essenciais</h4>

              <div className="flex items-start space-x-2">
                <Checkbox
                  id="ignoreDaemonsets"
                  checked={ignoreDaemonsets}
                  onCheckedChange={(checked) =>
                    setIgnoreDaemonsets(checked as boolean)
                  }
                />
                <div className="space-y-1">
                  <Label
                    htmlFor="ignoreDaemonsets"
                    className="cursor-pointer font-mono text-xs"
                  >
                    --ignore-daemonsets
                  </Label>
                  <p className="text-xs text-muted-foreground">
                    Ignora DaemonSets (recomendado)
                  </p>
                </div>
              </div>

              <div className="flex items-start space-x-2">
                <Checkbox
                  id="deleteEmptyDir"
                  checked={deleteEmptyDirData}
                  onCheckedChange={(checked) =>
                    setDeleteEmptyDirData(checked as boolean)
                  }
                />
                <div className="space-y-1">
                  <Label
                    htmlFor="deleteEmptyDir"
                    className="cursor-pointer font-mono text-xs"
                  >
                    --delete-emptydir-data
                  </Label>
                  <p className="text-xs text-muted-foreground">
                    Permite deletar pods com volumes emptyDir
                  </p>
                </div>
              </div>

              <div className="flex items-start space-x-2">
                <Checkbox
                  id="force"
                  checked={force}
                  onCheckedChange={(checked) => setForce(checked as boolean)}
                />
                <div className="space-y-1">
                  <Label
                    htmlFor="force"
                    className="cursor-pointer font-mono text-xs"
                  >
                    --force
                  </Label>
                  <p className="text-xs text-orange-600 dark:text-orange-400">
                    ⚠️ Força remoção de pods standalone (use com cuidado!)
                  </p>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="gracePeriod">Grace Period (segundos)</Label>
                  <Input
                    id="gracePeriod"
                    type="number"
                    value={gracePeriod}
                    onChange={(e) => setGracePeriod(e.target.value)}
                    min="0"
                  />
                  <p className="text-xs text-muted-foreground">
                    Tempo de espera antes de forçar terminação
                  </p>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="timeout">Timeout</Label>
                  <Input
                    id="timeout"
                    type="text"
                    value={timeout}
                    onChange={(e) => setTimeout(e.target.value)}
                    placeholder="5m, 300s, 10m"
                  />
                  <p className="text-xs text-muted-foreground">
                    Timeout total da operação
                  </p>
                </div>
              </div>
            </div>

            {/* Avançadas (Accordion) */}
            <div className="mt-4">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setShowAdvanced(!showAdvanced)}
                className="w-full justify-start"
              >
                {showAdvanced ? '▼' : '▶'} Avançadas (clique para expandir)
              </Button>

              {showAdvanced && (
                <div className="mt-4 space-y-4 pl-4 border-l-2 border-muted">
                  <div className="flex items-start space-x-2">
                    <Checkbox
                      id="disableEviction"
                      checked={disableEviction}
                      onCheckedChange={(checked) =>
                        setDisableEviction(checked as boolean)
                      }
                    />
                    <div className="space-y-1">
                      <Label
                        htmlFor="disableEviction"
                        className="cursor-pointer font-mono text-xs"
                      >
                        --disable-eviction
                      </Label>
                      <p className="text-xs text-muted-foreground">
                        Usa DELETE ao invés de Eviction API (não respeita PDBs)
                      </p>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="skipWait">
                      Skip Wait Timeout (segundos)
                    </Label>
                    <Input
                      id="skipWait"
                      type="number"
                      value={skipWaitTimeout}
                      onChange={(e) => setSkipWaitTimeout(e.target.value)}
                      min="0"
                    />
                    <p className="text-xs text-muted-foreground">
                      Timeout para aguardar deleção de pods
                    </p>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="podSelector">Pod Selector</Label>
                    <Input
                      id="podSelector"
                      type="text"
                      value={podSelector}
                      onChange={(e) => setPodSelector(e.target.value)}
                      placeholder="app=nginx,tier!=frontend"
                    />
                    <p className="text-xs text-muted-foreground">
                      Label selector para filtrar pods
                    </p>
                  </div>

                  <div className="flex items-start space-x-2">
                    <Checkbox
                      id="dryRun"
                      checked={dryRun}
                      onCheckedChange={(checked) =>
                        setDryRun(checked as boolean)
                      }
                    />
                    <div className="space-y-1">
                      <Label
                        htmlFor="dryRun"
                        className="cursor-pointer font-mono text-xs"
                      >
                        --dry-run
                      </Label>
                      <p className="text-xs text-muted-foreground">
                        Simular operação sem executar
                      </p>
                    </div>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="chunkSize">Chunk Size (nodes)</Label>
                    <Input
                      id="chunkSize"
                      type="number"
                      value={chunkSize}
                      onChange={(e) => setChunkSize(e.target.value)}
                      min="1"
                    />
                    <p className="text-xs text-muted-foreground">
                      Quantos nodes drenar em paralelo
                    </p>
                  </div>
                </div>
              )}
            </div>
          </section>

          <hr />

          {/* Fluxo de Execução (Preview) */}
          <section>
            <h3 className="text-sm font-semibold mb-3">
              📊 Fluxo de Execução
            </h3>

            <div className="space-y-3 text-sm">
              <div className="flex items-start gap-2">
                <Badge variant="secondary">1️⃣</Badge>
                <div>
                  <div className="font-medium">FASE PRE-DRAIN</div>
                  <p className="text-muted-foreground">
                    Ajustar {dest?.name} (destino) para receber pods
                  </p>
                  {dest?.preDrainChanges && (
                    <p className="text-xs text-muted-foreground">
                      → Min={dest.preDrainChanges.minNodes}, Max=
                      {dest.preDrainChanges.maxNodes}, Autoscaling=ON
                    </p>
                  )}
                </div>
              </div>

              <div className="flex items-start gap-2">
                <Badge variant="secondary">2️⃣</Badge>
                <div>
                  <div className="font-medium">AGUARDAR NODES READY (30s)</div>
                  <p className="text-muted-foreground">
                    Aguardar nodes do destino ficarem Ready
                  </p>
                </div>
              </div>

              <div className="flex items-start gap-2">
                <Badge variant="secondary">3️⃣</Badge>
                <div>
                  <div className="font-medium">CORDON</div>
                  <p className="text-muted-foreground">
                    Marcar nodes do {origin?.name} (origem) como unschedulable
                  </p>
                </div>
              </div>

              <div className="flex items-start gap-2">
                <Badge variant="secondary">4️⃣</Badge>
                <div>
                  <div className="font-medium">DRAIN</div>
                  <p className="text-muted-foreground">
                    Migrar pods de {origin?.name} → {dest?.name}
                  </p>
                  {(ignoreDaemonsets ||
                    deleteEmptyDirData ||
                    force) && (
                    <p className="text-xs text-muted-foreground font-mono">
                      Com flags:{' '}
                      {[
                        ignoreDaemonsets && '--ignore-daemonsets',
                        deleteEmptyDirData && '--delete-emptydir-data',
                        force && '--force',
                      ]
                        .filter(Boolean)
                        .join(' ')}
                    </p>
                  )}
                </div>
              </div>

              <div className="flex items-start gap-2">
                <Badge variant="secondary">5️⃣</Badge>
                <div>
                  <div className="font-medium">FASE POST-DRAIN</div>
                  <p className="text-muted-foreground">
                    Ajustar {origin?.name} (origem) para desligar
                  </p>
                  {origin?.postDrainChanges && (
                    <p className="text-xs text-muted-foreground">
                      → Autoscaling=OFF, NodeCount=0
                    </p>
                  )}
                </div>
              </div>
            </div>
          </section>

          {/* Erros de validação */}
          {errors.length > 0 && (
            <Alert variant="destructive">
              <AlertTriangle className="h-4 w-4" />
              <AlertDescription>
                <ul className="list-disc list-inside">
                  {errors.map((error, i) => (
                    <li key={i}>{error}</li>
                  ))}
                </ul>
              </AlertDescription>
            </Alert>
          )}

          {/* Aviso de downtime */}
          <Alert>
            <Info className="h-4 w-4" />
            <AlertDescription>
              <strong>⏱️ Tempo estimado: ~7 minutos</strong>
              <br />
              ⚠️ Esta operação pode causar downtime temporário se o destino não
              tiver capacidade suficiente.
            </AlertDescription>
          </Alert>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onCancel}>
            Cancelar
          </Button>
          <Button variant="secondary" onClick={validate}>
            🔍 Validar Configuração
          </Button>
          <Button onClick={handleConfirm}>✅ Executar Sequenciamento</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
