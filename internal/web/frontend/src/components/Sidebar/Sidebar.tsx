import { useState, useEffect } from "react";
import { SidebarSection } from "./SidebarSection";
import { SidebarToggle } from "./SidebarToggle";
import {
  FileCode,
  Key,
  Package,
  Box,
  Layers,
  Clock,
  Activity,
} from "lucide-react";

interface SidebarProps {
  activeTab: string;
  onTabChange: (tabId: string) => void;
}

export const Sidebar = ({ activeTab, onTabChange }: SidebarProps) => {
  // Carregar estado de collapsed do localStorage
  const [isCollapsed, setIsCollapsed] = useState(() => {
    const stored = localStorage.getItem("sidebar_collapsed");
    return stored === "true";
  });

  // Persistir estado no localStorage
  useEffect(() => {
    localStorage.setItem("sidebar_collapsed", String(isCollapsed));
  }, [isCollapsed]);

  const sections = [
    {
      title: "WORKLOAD",
      items: [
        { id: "configmaps", label: "ConfigMaps", icon: FileCode },
        { id: "secrets", label: "Secrets", icon: Key },
        { id: "deployments", label: "Deployments", icon: Package },
        { id: "containers", label: "Containers", icon: Box },
        { id: "pods", label: "Pods", icon: Layers },
        { id: "cronjobs", label: "CronJobs", icon: Clock },
        { id: "prometheus", label: "Prometheus", icon: Activity },
      ],
    },
  ];

  return (
    <aside
      className={`
        bg-card border-r border-border
        transition-all duration-300 ease-in-out
        flex flex-col flex-shrink-0
        ${isCollapsed ? "w-16" : "w-60"}
      `}
    >
      {/* Conteúdo scrollável */}
      <div className="flex-1 overflow-y-auto py-4">
        {sections.map((section) => (
          <SidebarSection
            key={section.title}
            title={section.title}
            items={section.items}
            activeTab={activeTab}
            onTabChange={onTabChange}
            isCollapsed={isCollapsed}
          />
        ))}
      </div>

      {/* Botão de toggle fixo no bottom */}
      <div className="border-t border-border p-2">
        <SidebarToggle
          isCollapsed={isCollapsed}
          onToggle={() => setIsCollapsed(!isCollapsed)}
        />
      </div>
    </aside>
  );
};
