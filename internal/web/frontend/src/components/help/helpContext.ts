import { createContext, useContext, useEffect } from "react";

// Contexto da ajuda. Fica separado do provider (HelpProvider.tsx) para o painel poder usá-lo sem
// importação circular.

export interface HelpContextValue {
  open: boolean;
  /** "page": ajuda de uma aba; "index": lista de todas as páginas. */
  view: "page" | "index";
  /** Id da aba em uso (informado pelo Index). */
  activeTabId: string | null;
  /** Página escolhida à mão (índice ou openHelp); null = acompanha a aba ativa. */
  pageOverride: string | null;
  /** Seção (âncora) a rolar ao abrir a página. */
  anchor: string | null;
  /** Abre a ajuda: da aba ativa, ou da página `id` (e da seção `anchor`). */
  openHelp: (id?: string, anchor?: string) => void;
  closeHelp: () => void;
  toggleHelp: () => void;
  showIndex: () => void;
  setActiveTabId: (id: string) => void;
}

const noop = () => {};

export const HelpContext = createContext<HelpContextValue>({
  open: false,
  view: "page",
  activeTabId: null,
  pageOverride: null,
  anchor: null,
  openHelp: noop,
  closeHelp: noop,
  toggleHelp: noop,
  showIndex: noop,
  setActiveTabId: noop,
});

export function useHelp(): HelpContextValue {
  return useContext(HelpContext);
}

/** Informa à ajuda qual aba está ativa. Chamar uma vez, no componente que decide a aba (Index). */
export function useHelpTab(tabId: string): void {
  const { setActiveTabId } = useHelp();
  useEffect(() => {
    setActiveTabId(tabId);
  }, [tabId, setActiveTabId]);
}
