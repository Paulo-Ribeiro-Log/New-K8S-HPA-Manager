import { LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

interface SidebarItemProps {
  id: string;
  label: string;
  icon: LucideIcon;
  isActive: boolean;
  isCollapsed: boolean;
  badge?: number;
  onClick: (id: string) => void;
}

export const SidebarItem = ({
  id,
  label,
  icon: Icon,
  isActive,
  isCollapsed,
  badge,
  onClick,
}: SidebarItemProps) => {
  const button = (
    <button
      onClick={() => onClick(id)}
      className={`
        flex items-center gap-3 w-full px-3 py-2 rounded-lg
        text-sm font-medium transition-all duration-200
        ${
          isActive
            ? "bg-gradient-primary text-white shadow-md"
            : "text-foreground hover:bg-muted"
        }
        ${isCollapsed ? "justify-center" : ""}
      `}
    >
      <Icon className="w-4 h-4 flex-shrink-0" />
      {!isCollapsed && (
        <>
          <span className="truncate flex-1 text-left">{label}</span>
          {badge !== undefined && badge > 0 && (
            <Badge
              variant={isActive ? "secondary" : "default"}
              className="ml-auto h-5 min-w-5 px-1.5 text-xs"
            >
              {badge}
            </Badge>
          )}
        </>
      )}
    </button>
  );

  // Se colapsada, mostrar tooltip com o label
  if (isCollapsed) {
    return (
      <Tooltip delayDuration={0}>
        <TooltipTrigger asChild>{button}</TooltipTrigger>
        <TooltipContent side="right" className="flex items-center gap-2">
          {label}
          {badge !== undefined && badge > 0 && (
            <Badge variant="secondary" className="h-5 min-w-5 px-1.5 text-xs">
              {badge}
            </Badge>
          )}
        </TooltipContent>
      </Tooltip>
    );
  }

  return button;
};
