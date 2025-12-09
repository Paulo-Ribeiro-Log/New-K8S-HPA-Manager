import { LucideIcon, ChevronDown } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  FileCode,
  Key,
  Package,
  Box,
  Layers,
  Clock,
  Activity,
} from "lucide-react";

interface WorkloadMenuProps {
  activeTab: string;
  onTabChange: (tabId: string) => void;
}

interface WorkloadTab {
  id: string;
  label: string;
  icon: LucideIcon;
}

const workloadTabs: WorkloadTab[] = [
  { id: "configmaps", label: "ConfigMaps", icon: FileCode },
  { id: "secrets", label: "Secrets", icon: Key },
  { id: "deployments", label: "Deployments", icon: Package },
  { id: "containers", label: "Containers", icon: Box },
  { id: "pods", label: "Pods", icon: Layers },
  { id: "cronjobs", label: "CronJobs", icon: Clock },
  { id: "prometheus", label: "Prometheus", icon: Activity },
];

export const WorkloadMenu = ({ activeTab, onTabChange }: WorkloadMenuProps) => {
  // Verificar se alguma aba workload está ativa
  const isWorkloadActive = workloadTabs.some((tab) => tab.id === activeTab);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          className={`
            flex items-center gap-2 px-3 py-1.5 rounded-lg font-medium text-sm
            transition-all duration-200 relative
            ${
              isWorkloadActive
                ? "bg-gradient-primary text-white shadow-md"
                : "text-muted-foreground hover:bg-muted hover:text-foreground"
            }
          `}
        >
          <Layers className="w-4 h-4" />
          Workload
          <ChevronDown className="w-3 h-3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        {workloadTabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id;

          return (
            <DropdownMenuItem
              key={tab.id}
              onClick={() => onTabChange(tab.id)}
              className={`
                flex items-center gap-2 px-3 py-2 cursor-pointer
                ${isActive ? "bg-accent text-accent-foreground font-medium" : ""}
              `}
            >
              <Icon className="w-4 h-4" />
              {tab.label}
            </DropdownMenuItem>
          );
        })}
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
