import { useState, useEffect, useMemo } from 'react';
import { SplitView } from './SplitView';
import { HelmReleaseList } from './HelmReleaseList.tsx';
import { HelmReleaseDetails } from './HelmReleaseDetails.tsx';
import { HelmProvider, useHelmStore } from '../store/helmStore.tsx';
import { useHelmReleases, useHelmRelease, useHelmHistory } from '../hooks/useHelm';
import { Input } from './ui/input';
import { Button } from './ui/button';
import { Search, RefreshCw, Filter, Plus, Eye, EyeOff } from 'lucide-react';
import { HelmInstallModal } from './HelmInstallModal.tsx';
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

  const [searchInput, setSearchInput] = useState(filters.search);
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

  // Sync search input with store
  useEffect(() => {
    setSearchInput(filters.search);
  }, [filters.search]);

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

  const handleSearchChange = (value: string) => {
    setSearchInput(value);
  };

  const handleSearchSubmit = () => {
    setFilters({ search: searchInput });
  };

  const handleSearchKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleSearchSubmit();
    }
  };

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
        {/* Search Filter */}
        <div className="flex items-center gap-2">
          <div className="relative flex-1">
            <Search className="absolute left-2 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              placeholder="Buscar releases..."
              value={searchInput}
              onChange={(e) => handleSearchChange(e.target.value)}
              onKeyDown={handleSearchKeyDown}
              className="pl-8"
            />
          </div>
          <Button
            variant="outline"
            size="icon"
            onClick={handleSearchSubmit}
            className="h-10 w-10"
          >
            <Filter className="h-4 w-4" />
          </Button>
        </div>

        {/* Release List */}
        <div className="flex-1 overflow-auto">
          <HelmReleaseList
            cluster={selectedCluster}
            onSelectRelease={selectRelease}
            showSystemNamespaces={showSystemNamespaces}
            isSystemNamespace={isSystemNamespace}
          />
        </div>
      </div>
    ),
  };

  const rightPanel = {
    title: selectedRelease ? `Release: ${selectedRelease}` : 'Detalhes',
    titleAction: selectedRelease ? (
      <Button
        variant="ghost"
        size="sm"
        onClick={() => selectRelease(null)}
      >
        Fechar
      </Button>
    ) : null,
    content: (
      <HelmReleaseDetails
        cluster={selectedCluster}
        release={selectedRelease}
        onInstallClick={() => setShowInstallModal(true)}
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
