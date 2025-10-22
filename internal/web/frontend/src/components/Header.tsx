import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { LogOut, CheckCircle, Zap, Save, FolderOpen } from "lucide-react";
import { ModeToggle } from "@/components/mode-toggle";

interface HeaderProps {
  selectedCluster: string;
  onClusterChange: (value: string) => void;
  clusters: string[];
  modifiedCount: number;
  onApplyAll: () => void;
  onApplySequential?: () => void;
  onSaveSession?: () => void;
  onLoadSession?: () => void;
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
  userInfo,
  onLogout,
}: HeaderProps) => {
  return (
    <header className="h-16 bg-gradient-primary flex items-center justify-between px-6 shadow-lg flex-shrink-0">
      <div className="flex items-center gap-6">
        <h1 className="text-xl font-semibold text-white tracking-tight">
          k8s-hpa-manager
        </h1>
        <Select value={selectedCluster} onValueChange={onClusterChange}>
          <SelectTrigger className="w-[280px] bg-white/20 border-white/30 text-white hover:bg-white/25 transition-colors">
            <SelectValue placeholder="Select a cluster..." />
          </SelectTrigger>
          <SelectContent>
            {clusters.map((cluster) => (
              <SelectItem key={cluster} value={cluster}>
                {cluster}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
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
