import { useHelmStore } from '../store/helmStore.tsx';
import { Card } from './ui/card';
import { Badge } from './ui/badge';
import { Loader2, AlertCircle, Package } from 'lucide-react';
import { cn } from '../lib/utils';
import type { ReleaseSummary } from '../types/helm';

// Função para formatar versão de x-x-x-x para x.x.x-x (semver)
const formatVersion = (version: string | undefined): string => {
  if (!version) return '';
  const parts = version.split('-');
  if (parts.length === 4 && parts.every(p => /^\d+$/.test(p)))
    return `${parts[0]}.${parts[1]}.${parts[2]}-${parts[3]}`;
  return version;
};

interface HelmReleaseListProps {
  cluster: string;
  releases: ReleaseSummary[];
  onSelectRelease: (releaseName: string, namespace: string) => void;
}

export const HelmReleaseList = ({ cluster, releases, onSelectRelease }: HelmReleaseListProps) => {
  const {
    releasesLoading,
    releasesError,
    selectedRelease,
  } = useHelmStore();

  if (releasesLoading) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="flex flex-col items-center gap-2">
          <Loader2 className="h-8 w-8 animate-spin text-primary" />
          <p className="text-sm text-muted-foreground">Sincronizando releases Helm...</p>
        </div>
      </div>
    );
  }

  if (releasesError) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="flex flex-col items-center gap-2 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <p className="text-sm font-medium text-destructive">Erro ao carregar releases</p>
          <p className="text-xs text-muted-foreground">{releasesError}</p>
        </div>
      </div>
    );
  }

  if (!releases || releases.length === 0) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="flex flex-col items-center gap-2 text-center">
          <Package className="h-12 w-12 text-muted-foreground/50" />
          <p className="text-sm font-medium text-muted-foreground">Nenhum release encontrado</p>
          <p className="text-xs text-muted-foreground">
            Instale um chart Helm ou ajuste os filtros
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-2">
      {releases.map((release) => (
        <ReleaseCard
          key={`${release.namespace}-${release.name}`}
          release={release}
          isSelected={selectedRelease === release.name}
          onClick={() => onSelectRelease(release.name, release.namespace)}
        />
      ))}
    </div>
  );
};

interface ReleaseCardProps {
  release: ReleaseSummary;
  isSelected: boolean;
  onClick: () => void;
}

const ReleaseCard = ({ release, isSelected, onClick }: ReleaseCardProps) => {
  const getStatusColor = (status: string) => {
    const statusLower = status.toLowerCase();
    if (statusLower === 'deployed') return 'bg-green-500/10 text-green-500 border-green-500/20';
    if (statusLower === 'failed') return 'bg-red-500/10 text-red-500 border-red-500/20';
    if (statusLower.includes('pending')) return 'bg-yellow-500/10 text-yellow-500 border-yellow-500/20';
    if (statusLower === 'uninstalling') return 'bg-orange-500/10 text-orange-500 border-orange-500/20';
    return 'bg-blue-500/10 text-blue-500 border-blue-500/20';
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

  return (
    <Card
      className={cn(
        'p-3 cursor-pointer transition-all hover:border-primary/50',
        isSelected && 'border-primary bg-primary/5'
      )}
      onClick={onClick}
    >
      <div className="space-y-2">
        <div className="flex items-start justify-between gap-2">
          <div className="flex-1 min-w-0">
            <h4 className="text-sm font-semibold text-foreground truncate">
              {release.name}
            </h4>
            <p className="text-xs text-muted-foreground truncate">
              {release.namespace}
            </p>
          </div>
          <Badge className={cn('text-xs', getStatusColor(release.status))}>
            {release.status}
          </Badge>
        </div>

        <div className="grid grid-cols-2 gap-2 text-xs">
          <div>
            <span className="text-muted-foreground">Chart:</span>
            <p className="font-medium text-foreground truncate">{release.chart}</p>
          </div>
          <div>
            <span className="text-muted-foreground">App Version:</span>
            <p className="font-medium text-foreground truncate">{formatVersion(release.appVersion) || 'N/A'}</p>
          </div>
        </div>

        <div className="flex items-center justify-between text-xs">
          <span className="text-muted-foreground">
            Revisão {release.revision}
          </span>
          <span className="text-muted-foreground">
            {formatDate(release.updatedAt)}
          </span>
        </div>

        {release.hasPendingUpgrade && (
          <Badge variant="outline" className="text-xs w-full justify-center">
            ⏳ Upgrade Pendente
          </Badge>
        )}
      </div>
    </Card>
  );
};
