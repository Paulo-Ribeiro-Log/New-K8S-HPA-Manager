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
  PackageOpen,
  Box,
  Layers,
  Clock,
  Activity,
  Network,
  Calendar,
  Server,
  Database,
  TrendingUp,
  Globe,
  Route,
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
  { id: "ingresses", label: "Ingresses", icon: Network },
  { id: "gateways", label: "Gateways", icon: Route },
  { id: "secrets", label: "Secrets", icon: Key },
  { id: "deployments", label: "Deployments", icon: Package },
  { id: "daemonsets", label: "DaemonSets", icon: Server },
  { id: "statefulsets", label: "StatefulSets", icon: Database },
  { id: "vpas", label: "VPAs", icon: TrendingUp },
  { id: "services", label: "Services", icon: Globe },
  { id: "containers", label: "Containers", icon: Box },
  { id: "pods", label: "Pods", icon: Layers },
  { id: "events", label: "Events", icon: Calendar },
  { id: "cronjobs", label: "CronJobs", icon: Clock },
  { id: "namespaces", label: "Namespaces", icon: Database },
  { id: "helm", label: "Helm", icon: PackageOpen },
  { id: "prometheus", label: "Prometheus", icon: Activity },
];

export const WorkloadMenu = ({ activeTab, onTabChange }: WorkloadMenuProps) => {
  // Verificar se alguma aba workload está ativa
  const isWorkloadActive = workloadTabs.some((tab) => tab.id === activeTab);
  const activeWorkloadTab = workloadTabs.find((tab) => tab.id === activeTab);

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
          Workload{activeWorkloadTab ? `: ${activeWorkloadTab.label}` : ""}
          <ChevronDown className="w-3 h-3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56 p-0">
        <div className="overflow-y-auto py-1" style={{ maxHeight: "var(--radix-popper-available-height, 80vh)" }}>
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
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
