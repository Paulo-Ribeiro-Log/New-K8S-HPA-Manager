import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { X, Plus, ChevronLeft, ChevronRight } from 'lucide-react';
import { useTabManager } from '@/contexts/TabContext';
import { TabState } from '@/types/tabs';
import { cn } from '@/lib/utils';

// Componente individual da aba
const TabItem = ({ 
  tab, 
  index, 
  isActive, 
  onClose, 
  onSwitch 
}: { 
  tab: TabState; 
  index: number; 
  isActive: boolean;
  onClose: (index: number) => void;
  onSwitch: (index: number) => void;
}) => {
  const getStatusIcon = (status?: string) => {
    switch (status) {
      case 'connected': return '🟢';
      case 'error': return '🔴';
      case 'timeout': return '🟡';
      default: return '⚪';
    }
  };

  const formatTabName = (name: string) => {
    if (name.length > 12) {
      return name.substring(0, 12) + '...';
    }
    return name;
  };

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <div
            className={cn(
              "flex items-center gap-2 px-3 py-2 text-sm border-b-2 cursor-pointer transition-all",
              "hover:bg-muted/50 min-w-0 max-w-48",
              isActive 
                ? "border-primary bg-background text-foreground" 
                : "border-transparent text-muted-foreground hover:text-foreground"
            )}
            onClick={() => onSwitch(index)}
          >
            {/* Status do cluster */}
            <span className="text-xs flex-shrink-0">
              {getStatusIcon(tab.cluster?.status)}
            </span>
            
            {/* Nome da aba */}
            <span className="truncate flex-1 min-w-0">
              {formatTabName(tab.name)}
            </span>
            
            {/* Indicador de mudanças */}
            {tab.modified && tab.pendingChanges.total > 0 && (
              <Badge variant="secondary" className="h-5 px-1 text-xs flex-shrink-0">
                {tab.pendingChanges.total}
              </Badge>
            )}
            
            {/* Botão fechar */}
            <Button
              variant="ghost"
              size="sm"
              className="h-4 w-4 p-0 hover:bg-destructive/20 flex-shrink-0"
              onClick={(e) => {
                e.stopPropagation();
                onClose(index);
              }}
            >
              <X className="h-3 w-3" />
            </Button>
            
            {/* Indicador de atalho de teclado */}
            {index < 9 && (
              <span className="text-xs text-muted-foreground/60 flex-shrink-0">
                {index + 1}
              </span>
            )}
            {index === 9 && (
              <span className="text-xs text-muted-foreground/60 flex-shrink-0">
                0
              </span>
            )}
          </div>
        </TooltipTrigger>
        <TooltipContent>
          <div className="space-y-1">
            <div className="font-medium">{tab.name}</div>
            <div className="text-xs text-muted-foreground">
              Cluster: {tab.clusterContext}
            </div>
            {tab.cluster?.status && (
              <div className="text-xs text-muted-foreground">
                Status: {tab.cluster.status}
              </div>
            )}
            {tab.modified && (
              <div className="text-xs text-muted-foreground">
                Mudanças pendentes: {tab.pendingChanges.total}
              </div>
            )}
            <div className="text-xs text-muted-foreground border-t pt-1">
              Atalho: Alt+{index < 9 ? index + 1 : 0}
            </div>
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
};

// Componente principal da barra de abas
export const TabBar = () => {
  const { state, addTab, closeTab, switchTab, canAddTab } = useTabManager();
  const [scrollOffset, setScrollOffset] = useState(0);
  
  const tabs = state.tabManager.tabs;
  const activeTabIndex = state.tabManager.activeTabIndex;
  
  // Controle de scroll horizontal para muitas abas
  const maxVisibleTabs = 8;
  const canScrollLeft = scrollOffset > 0;
  const canScrollRight = scrollOffset + maxVisibleTabs < tabs.length;
  
  const visibleTabs = tabs.slice(scrollOffset, scrollOffset + maxVisibleTabs);
  
  const scrollLeft = () => {
    if (canScrollLeft) {
      setScrollOffset(Math.max(0, scrollOffset - 1));
    }
  };
  
  const scrollRight = () => {
    if (canScrollRight) {
      setScrollOffset(Math.min(tabs.length - maxVisibleTabs, scrollOffset + 1));
    }
  };
  
  const handleAddTab = () => {
    if (canAddTab()) {
      // TODO: Integrar com seletor de cluster
      const clusterContext = `cluster-${tabs.length + 1}`;
      addTab(`Cluster ${tabs.length + 1}`, clusterContext);
    }
  };
  
  return (
    <div className="flex items-center bg-muted/30 border-b">
      {/* Scroll Left */}
      {canScrollLeft && (
        <Button
          variant="ghost"
          size="sm"
          className="h-8 w-8 p-0 flex-shrink-0"
          onClick={scrollLeft}
        >
          <ChevronLeft className="h-4 w-4" />
        </Button>
      )}
      
      {/* Abas visíveis */}
      <div className="flex items-center flex-1 min-w-0">
        {visibleTabs.map((tab, visibleIndex) => {
          const actualIndex = scrollOffset + visibleIndex;
          return (
            <TabItem
              key={tab.id}
              tab={tab}
              index={actualIndex}
              isActive={actualIndex === activeTabIndex}
              onClose={closeTab}
              onSwitch={switchTab}
            />
          );
        })}
      </div>
      
      {/* Scroll Right */}
      {canScrollRight && (
        <Button
          variant="ghost"
          size="sm"
          className="h-8 w-8 p-0 flex-shrink-0"
          onClick={scrollRight}
        >
          <ChevronRight className="h-4 w-4" />
        </Button>
      )}
      
      {/* Botão adicionar aba */}
      <div className="flex-shrink-0 pl-2 pr-4">
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="h-8 w-8 p-0"
                onClick={handleAddTab}
                disabled={!canAddTab()}
              >
                <Plus className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              <div className="space-y-1">
                <div>Nova aba</div>
                <div className="text-xs text-muted-foreground">
                  {canAddTab() 
                    ? `Atalho: Alt+T (${tabs.length}/${state.tabManager.maxTabs})`
                    : `Limite atingido (${state.tabManager.maxTabs}/${state.tabManager.maxTabs})`
                  }
                </div>
              </div>
            </TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </div>
    </div>
  );
};

// Componente de estatísticas das abas (opcional)
export const TabStats = () => {
  const { state, getTabsWithChanges } = useTabManager();
  const tabsWithChanges = getTabsWithChanges();
  
  if (tabsWithChanges.length === 0) {
    return null;
  }
  
  const totalChanges = tabsWithChanges.reduce(
    (sum, tab) => sum + tab.pendingChanges.total, 
    0
  );
  
  return (
    <div className="flex items-center gap-2 px-4 py-2 bg-amber-50 dark:bg-amber-950/20 border-b text-sm">
      <span className="text-amber-700 dark:text-amber-300">
        ⚠️ {tabsWithChanges.length} aba{tabsWithChanges.length > 1 ? 's' : ''} com mudanças pendentes
        ({totalChanges} alteraç{totalChanges > 1 ? 'ões' : 'ão'} no total)
      </span>
    </div>
  );
};

export default TabBar;