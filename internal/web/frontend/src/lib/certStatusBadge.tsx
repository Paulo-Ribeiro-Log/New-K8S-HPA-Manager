import { Badge } from "@/components/ui/badge";

// Selo de status de certificado (válido/expirando/expirado), compartilhado pelas telas de certificados.
export function getStatusBadge(status: string) {
  switch (status) {
    case "valid":
      return <Badge className="bg-green-500/20 text-green-400 border-green-500/30">Valido</Badge>;
    case "expiring":
      return <Badge className="bg-yellow-500/20 text-yellow-400 border-yellow-500/30">Expirando</Badge>;
    case "expired":
      return <Badge className="bg-red-500/20 text-red-400 border-red-500/30">Expirado</Badge>;
    default:
      return <Badge variant="secondary">{status}</Badge>;
  }
}
