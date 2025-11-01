import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { LogOut, CheckCircle, Zap, Save, FolderOpen, FileText, ChevronsUpDown, Check } from "lucide-react";
import { ModeToggle } from "@/components/mode-toggle";
import { cn } from "@/lib/utils";

interface HeaderProps {
  selectedCluster: string;
  onClusterChange: (value: string) => void;
  clusters: string[];
  modifiedCount: number;
  onApplyAll: () => void;
  onApplySequential?: () => void;
  onSaveSession?: () => void;
  onLoadSession?: () => void;
  onViewLogs?: () => void;
  userInfo: string;
  onLogout: () => void;
}

export const Header = ({
  selectedCluster,
  onClusterChange,
  clusters,
  modifiedCount,
  onApplyAll,
  onApplySequential,
  onSaveSession,
  onLoadSession,
  onViewLogs,
  userInfo,
  onLogout,
}: HeaderProps) => {
  const [open, setOpen] = useState(false);
  return (
    <header className="h-16 bg-gradient-primary flex items-center justify-between px-6 shadow-lg flex-shrink-0">
      <div className="flex items-center gap-4">
        <h1 className="text-xl font-semibold text-white tracking-tight">
          k8s-hpa-manager
        </h1>

        {/* Combobox de cluster com busca integrada */}
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button
              variant="outline"
              role="combobox"
              aria-expanded={open}
              className="w-[400px] justify-between bg-white/20 border-white/30 text-white hover:bg-white/25 hover:text-white"
            >
              {selectedCluster
                ? clusters.find((cluster) => cluster === selectedCluster)
                : "Selecione ou busque um cluster..."}
              <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
            </Button>
          </PopoverTrigger>
          <PopoverContent className="w-[400px] p-0">
            <Command>
              <CommandInput placeholder="Buscar cluster..." />
              <CommandList>
                <CommandEmpty>Nenhum cluster encontrado.</CommandEmpty>
                <CommandGroup>
                  {clusters.map((cluster) => (
                    <CommandItem
                      key={cluster}
                      value={cluster}
                      onSelect={(currentValue) => {
                        onClusterChange(currentValue === selectedCluster ? "" : currentValue);
                        setOpen(false);
                      }}
                    >
                      <Check
                        className={cn(
                          "mr-2 h-4 w-4",
                          selectedCluster === cluster ? "opacity-100" : "opacity-0"
                        )}
                      />
                      {cluster}
                    </CommandItem>
                  ))}
                </CommandGroup>
              </CommandList>
            </Command>
          </PopoverContent>
        </Popover>
      </div>

      <div className="flex items-center gap-3">
        {/* Session Management Buttons */}
        {onLoadSession && (
          <Button
            variant="secondary"
            size="sm"
            className="bg-white/20 hover:bg-white/30 text-white border-white/30"
            onClick={onLoadSession}
            title="Load Session"
          >
            <FolderOpen className="w-4 h-4 mr-2" />
            Load Session
          </Button>
        )}
        
        {onSaveSession && (
          <Button
            variant="secondary"
            size="sm"
            className="bg-white/20 hover:bg-white/30 text-white border-white/30"
            onClick={onSaveSession}
            title="Save Session"
          >
            <Save className="w-4 h-4 mr-2" />
            Save Session
          </Button>
        )}
        
        {onApplySequential && (
          <Button
            variant="secondary"
            className="bg-warning hover:bg-warning/90 text-white border-0"
            onClick={onApplySequential}
          >
            <Zap className="w-4 h-4 mr-2" />
            Apply Sequential
          </Button>
        )}
        
        {modifiedCount > 0 && (
          <Button
            variant="secondary"
            className="bg-success hover:bg-success/90 text-white border-0"
            onClick={onApplyAll}
          >
            <CheckCircle className="w-4 h-4 mr-2" />
            Apply All
            <span className="ml-2 px-2 py-0.5 bg-white/20 rounded-full text-xs">
              {modifiedCount}
            </span>
          </Button>
        )}
        
        {onViewLogs && (
          <Button
            variant="secondary"
            size="sm"
            className="bg-white/20 hover:bg-white/30 text-white border-white/30"
            onClick={onViewLogs}
            title="View System Logs"
          >
            <FileText className="w-4 h-4" />
          </Button>
        )}

        <span className="text-white/90 text-sm">{userInfo}</span>

        <ModeToggle />

        <Button
          variant="secondary"
          size="sm"
          className="bg-white/20 hover:bg-white/30 text-white border-white/30"
          onClick={onLogout}
        >
          <LogOut className="w-4 h-4 mr-2" />
          Logout
        </Button>
      </div>
    </header>
  );
};
