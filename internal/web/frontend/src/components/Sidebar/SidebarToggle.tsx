import { ChevronLeft, ChevronRight } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

interface SidebarToggleProps {
  isCollapsed: boolean;
  onToggle: () => void;
}

export const SidebarToggle = ({ isCollapsed, onToggle }: SidebarToggleProps) => {
  const button = (
    <button
      onClick={onToggle}
      className="w-full p-2 hover:bg-muted rounded-lg transition-colors flex items-center justify-center"
      aria-label={isCollapsed ? "Expandir sidebar" : "Recolher sidebar"}
    >
      {isCollapsed ? (
        <ChevronRight className="w-4 h-4 text-muted-foreground" />
      ) : (
        <ChevronLeft className="w-4 h-4 text-muted-foreground" />
      )}
    </button>
  );

  if (isCollapsed) {
    return (
      <Tooltip delayDuration={0}>
        <TooltipTrigger asChild>{button}</TooltipTrigger>
        <TooltipContent side="right">Expandir sidebar</TooltipContent>
      </Tooltip>
    );
  }

  return button;
};
