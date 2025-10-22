import { LucideIcon, Plus, X, Layers } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useClusterTabs } from "@/hooks/useClusterTabs";
import { useClusters } from "@/hooks/useAPI";

interface ClusterTab {
  id: string;
  label: string;
  icon: LucideIcon;
  cluster: string;
}

interface ClusterTabsProps {
  tabs?: ClusterTab[];
  activeTab?: string;
  onTabChange?: (tabId: string) => void;
  onAddTab?: () => void;
  onRemoveTab?: (tabId: string) => void;
  addButtonLabel?: string;
  showRemoveButton?: boolean;
}

// Componente integrado que usa o hook automaticamente
export const ClusterTabs = ({ 
  tabs: externalTabs, 
  activeTab: externalActiveTab, 
  onTabChange: externalOnTabChange, 
  onAddTab: externalOnAddTab, 
  onRemoveTab: externalOnRemoveTab,
  addButtonLabel = "Novo Cluster",
  showRemoveButton = true 
}: ClusterTabsProps = {}) => {
  
  // Se não receber props, usar o hook interno
  const { clusters } = useClusters();
  const {
    clusterTabs,
    activeClusterTab,
    handleTabChange,
    addClusterTab,
    removeClusterTab,
  } = useClusterTabs(clusters.map(c => c.context));

  // Usar props externas se fornecidas, senão usar hook interno
  const tabs = externalTabs || clusterTabs.map(tab => ({
    id: tab.id,
    label: tab.label,
    icon: Layers as LucideIcon,
    cluster: tab.cluster
  }));

  const activeTab = externalActiveTab || activeClusterTab;
  const onTabChange = externalOnTabChange || handleTabChange;
  const onAddTab = externalOnAddTab || (() => {
    // Usa a função do hook que já tem a lógica implementada
    addClusterTab();
  });
  const onRemoveTab = externalOnRemoveTab || removeClusterTab;

  return (
    <div className="flex items-center px-6 py-2 gap-2 overflow-x-auto">
      {/* Abas de cluster */}
      <div className="flex items-center gap-1">
        {tabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id;
          
          return (
            <div key={tab.id} className="relative group/tab">
              <button
                onClick={() => onTabChange(tab.id)}
                className={`
                  group relative flex items-center gap-2 px-3 py-1.5 rounded-md font-medium text-sm
                  transition-all duration-200 whitespace-nowrap
                  ${
                    isActive
                      ? "bg-primary text-primary-foreground shadow-sm"
                      : "text-muted-foreground hover:text-foreground hover:bg-accent/50"
                  }
                `}
              >
                <Icon className={`w-4 h-4 transition-transform duration-200 ${isActive ? "scale-110" : "group-hover:scale-105"}`} />
                <span className="relative">
                  {tab.label}
                  {isActive && (
                    <span className="absolute -bottom-0.5 left-0 right-0 h-0.5 bg-primary-foreground rounded-full" />
                  )}
                </span>
              </button>
              {showRemoveButton && onRemoveTab && tabs.length > 1 && (
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onRemoveTab(tab.id);
                  }}
                  className="absolute -top-1 -right-1 opacity-0 group-hover/tab:opacity-100 transition-opacity duration-200 bg-destructive text-destructive-foreground rounded-full p-0.5 hover:scale-110 z-10"
                >
                  <X className="w-3 h-3" />
                </button>
              )}
            </div>
          );
        })}
      </div>
      
      {/* Bot\u00e3o para adicionar nova aba */}
      {onAddTab && (
        <Button
          onClick={onAddTab}
          variant="outline"
          size="sm"
          className="ml-2 gap-1.5 hover:bg-primary hover:text-primary-foreground transition-colors shrink-0"
        >
          <Plus className="w-3.5 h-3.5" />
          {addButtonLabel}
        </Button>
      )}
    </div>
  );
};