import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Loader2 } from "lucide-react";

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
  return (
    <div className="px-6 py-3 bg-muted/30 border-b">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-muted-foreground">
            {tabLabel} - Cluster Context:
          </span>
          {isLoading && (
            <Loader2 className="w-4 h-4 animate-spin text-primary" />
          )}
        </div>
        <Select 
          value={selectedCluster} 
          onValueChange={onClusterChange}
          disabled={isLoading}
        >
          <SelectTrigger className="w-[280px]">
            <SelectValue placeholder="Select a cluster..." />
          </SelectTrigger>
          <SelectContent>
            {clusters.map((cluster) => (
              <SelectItem key={cluster} value={cluster}>
                {cluster}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  );
};