import { useState, useMemo } from "react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import { Loader2, Search, X } from "lucide-react";
import { Button } from "@/components/ui/button";

interface ClusterSelectorForTabProps {
  selectedCluster: string;
  onClusterChange: (value: string) => void;
  clusters: string[];
  tabLabel: string;
  isLoading?: boolean;
}

export const ClusterSelectorForTab = ({
  selectedCluster,
  onClusterChange,
  clusters,
  tabLabel,
  isLoading = false
}: ClusterSelectorForTabProps) => {
  const [searchTerm, setSearchTerm] = useState("");

  // Filtra clusters baseado no termo de busca
  const filteredClusters = useMemo(() => {
    if (!searchTerm) return clusters;

    const term = searchTerm.toLowerCase();
    return clusters.filter(cluster =>
      cluster.toLowerCase().includes(term)
    );
  }, [clusters, searchTerm]);

  // Limpa a busca
  const clearSearch = () => {
    setSearchTerm("");
  };

  return (
    <div className="px-6 py-3 bg-muted/30 border-b">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-muted-foreground">
            {tabLabel} - Cluster Context:
          </span>
          {isLoading && (
            <Loader2 className="w-4 h-4 animate-spin text-primary" />
          )}
        </div>

        <div className="flex items-center gap-2">
          {/* Campo de busca */}
          <div className="relative w-[200px]">
            <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
            <Input
              type="text"
              placeholder="Buscar cluster..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="pl-8 pr-8 h-9"
            />
            {searchTerm && (
              <Button
                variant="ghost"
                size="sm"
                onClick={clearSearch}
                className="absolute right-0 top-1/2 -translate-y-1/2 h-7 w-7 p-0 hover:bg-transparent"
              >
                <X className="w-4 h-4 text-muted-foreground hover:text-foreground" />
              </Button>
            )}
          </div>

          {/* Select de cluster */}
          <Select
            value={selectedCluster}
            onValueChange={onClusterChange}
            disabled={isLoading}
          >
            <SelectTrigger className="w-[280px]">
              <SelectValue placeholder="Selecione um cluster..." />
            </SelectTrigger>
            <SelectContent>
              {filteredClusters.length > 0 ? (
                filteredClusters.map((cluster) => (
                  <SelectItem key={cluster} value={cluster}>
                    {cluster}
                  </SelectItem>
                ))
              ) : (
                <div className="px-2 py-6 text-center text-sm text-muted-foreground">
                  Nenhum cluster encontrado com "{searchTerm}"
                </div>
              )}
            </SelectContent>
          </Select>
        </div>
      </div>
    </div>
  );
};