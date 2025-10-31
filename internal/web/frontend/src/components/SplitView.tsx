import { ReactNode } from "react";
import { Card } from "@/components/ui/card";

interface SplitViewProps {
  leftPanel: {
    title: string;
    titleAction?: ReactNode;
    content: ReactNode;
  };
  rightPanel: {
    title: string;
    titleAction?: ReactNode;
    content: ReactNode;
  };
}

export const SplitView = ({ leftPanel, rightPanel }: SplitViewProps) => {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-5 gap-4 p-4 h-full">
      <Card className="lg:col-span-2 p-4 bg-gradient-card border-border/50 flex flex-col min-h-0">
        <div className="flex items-center justify-between mb-3 pb-2 border-b-2 border-primary flex-shrink-0">
          <h3 className="text-base font-semibold text-primary">
            {leftPanel.title}
          </h3>
          {leftPanel.titleAction && (
            <div className="flex items-center gap-2">
              {leftPanel.titleAction}
            </div>
          )}
        </div>
        <div className="flex-1 overflow-auto min-h-0">
          {leftPanel.content}
        </div>
      </Card>

      <Card className="lg:col-span-3 p-4 bg-gradient-card border-border/50 flex flex-col min-h-0">
        <div className="flex items-center justify-between mb-3 pb-2 border-b-2 border-primary flex-shrink-0">
          <h3 className="text-base font-semibold text-primary">
            {rightPanel.title}
          </h3>
          {rightPanel.titleAction && (
            <div className="flex items-center gap-2">
              {rightPanel.titleAction}
            </div>
          )}
        </div>
        <div className="flex-1 overflow-auto min-h-0">
          {rightPanel.content}
        </div>
      </Card>
    </div>
  );
};
