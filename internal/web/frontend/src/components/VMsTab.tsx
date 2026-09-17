import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Loader2, RefreshCcw, Play, Square, RotateCw, Server } from "lucide-react";
import { toast } from "sonner";
import { ProtectedAction } from "@/components/rbac";
import { useVMAwsProfiles, useVMInstances } from "@/hooks/useVMs";
import { apiClient } from "@/lib/api/client";
import type { VMInstance, VMPowerState } from "@/lib/api/types";

// Regiões usadas pra descoberta automática de EKS (internal/config/eks_discovery.go,
// eksDefaultRegions) — reaproveitadas aqui como atalhos, já que são as regiões reais que esta
// organização usa; o campo continua sendo um <Input> livre, não um <Select> fechado, pra não
// bloquear uma região fora dessa lista.
const REGION_SHORTCUTS = ["us-east-1", "us-east-2", "us-west-2", "sa-east-1"];

function powerStateBadge(state: VMPowerState) {
  switch (state) {
    case "running":
      return <Badge className="bg-green-500/20 text-green-400 border-green-500/30">Rodando</Badge>;
    case "stopped":
      return <Badge className="bg-red-500/20 text-red-400 border-red-500/30">Parada</Badge>;
    case "pending":
      return <Badge className="bg-yellow-500/20 text-yellow-400 border-yellow-500/30">Iniciando</Badge>;
    case "stopping":
    case "shutting-down":
      return <Badge className="bg-yellow-500/20 text-yellow-400 border-yellow-500/30">Parando</Badge>;
    case "terminated":
      return <Badge variant="secondary">Terminada</Badge>;
    default:
      return <Badge variant="secondary">{state}</Badge>;
  }
}

type PowerAction = "start" | "stop" | "reboot";

const ACTION_LABEL: Record<PowerAction, string> = {
  start: "iniciar",
  stop: "parar",
  reboot: "reiniciar",
};

