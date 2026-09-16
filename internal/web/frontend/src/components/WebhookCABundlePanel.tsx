import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Loader2, RefreshCcw, Webhook, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { getStatusBadge } from "@/components/CertificateDetailModal";
import { WebhookCABundleUpdateModal } from "@/components/WebhookCABundleUpdateModal";
import { ClusterSelectorForTab } from "@/components/ClusterSelectorForTab";
import { ProtectedAction } from "@/components/rbac";
import { useClusters } from "@/hooks/useAPI";
import { useCertificates } from "@/hooks/useCertificates";
import type { MutatingWebhookConfigSummary } from "@/types/certificates";

// Painel "Webhooks (CA Bundle)" — atualiza o clientConfig.caBundle de MutatingWebhookConfiguration
// (objeto cluster-scoped nativo do K8s, não um Secret) pra rotacionar a confiança do apiserver em
// webhooks de terceiro (ex: Delinea DSV injector, Istio sidecar injector) quando o certificado de
// serviço deles é renovado mas o objeto de configuração — normalmente escrito uma única vez pelo
// instalador do produto — nunca é atualizado junto.
export function WebhookCABundlePanel() {
  const { clusters } = useClusters();
  const { listMutatingWebhooks } = useCertificates();

  const [cluster, setCluster] = useState("");
  const [configs, setConfigs] = useState<MutatingWebhookConfigSummary[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [updateTarget, setUpdateTarget] = useState<MutatingWebhookConfigSummary | null>(null);

  const fetchConfigs = async (targetCluster: string) => {
    if (!targetCluster) return;
    setLoading(true);
    try {
      const result = await listMutatingWebhooks(targetCluster);
      setConfigs(result);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Erro ao listar MutatingWebhookConfigurations");
      setConfigs(null);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="h-full flex flex-col min-h-0">
      <div className="flex flex-wrap items-center gap-3 px-1 pb-3 flex-shrink-0">
        <div className="min-w-[220px]">
          <ClusterSelectorForTab
            selectedCluster={cluster}
            onClusterChange={(v) => {
              setCluster(v);
              setConfigs(null);
              if (v) fetchConfigs(v);
            }}
            clusters={clusters.map((c) => c.context)}
            tabLabel="Webhooks"
            clusterProviders={Object.fromEntries(clusters.map((c) => [c.context, c.cloud_provider || "unknown"]))}
            clusterJourneys={Object.fromEntries(clusters.map((c) => [c.context, c.journey || ""]))}
          />
        </div>
        <Button variant="outline" size="sm" disabled={!cluster || loading} onClick={() => fetchConfigs(cluster)}>
          {loading ? <Loader2 className="h-3.5 w-3.5 mr-1.5 animate-spin" /> : <RefreshCcw className="h-3.5 w-3.5 mr-1.5" />}
          Atualizar
        </Button>
        <p className="text-xs text-muted-foreground">
          Lista os MutatingWebhookConfiguration do cluster e permite sobrescrever o CA confiado
          pelo apiserver ao chamar cada webhook (ex: Delinea DSV injector, Istio sidecar injector).
        </p>
      </div>

      <ScrollArea className="flex-1 min-h-0">
        {!cluster && (
          <div className="text-sm text-muted-foreground p-4">
            Selecione um cluster para listar os MutatingWebhookConfiguration.
          </div>
        )}

        {cluster && !loading && configs !== null && configs.length === 0 && (
          <div className="text-sm text-muted-foreground p-4">
            Nenhum MutatingWebhookConfiguration encontrado neste cluster.
          </div>
        )}

        {configs && configs.length > 0 && (
          <div className="space-y-3 p-1">
            {configs.map((cfg) => (
              <div key={cfg.name} className="rounded-md border p-3 space-y-2">
                <div className="flex items-center justify-between gap-2 flex-wrap">
                  <div className="flex items-center gap-2 min-w-0">
                    <Webhook className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                    <span className="font-mono text-sm truncate" title={cfg.name}>{cfg.name}</span>
                    <Badge variant="secondary" className="text-xs flex-shrink-0">
                      {cfg.webhooks.length} entrada(s)
                    </Badge>
                  </div>
                  <ProtectedAction>
                    <Button size="sm" onClick={() => setUpdateTarget(cfg)}>
                      Atualizar CA Bundle
                    </Button>
                  </ProtectedAction>
                </div>

                <div className="space-y-1.5">
                  {cfg.webhooks.map((wh) => (
                    <div key={wh.name} className="flex items-center gap-2 text-xs pl-6 flex-wrap">
                      <span className="font-mono text-muted-foreground truncate max-w-[260px]" title={wh.name}>
                        {wh.name}
                      </span>
                      {(wh.serviceName || wh.url) && (
                        <span className="flex items-center gap-1 text-muted-foreground">
                          <ExternalLink className="h-3 w-3" />
                          {wh.serviceName ? `${wh.serviceNamespace}/${wh.serviceName}` : wh.url}
                        </span>
                      )}
                      {wh.caBundleEmpty ? (
                        <Badge variant="secondary" className="text-xs">caBundle vazio</Badge>
                      ) : (
                        <>
                          {wh.caBundleStatus && getStatusBadge(wh.caBundleStatus)}
                          <span className="text-muted-foreground truncate">
                            {wh.caBundleSubject}
                            {wh.caBundleDays !== undefined && ` · ${wh.caBundleDays}d restantes`}
                          </span>
                        </>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </ScrollArea>

      <WebhookCABundleUpdateModal
        open={updateTarget !== null}
        onOpenChange={(open) => {
          if (!open) setUpdateTarget(null);
        }}
        cluster={cluster}
        config={updateTarget}
        onSuccess={() => {
          setUpdateTarget(null);
          fetchConfigs(cluster);
        }}
      />
    </div>
  );
}
