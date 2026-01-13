import { useState, useEffect, useMemo } from 'react';
import { SplitView } from './SplitView';
import { HelmReleaseList } from './HelmReleaseList.tsx';
import { HelmReleaseDetails } from './HelmReleaseDetails.tsx';
import { HelmProvider, useHelmStore } from '../store/helmStore.tsx';
import { useHelmReleases, useHelmRelease, useHelmHistory } from '../hooks/useHelm';
import { Input } from './ui/input';
import { Button } from './ui/button';
import { Search, RefreshCw, Eye, EyeOff, X } from 'lucide-react';
import { HelmInstallModal } from './HelmInstallModal.tsx';
import { toast } from 'sonner';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './ui/select';

interface HelmTabProps {
  selectedCluster: string;
}

const HelmTabContent = ({ selectedCluster }: HelmTabProps) => {
  const {
    releases,
    selectedRelease,
    selectedReleaseNamespace,
    releaseDetail,
    releaseDetailError,
    filters,
    setFilters,
    selectRelease,
  } = useHelmStore();

  const [searchQuery, setSearchQuery] = useState("");
  const [showInstallModal, setShowInstallModal] = useState(false);
  const [showSystemNamespaces, setShowSystemNamespaces] = useState(false);

  // Lista de namespaces de sistema (sincronizada com backend)
  const isSystemNamespace = (namespace: string): boolean => {
    const systemNamespaces = [
      'default',
      'kube-system',
      'kube-public',
      'kube-node-lease',
      'keycloak',
      'gatekeeper-system',
      'istio-system',
      'istio-injection',
      'cert-manager',
      'elastic-system',
      'logging',
      'dynatrace',
      'flux-system',
      'argocd',
      'dsv',
      'monitoring',
      'guardicore',
      'guardicore-orch',
      'cattle-system',
      'longhorn-system',
      'metallb-system',
      'calico-system',
      'tigera-operator',
    ];
    return systemNamespaces.includes(namespace);
  };

  // Coletar namespaces únicos dos releases
  const availableNamespaces = useMemo(() => {
    const namespaces = new Set<string>();
    releases.forEach(release => {
      // Aplicar filtro de sistema aos namespaces
      if (showSystemNamespaces || !isSystemNamespace(release.namespace)) {
        namespaces.add(release.namespace);
      }
    });
    return Array.from(namespaces).sort();
  }, [releases, showSystemNamespaces]);

  // Fetch releases
  const { refetch: refetchReleases } = useHelmReleases({
    cluster: selectedCluster,
  });

  // Fetch release details when selected
  useHelmRelease(
    selectedRelease && selectedReleaseNamespace
      ? {
          cluster: selectedCluster,
          release: selectedRelease,
          namespace: selectedReleaseNamespace,
        }
      : null
  );

  // Fetch release history
  useHelmHistory(
    selectedRelease && selectedReleaseNamespace
      ? {
          cluster: selectedCluster,
          release: selectedRelease,
          namespace: selectedReleaseNamespace,
        }
      : null
  );

  // Clear selection if release detail has error (not found)
  useEffect(() => {
    if (releaseDetailError && releaseDetailError.includes('not found') && selectedRelease) {
      console.log('[HelmTab] Auto-clearing selection due to not found error');
      selectRelease(null);
    }
  }, [releaseDetailError, selectedRelease, selectRelease]);

  // Clear namespace filter if it becomes unavailable when toggling system namespaces
  useEffect(() => {
    if (filters.namespace && !availableNamespaces.includes(filters.namespace)) {
      setFilters({ namespace: '' });
    }
  }, [availableNamespaces, filters.namespace, setFilters]);

  // Filtrar releases client-side (padrão DeploymentsTab)
  const filteredReleases = useMemo(() => {
    let filtered = releases;

    // Filtro de namespaces de sistema
    if (!showSystemNamespaces) {
      filtered = filtered.filter(release => !isSystemNamespace(release.namespace));
    }

    // Filtro de namespace selecionado
    if (filters.namespace) {
      filtered = filtered.filter(release => release.namespace === filters.namespace);
    }

    // Busca dinâmica (nome, namespace, chart, appVersion, status)
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      filtered = filtered.filter(release =>
        release.name.toLowerCase().includes(query) ||
        release.namespace.toLowerCase().includes(query) ||
        release.chart.toLowerCase().includes(query) ||
        (release.appVersion && release.appVersion.toLowerCase().includes(query)) ||
        release.status.toLowerCase().includes(query)
      );
    }

    return filtered;
  }, [releases, showSystemNamespaces, filters.namespace, searchQuery]);

  const handleRefresh = () => {
    refetchReleases();
    // Clear selection if there's an error
    if (releaseDetailError) {
      selectRelease(null);
    }
  };

  const handleNamespaceChange = (value: string) => {
    setFilters({ namespace: value === 'all' ? '' : value });
  };

  const leftPanel = {
    title: 'Helm Releases',
    titleAction: (
      <div className="flex items-center gap-2 flex-wrap">
        <Select
          value={filters.namespace || 'all'}
          onValueChange={handleNamespaceChange}
        >
          <SelectTrigger className="h-8 w-[180px]">
            <SelectValue placeholder="Todos os namespaces" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">Todos os namespaces</SelectItem>
            {availableNamespaces.map(ns => (
              <SelectItem key={ns} value={ns}>{ns}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          variant={showSystemNamespaces ? "secondary" : "outline"}
          size="sm"
          onClick={() => setShowSystemNamespaces(!showSystemNamespaces)}
          title={showSystemNamespaces ? "Ocultar namespaces de sistema" : "Mostrar namespaces de sistema"}
          className="h-8"
        >
          {showSystemNamespaces ? <Eye className="w-4 h-4 mr-1" /> : <EyeOff className="w-4 h-4 mr-1" />}
          Sistema
        </Button>
        <Button
          variant="ghost"
          size="icon"
          onClick={handleRefresh}
          className="h-8 w-8"
        >
          <RefreshCw className="h-4 w-4" />
        </Button>
      </div>
    ),
    content: (
      <div className="flex flex-col h-full gap-3">
        {/* Search Filter - Busca Dinâmica */}
        <div className="relative">
          <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Buscar por nome, namespace, chart, versão..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-8 pr-8"
          />
          {searchQuery && (
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setSearchQuery("")}
              className="absolute right-0 top-0 h-10 w-10 hover:bg-transparent"
              title="Limpar busca"
            >
              <X className="h-4 w-4 text-muted-foreground hover:text-foreground" />
            </Button>
          )}
        </div>

        {/* Release List */}
        <div className="flex-1 overflow-auto">
          <HelmReleaseList
            cluster={selectedCluster}
            releases={filteredReleases}
            onSelectRelease={selectRelease}
          />
        </div>
      </div>
    ),
  };

  const handleRefreshAfterOperation = async () => {
    // Salvar release atual para re-selecionar depois
    const currentRelease = selectedRelease;
    const currentNamespace = selectedReleaseNamespace;

    try {
      // Atualizar lista de releases
      await refetchReleases();

      // Toast de sucesso
      toast.success('Dados atualizados', {
        description: 'Lista de releases atualizada com sucesso',
      });

      // Re-selecionar release se ainda existir
      if (currentRelease && currentNamespace) {
        // Pequeno delay para garantir que dados foram atualizados
        setTimeout(() => {
          selectRelease(currentRelease, currentNamespace);
        }, 300);
      }
    } catch (error) {
      toast.error('Erro ao atualizar dados', {
        description: error instanceof Error ? error.message : 'Erro desconhecido',
      });
    }
  };

  const handleRefreshReleaseDetails = async () => {
    // Força re-fetch dos dados do release atual
    if (selectedRelease && selectedReleaseNamespace) {
      try {
        // Limpar seleção temporariamente
        selectRelease(null);

        // Pequeno delay para garantir limpeza
        setTimeout(() => {
          selectRelease(selectedRelease, selectedReleaseNamespace);
          toast.success('Release atualizado', {
            description: 'Dados do release recarregados com sucesso',
          });
        }, 100);
      } catch (error) {
        toast.error('Erro ao atualizar release', {
          description: error instanceof Error ? error.message : 'Erro desconhecido',
        });
      }
    }
  };

  const rightPanel = {
    title: selectedRelease ? `Release: ${selectedRelease}` : 'Detalhes',
    titleAction: selectedRelease ? (
      <Button
        variant="ghost"
        size="sm"
        onClick={handleRefreshReleaseDetails}
      >
        <RefreshCw className="h-4 w-4 mr-1" />
        Atualizar
      </Button>
    ) : null,
    content: (
      <HelmReleaseDetails
        cluster={selectedCluster}
        release={selectedRelease}
        onInstallClick={() => setShowInstallModal(true)}
        onRefreshNeeded={handleRefreshAfterOperation}
      />
    ),
  };

  return (
    <>
      <SplitView leftPanel={leftPanel} rightPanel={rightPanel} />
      
      <HelmInstallModal
        open={showInstallModal}
        onOpenChange={setShowInstallModal}
        cluster={selectedCluster}
        namespace={filters.namespace}
        onSuccess={() => {
          refetchReleases();
        }}
      />
    </>
  );
};

// Export with Provider wrapper
export const HelmTab = (props: HelmTabProps) => {
  return (
    <HelmProvider>
      <HelmTabContent {...props} />
    </HelmProvider>
  );
};