// VMsTab — aba "VMs / EC2" (Fase 2 do plano: inventário + power actions, sem terminal/SFTP ainda,
// ver /home/paulo/.claude/plans/scalable-greeting-kazoo.md). Hoje só AWS EC2 — o seletor de
// provider fica implícito (não exposto na UI) até um segundo provider (Azure VM/GCE) existir de
// fato, evitando uma escolha que sempre daria no mesmo resultado.
export default function VMsTab() {
  const { profiles, loading: loadingProfiles } = useVMAwsProfiles();
  const [profile, setProfile] = useState("");
  const [region, setRegion] = useState("us-east-1");
  const { instances, loading, error, refetch } = useVMInstances(profile, region);

  const [pendingAction, setPendingAction] = useState<{ instance: VMInstance; action: PowerAction } | null>(null);
  const [actingOn, setActingOn] = useState<string | null>(null);

  const handleConfirm = async () => {
    if (!pendingAction) return;
    const { instance, action } = pendingAction;
    setPendingAction(null);
    setActingOn(instance.id);
    try {
      if (action === "start") await apiClient.startVMInstance(instance.id, profile, region);
      else if (action === "stop") await apiClient.stopVMInstance(instance.id, profile, region);
      else await apiClient.rebootVMInstance(instance.id, profile, region);
      toast.success(`Ação "${ACTION_LABEL[action]}" disparada para ${instance.name}`);
      // O estado real (running/stopped) demora alguns segundos pra refletir na AWS — o cache do
      // backend já foi invalidado no momento da ação (vmInstanceCacheTTL), então este refetch já
      // busca dado fresco, mas pode ainda mostrar o estado transitório (pending/stopping).
      refetch();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : `Falha ao ${ACTION_LABEL[action]} a instância`);
    } finally {
      setActingOn(null);
    }
  };

  return (
    <div className="h-full flex flex-col min-h-0 p-4 gap-4">
      <div className="flex flex-wrap items-end gap-3 flex-shrink-0">
        <div className="space-y-1.5 min-w-[200px]">
          <Label className="text-xs">Profile AWS</Label>
          <Select value={profile} onValueChange={setProfile} disabled={loadingProfiles}>
            <SelectTrigger>
              <SelectValue placeholder={loadingProfiles ? "Carregando..." : "Selecione um profile..."} />
            </SelectTrigger>
            <SelectContent>
              {profiles.map((p) => (
                <SelectItem key={p} value={p}>
                  {p}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="space-y-1.5 min-w-[160px]">
          <Label className="text-xs">Região</Label>
          <Input value={region} onChange={(e) => setRegion(e.target.value)} placeholder="us-east-1" />
        </div>

        <div className="flex gap-1 pb-0.5">
          {REGION_SHORTCUTS.map((r) => (
            <Button key={r} variant={region === r ? "default" : "outline"} size="sm" onClick={() => setRegion(r)}>
              {r}
            </Button>
          ))}
        </div>

        <Button variant="outline" size="sm" disabled={!profile || !region || loading} onClick={() => refetch({ refresh: true })}>
          {loading ? <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" /> : <RefreshCcw className="h-3.5 w-3.5 mr-1.5" />}
          Atualizar
        </Button>
      </div>

      <ScrollArea className="flex-1 min-h-0 border rounded-md">
        {!profile && (
          <div className="text-sm text-muted-foreground p-6 text-center">
            Selecione um profile AWS para listar as instâncias EC2.
          </div>
        )}

        {profile && error && (
          <div className="text-sm text-red-500 p-6 text-center">{error}</div>
        )}

        {profile && !error && !loading && instances.length === 0 && (
          <div className="text-sm text-muted-foreground p-6 text-center">
            Nenhuma instância encontrada em {region} para o profile "{profile}".
          </div>
        )}

        {instances.length > 0 && (
          <div className="divide-y">
            {instances.map((inst) => (
              <div key={inst.id} className="flex items-center gap-3 p-3 flex-wrap">
                <Server className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                <div className="min-w-[180px]">
                  <div className="text-sm font-medium truncate" title={inst.name}>{inst.name}</div>
                  <div className="text-xs text-muted-foreground font-mono">{inst.id}</div>
                </div>
                {powerStateBadge(inst.state)}
                <span className="text-xs text-muted-foreground">{inst.instanceType}</span>
                <span className="text-xs text-muted-foreground">{inst.os === "windows" ? "🪟 Windows" : "🐧 Linux"}</span>
                {inst.privateIp && <span className="text-xs text-muted-foreground font-mono">priv: {inst.privateIp}</span>}
                {inst.publicIp && <span className="text-xs text-muted-foreground font-mono">pub: {inst.publicIp}</span>}
                <div className="flex items-center gap-1 flex-wrap">
                  {(inst.supportedConnectionModes ?? []).includes("ssm") && (
                    <Badge variant="secondary" className="text-xs">SSM Agent detectado</Badge>
                  )}
                </div>

                <div className="ml-auto flex items-center gap-1">
                  <ProtectedAction>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={inst.state === "running" || actingOn === inst.id}
                      onClick={() => setPendingAction({ instance: inst, action: "start" })}
                      title="Iniciar"
                    >
                      {actingOn === inst.id ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}
                    </Button>
                  </ProtectedAction>
                  <ProtectedAction>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={inst.state !== "running" || actingOn === inst.id}
                      onClick={() => setPendingAction({ instance: inst, action: "stop" })}
                      title="Parar"
                    >
                      <Square className="h-3.5 w-3.5" />
                    </Button>
                  </ProtectedAction>
                  <ProtectedAction>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={inst.state !== "running" || actingOn === inst.id}
                      onClick={() => setPendingAction({ instance: inst, action: "reboot" })}
                      title="Reiniciar"
                    >
                      <RotateCw className="h-3.5 w-3.5" />
                    </Button>
                  </ProtectedAction>
                </div>
              </div>
            ))}
          </div>
        )}
      </ScrollArea>

      <AlertDialog open={pendingAction !== null} onOpenChange={(open) => !open && setPendingAction(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Confirmar {pendingAction ? ACTION_LABEL[pendingAction.action] : ""} instância?
            </AlertDialogTitle>
            <AlertDialogDescription>
              Isso vai {pendingAction ? ACTION_LABEL[pendingAction.action] : ""} a instância{" "}
              <strong>{pendingAction?.instance.name}</strong> ({pendingAction?.instance.id}) no profile "{profile}"
              região {region}. A transição de estado não é instantânea — pode levar alguns segundos/minutos pra
              refletir na AWS.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancelar</AlertDialogCancel>
            <AlertDialogAction onClick={handleConfirm}>Confirmar</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
