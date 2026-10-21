import { toast } from "sonner";

// Copia para a área de transferência e avisa com um toast ("Copiado: <what>").
export async function copyText(text: string, what: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(`Copiado: ${what}`);
  } catch {
    toast.error("Falha ao copiar — verifique permissões do navegador");
  }
}
