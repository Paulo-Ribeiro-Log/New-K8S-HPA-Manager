import { LucideIcon } from "lucide-react";
import { SidebarItem } from "./SidebarItem";

interface SidebarSectionItem {
  id: string;
  label: string;
  icon: LucideIcon;
  badge?: number;
}

interface SidebarSectionProps {
  title: string;
  items: SidebarSectionItem[];
  activeTab: string;
  isCollapsed: boolean;
  onTabChange: (tabId: string) => void;
}

export const SidebarSection = ({
  title,
  items,
  activeTab,
  isCollapsed,
  onTabChange,
}: SidebarSectionProps) => {
  return (
    <div className="mb-6">
      {/* Section Header */}
      {!isCollapsed && (
        <div className="px-3 py-2 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
          {title}
        </div>
      )}

      {/* Section Items */}
      <div className="space-y-1 px-2">
        {items.map((item) => (
          <SidebarItem
            key={item.id}
            id={item.id}
            label={item.label}
            icon={item.icon}
            isActive={activeTab === item.id}
            isCollapsed={isCollapsed}
            badge={item.badge}
            onClick={onTabChange}
          />
        ))}
      </div>
    </div>
  );
};
