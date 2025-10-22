import { ReactNode } from "react";
import { Card } from "@/components/ui/card";

interface SplitViewProps {
  leftPanel: {
    title: string;
    content: ReactNode;
  };
  rightPanel: {
    title: string;
    content: ReactNode;
  };
}

export const SplitView = ({ leftPanel, rightPanel }: SplitViewProps) => {
  return (
    <div className="grid grid-cols-1 lg:grid-cols-5 gap-4 p-4 h-full">
      <Card className="lg:col-span-2 p-4 bg-gradient-card border-border/50 flex flex-col min-h-0">
        <h3 className="text-base font-semibold text-primary mb-3 pb-2 border-b-2 border-primary flex-shrink-0">
          {leftPanel.title}
        </h3>
        <div className="flex-1 overflow-auto min-h-0">
          {leftPanel.content}
        </div>
      </Card>

      <Card className="lg:col-span-3 p-4 bg-gradient-card border-border/50 flex flex-col min-h-0">
        <h3 className="text-base font-semibold text-primary mb-3 pb-2 border-b-2 border-primary flex-shrink-0">
          {rightPanel.title}
        </h3>
        <div className="flex-1 overflow-auto min-h-0">
          {rightPanel.content}
        </div>
      </Card>
    </div>
  );
};
