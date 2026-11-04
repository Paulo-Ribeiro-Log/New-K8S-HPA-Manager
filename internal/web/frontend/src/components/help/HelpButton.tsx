import { HelpCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useHelp } from "./helpContext";

/** Botão "?" do cabeçalho: abre a ajuda da aba ativa (ou a fecha). */
export function HelpButton() {
  const { open, toggleHelp } = useHelp();
  return (
    <Button
      variant="ghost"
      size="icon"
      className="h-9 w-9"
      onClick={toggleHelp}
      title="Ajuda desta aba"
      aria-label="Ajuda desta aba"
      aria-pressed={open}
    >
      <HelpCircle className="h-5 w-5" />
    </Button>
  );
}
