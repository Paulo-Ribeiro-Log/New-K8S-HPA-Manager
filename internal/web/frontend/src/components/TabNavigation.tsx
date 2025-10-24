import { LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";

interface Tab {
  id: string;
  label: string;
  icon: LucideIcon;
  badge?: number;
}

interface TabNavigationProps {
  tabs: Tab[];
  activeTab: string;
  onTabChange: (tabId: string) => void;
}

export const TabNavigation = ({ tabs, activeTab, onTabChange }: TabNavigationProps) => {
  return (
    <div className="h-12 bg-card border-b border-border flex items-center px-4 gap-1 flex-shrink-0">
      {tabs.map((tab) => {
        const Icon = tab.icon;
        const isActive = activeTab === tab.id;

        return (
          <button
            key={tab.id}
            onClick={() => onTabChange(tab.id)}
            className={`
              flex items-center gap-2 px-3 py-1.5 rounded-lg font-medium text-sm
              transition-all duration-200 relative
              ${
                isActive
                  ? "bg-gradient-primary text-white shadow-md"
                  : "text-muted-foreground hover:bg-muted hover:text-foreground"
              }
            `}
          >
            <Icon className="w-4 h-4" />
            {tab.label}
            {tab.badge !== undefined && tab.badge > 0 && (
              <Badge
                variant={isActive ? "secondary" : "default"}
                className="ml-1 h-5 min-w-5 px-1.5 text-xs"
              >
                {tab.badge}
              </Badge>
            )}
          </button>
        );
      })}
    </div>
  );
};
