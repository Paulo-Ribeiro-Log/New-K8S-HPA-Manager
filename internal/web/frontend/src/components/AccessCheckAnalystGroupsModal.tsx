import { useMemo, useState } from "react";
import { Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { CopyValue } from "@/components/CopyValue";
import { copyText } from "@/lib/clipboard";
import { globMatcher } from "@/lib/groupFilter";

interface Group {
  id: string;
  displayName: string;
}

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  email: string;
  groups: Group[];
}

// Grupos do Entra ID do analista usados na verificação (todos, sem filtro de prefixo).
export function AccessCheckAnalystGroupsModal({ open, onOpenChange, email, groups }: Props) {
  const [search, setSearch] = useState("");

  const filtered = useMemo(() => {
    const match = globMatcher(search);
    return [...groups].filter((g) => match(g.displayName, g.id)).sort((a, b) => a.displayName.localeCompare(b.displayName));
  }, [groups, search]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>Grupos AAD usados na verificação</DialogTitle>
          <DialogDescription>
            Todos os {groups.length} grupos de <span className="font-medium text-foreground">{email}</span>, sem filtro de prefixo.
          </DialogDescription>
        </DialogHeader>

        <div className="flex items-center gap-2">
          <Input placeholder="Nome (vv_cloud_*, az_c_*) ou Object ID" value={search} onChange={(e) => setSearch(e.target.value)} />
          <Button
            variant="outline"
            size="sm"
            disabled={filtered.length === 0}
            onClick={() => copyText(filtered.map((g) => `${g.displayName}\t${g.id}`).join("\n"), `${filtered.length} grupo(s)`)}
          >
            <Copy className="w-3.5 h-3.5 mr-1.5" />
            Copiar lista
          </Button>
        </div>
        <div className="text-xs text-muted-foreground">
          {filtered.length} de {groups.length} grupo(s)
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto">
          {filtered.map((g) => (
            <div key={g.id} className="border-b border-border py-1.5">
              <div>
                <CopyValue value={g.displayName} label="nome do grupo" className="text-sm font-medium break-all" />
              </div>
              <div>
                <CopyValue value={g.id} label="Object ID" className="text-xs font-mono text-muted-foreground break-all" />
              </div>
            </div>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
