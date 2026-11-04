import { useCallback, useMemo, useRef, useState, type ReactNode } from "react";
import { HelpContext, type HelpContextValue } from "./helpContext";
import { HelpPanel } from "./HelpPanel";

export function HelpProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<"page" | "index">("page");
  const [activeTabId, setActiveTab] = useState<string | null>(null);
  const [pageOverride, setPageOverride] = useState<string | null>(null);
  const [anchor, setAnchor] = useState<string | null>(null);

  const openHelp = useCallback((id?: string, section?: string) => {
    setOpen(true);
    setView("page");
    setPageOverride(id ?? null);
    setAnchor(section ?? null);
  }, []);

  const closeHelp = useCallback(() => setOpen(false), []);
  const toggleHelp = useCallback(() => {
    if (open) {
      setOpen(false);
    } else {
      openHelp();
    }
  }, [open, openHelp]);
  const showIndex = useCallback(() => setView("index"), []);

  // Trocar de aba volta a acompanhar a aba (descarta a página escolhida à mão).
  const lastTab = useRef<string | null>(null);
  const setActiveTabId = useCallback((id: string) => {
    if (lastTab.current === id) return;
    lastTab.current = id;
    setActiveTab(id);
    setPageOverride(null);
    setAnchor(null);
    setView("page");
  }, []);

  const value = useMemo<HelpContextValue>(
    () => ({ open, view, activeTabId, pageOverride, anchor, openHelp, closeHelp, toggleHelp, showIndex, setActiveTabId }),
    [open, view, activeTabId, pageOverride, anchor, openHelp, closeHelp, toggleHelp, showIndex, setActiveTabId],
  );

  return (
    <HelpContext.Provider value={value}>
      {children}
      <HelpPanel />
    </HelpContext.Provider>
  );
}
