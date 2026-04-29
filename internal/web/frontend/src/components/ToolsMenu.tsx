import { LucideIcon, ChevronDown, Wrench } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  BarChart3,
  Network,
  Activity,
  FileCode,
  Brain,
  GitCompareArrows,
  Link2,
  Shield,
  SplitSquareHorizontal,
  Terminal,
  AlertTriangle,
  CircleDollarSign,
  MessageSquarePlus,
} from "lucide-react";

interface ToolsMenuProps {
  activeTab: string;
  onTabChange: (tabId: string) => void;
}

interface ToolsTab {
  id: string;
  label: string;
  icon: LucideIcon;
}

const toolsTabs: ToolsTab[] = [
  { id: "monitoring", label: "Monitoramento", icon: BarChart3 },
  { id: "servicemesh", label: "Service Mesh", icon: Network },
  { id: "healthcheck", label: "Health Checking", icon: Activity },
  { id: "nexus-values", label: "Nexus Values", icon: FileCode },
  { id: "ai-diagnostics", label: "AI Diagnostics", icon: Brain },
  { id: "github-releases", label: "GitHub Releases", icon: GitCompareArrows },
  { id: "dependencies", label: "Dependencies", icon: Link2 },
  { id: "certificates", label: "Certificados TLS", icon: Shield },
  { id: "resource-compare", label: "Edição Lado a Lado", icon: SplitSquareHorizontal },
  { id: "command-runner", label: "Command Runner", icon: Terminal },
  { id: "dynatrace", label: "Dynatrace", icon: AlertTriangle },
  { id: "finops", label: "FinOps", icon: CircleDollarSign },
  { id: "teams-broadcast", label: "Teams Broadcast", icon: MessageSquarePlus },
];

export const ToolsMenu = ({ activeTab, onTabChange }: ToolsMenuProps) => {
  const isToolsActive = toolsTabs.some((tab) => tab.id === activeTab);
  const activeToolsTab = toolsTabs.find((tab) => tab.id === activeTab);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          className={`
            flex items-center gap-2 px-3 py-1.5 rounded-lg font-medium text-sm
            transition-all duration-200 relative
            ${
              isToolsActive
                ? "bg-gradient-primary text-white shadow-md"
                : "text-muted-foreground hover:bg-muted hover:text-foreground"
            }
          `}
        >
          <Wrench className="w-4 h-4" />
          Tools{activeToolsTab ? `: ${activeToolsTab.label}` : ""}
          <ChevronDown className="w-3 h-3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        {toolsTabs.map((tab) => {
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
