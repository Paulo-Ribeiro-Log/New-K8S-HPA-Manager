import { useMemo, useState } from "react";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  ChevronsUpDown,
  Check,
  Loader2,
  ScanEye,
  Play,
  Info,
  KeyRound,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { useClusters } from "@/hooks/useAPI";
import { ProtectedAction } from "@/components/rbac";
import { cn } from "@/lib/utils";
import { AKVDiscoveryModal } from "@/components/AKVDiscoveryModal";
import type { AKVExternalSecretSummary } from "@/lib/api/types";

function SimpleSearchableSelect({
  value,
  onChange,
  options,
  placeholder,
  searchPlaceholder,
  emptyMessage,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  options: string[];
  placeholder: string;
  searchPlaceholder: string;
  emptyMessage: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="w-full justify-between font-normal"
        >
          <span className="truncate">{value || placeholder}</span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command>
          <CommandInput placeholder={searchPlaceholder} />
          <CommandList>
            <CommandEmpty>{emptyMessage}</CommandEmpty>
            <CommandGroup>
              {options.map((opt) => (
                <CommandItem
                  key={opt}
                  value={opt}
                  onSelect={() => {
                    onChange(opt === value ? "" : opt);
                    setOpen(false);
                  }}
                >
                  <Check className={cn("mr-2 h-4 w-4", value === opt ? "opacity-100" : "opacity-0")} />
                  {opt}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

// MatchPatternCombobox — combobox de "Critério de busca (padrão de nome)": sugere "*" (Tudo — vira
// o regexp ".*", sem restrição) e os regexps de `find.name.regexp` já usados por ExternalSecrets
// existentes neste namespace (o mesmo texto que já aparecia como "critério:" nos cards acima), mas
// sempre aceita digitar um regexp arbitrário — a "*" existe porque a secret desejada nem sempre foi
// criada no namespace/local esperado, então o critério ideal pode não estar entre as sugestões.
function MatchPatternCombobox({
  value,
  onChange,
  suggestions,
  disabled,
}: {
  value: string;
  onChange: (value: string) => void;
  suggestions: string[];
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");

  // "*" é só um atalho de exibição pro regexp "sem restrição" — a comparação/armazenamento reais
  // sempre usam ".*" (mesmo valor que isUnscopedPattern já reconhece).
  const toRegexp = (opt: string) => (opt === "*" ? ".*" : opt);
  const toDisplay = (val: string) => (val.trim() === ".*" ? "*" : val);

  const filteredSuggestions = suggestions.filter(
    (s) => !search.trim() || s.toLowerCase().includes(search.trim().toLowerCase())
  );
  const hasExactMatch = suggestions.some((s) => toRegexp(s) === search.trim());

  return (
    <Popover open={open} onOpenChange={(o) => { setOpen(o); if (o) setSearch(""); }}>
      <PopoverTrigger asChild>
        <Button
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className="w-full justify-between font-normal font-mono h-9 text-xs"
        >
          <span className="truncate">{value ? toDisplay(value) : "Selecione ou digite um regexp..."}</span>
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0">
        <Command shouldFilter={false}>
          <CommandInput placeholder="Buscar sugestão ou digitar um regexp..." value={search} onValueChange={setSearch} />
          <CommandList>
            <CommandGroup>
              {filteredSuggestions.map((opt) => (
                <CommandItem
                  key={opt}
                  value={opt}
                  onSelect={() => {
                    onChange(toRegexp(opt));
                    setOpen(false);
                  }}
                >
                  <Check className={cn("mr-2 h-4 w-4", toRegexp(opt) === value ? "opacity-100" : "opacity-0")} />
                  <span className="font-mono">{opt}</span>
                </CommandItem>
              ))}
              {search.trim() && !hasExactMatch && (
                <CommandItem
                  value={`__custom__${search}`}
                  onSelect={() => {
                    onChange(search.trim());
                    setOpen(false);
                  }}
                >
                  <Check className="mr-2 h-4 w-4 opacity-0" />
                  Usar <span className="font-mono ml-1">"{search.trim()}"</span>
                </CommandItem>
              )}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}

interface ActiveSession {
  cluster: string;
  namespace: string;
  name: string;
  targetName: string;
  // defaultTargetSecretName — nome do Secret REAL sugerido pro "Salvar como Secret" do modal
  // (AKVDiscoveryModal.tsx), capturado no momento em que a consulta é iniciada — vem da referência
  // de ExternalSecret já existente escolhida pelo usuário, se houver (não o targetName acima, que
  // é sempre o Secret temporário desta consulta).
  defaultTargetSecretName?: string;
}

/**
 * AKVDiscoveryTab — página "Descoberta AKV" (Tools menu). Cria um ExternalSecret dedicado a esta
 * consulta (nome gerado, sempre removido ao final) usando um critério de busca configurável pelo
 * usuário, para conferir a sincronização com a fonte de segredos externa sem depender da
 * configuração do ExternalSecret já existente no namespace — que continua intocado. Ver
 * AKV-SECRET-VIEWER-STUDY.md para o histórico completo desta investigação.
 *
 * Este formulário escolhe cluster/namespace/referência/critério de busca; o resultado
 * (nomes/chaves/valores sincronizados) sempre abre em AKVDiscoveryModal — um modal próprio,
 * redimensionável, com Monaco e o ciclo de vida (criar → sincronizar → remover) do recurso.
 */
export default function AKVDiscoveryTab() {
  const { clusters } = useClusters();
  const [cluster, setCluster] = useState("");
  const [namespace, setNamespace] = useState("");
  const [selectedRef, setSelectedRef] = useState<string>("");
  const [secretStoreKind, setSecretStoreKind] = useState("ClusterSecretStore");
  const [secretStoreName, setSecretStoreName] = useState("");
  const [matchPattern, setMatchPattern] = useState("");
  const [reason, setReason] = useState("");
  const [starting, setStarting] = useState(false);
  const [activeSession, setActiveSession] = useState<ActiveSession | null>(null);

  const { data: namespaces = [] } = useQuery({
    queryKey: ["akv-discovery-namespaces", cluster],
    queryFn: () => apiClient.getNamespaces(cluster),
    enabled: !!cluster,
  });

  const {
    data: externalSecretsResp,
    isLoading: loadingExternalSecrets,
    isError: externalSecretsError,
  } = useQuery({
    queryKey: ["akv-discovery-external-secrets", cluster, namespace],
    queryFn: () => apiClient.listAKVExternalSecrets(cluster, namespace),
    enabled: !!cluster && !!namespace,
  });
  const existingSecrets: AKVExternalSecretSummary[] = useMemo(
    () => externalSecretsResp?.external_secrets ?? [],
    [externalSecretsResp]
  );

  const selectedRefSummary = useMemo(
    () => existingSecrets.find((s) => s.name === selectedRef),
    [existingSecrets, selectedRef]
  );

  // Sugestões do combobox de critério de busca: "*" (Tudo) sempre primeiro, seguido dos regexps
  // distintos já usados por ExternalSecrets existentes neste namespace (deduplicados, sem contar
  // ".*"/vazio, já cobertos pela opção "*").
  const matchPatternSuggestions = useMemo(() => {
    const distinct = new Set<string>();
    for (const s of existingSecrets) {
      const pattern = s.find_regexp?.trim();
      if (pattern && pattern !== ".*") distinct.add(pattern);
    }
    return ["*", ...Array.from(distinct).sort()];
  }, [existingSecrets]);

  const applyReference = (summary: AKVExternalSecretSummary) => {
    setSelectedRef(summary.name);
    setSecretStoreKind(summary.secret_store_kind || "ClusterSecretStore");
    setSecretStoreName(summary.secret_store_name || "");
    setMatchPattern(summary.find_regexp || "");
  };

  const isUnscopedPattern = matchPattern.trim() === ".*" || matchPattern.trim() === "";

  const canStart = !!cluster && !!namespace && !!secretStoreName.trim() && !!matchPattern.trim() && !!reason.trim();

  const handleStart = async () => {
    if (!canStart) return;
    setStarting(true);
    try {
      const res = await apiClient.startAKVDiscovery({
        cluster,
        namespace,
        secret_store_kind: secretStoreKind,
        secret_store_name: secretStoreName.trim(),
        regexp: matchPattern.trim(),
        reason: reason.trim(),
      });
      toast.success(`Consulta criada: ${res.name}`);
      setActiveSession({
        cluster,
        namespace,
        name: res.name,
        targetName: res.target_name,
        defaultTargetSecretName: selectedRefSummary?.target_name,
      });
    } catch (e) {
      toast.error("Falha ao iniciar a consulta", {
        description: e instanceof Error ? e.message : "Erro desconhecido",
      });
    } finally {
      setStarting(false);
    }
  };

  return (
    <div className="p-6 max-w-3xl mx-auto space-y-4">
      <div className="flex items-center gap-2">
        <ScanEye className="h-5 w-5 text-primary" />
        <h1 className="text-lg font-semibold">Descoberta AKV</h1>
      </div>
      <p className="text-sm text-muted-foreground">
        Cria um <code>ExternalSecret</code> dedicado a esta consulta (nome gerado, removido ao
        final) com um critério de busca configurável, para conferir o conteúdo sincronizado a
        partir do cofre de segredos sem depender da configuração do <code>ExternalSecret</code>{" "}
        já em uso no namespace. A configuração original não é alterada.
      </p>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <Label className="text-xs">Cluster</Label>
          <div className="mt-1">
            <SimpleSearchableSelect
              value={cluster}
              onChange={(v) => {
                setCluster(v);
                setNamespace("");
                setSelectedRef("");
                setSecretStoreName("");
                setMatchPattern("");
              }}
              options={clusters.map((c) => c.context)}
              placeholder="Selecione..."
              searchPlaceholder="Buscar cluster..."
              emptyMessage="Nenhum cluster encontrado."
            />
          </div>
        </div>
        <div>
          <Label className="text-xs">Namespace</Label>
          <div className="mt-1">
            <SimpleSearchableSelect
              value={namespace}
              onChange={(v) => {
                setNamespace(v);
                setSelectedRef("");
                setSecretStoreName("");
                setMatchPattern("");
              }}
              options={namespaces.map((n) => n.name)}
              placeholder="Selecione..."
              searchPlaceholder="Buscar namespace..."
              emptyMessage="Nenhum namespace encontrado."
              disabled={!cluster}
            />
          </div>
          <p className="text-[11px] text-muted-foreground mt-1">
            Só define onde o recurso temporário desta consulta é criado — não restringe o que
            pode ser encontrado na origem. Exceção: se a origem for do tipo{" "}
            <code>SecretStore</code> (não <code>ClusterSecretStore</code>), ela só pode ser
            referenciada a partir do mesmo namespace onde já existe.
          </p>
        </div>
      </div>

      {namespace && (
        <div className="border rounded-lg p-3 space-y-2">
          <Label className="text-xs flex items-center gap-1.5">
            <KeyRound className="h-3.5 w-3.5" /> Configurações já existentes neste namespace
          </Label>
          {loadingExternalSecrets && (
            <p className="text-xs text-muted-foreground flex items-center gap-1.5">
              <Loader2 className="h-3 w-3 animate-spin" /> Carregando...
            </p>
          )}
          {externalSecretsError && (
            <p className="text-xs text-amber-500">Não foi possível listar — os campos abaixo podem ser preenchidos manualmente.</p>
          )}
          {!loadingExternalSecrets && existingSecrets.length === 0 && (
            <p className="text-xs text-muted-foreground">Nenhuma encontrada — preencha os campos manualmente abaixo.</p>
          )}
          <div className="space-y-1.5">
            {existingSecrets.map((s) => (
              <button
                key={s.name}
                type="button"
                onClick={() => applyReference(s)}
                className={cn(
                  "w-full text-left text-xs rounded border p-2 transition-colors",
                  selectedRef === s.name ? "border-primary bg-primary/5" : "border-border/60 hover:bg-muted/40"
                )}
              >
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="font-mono">{s.name}</span>
                  <Badge
                    variant="outline"
                    className={cn(
                      "text-[10px]",
                      s.ready ? "bg-green-500/10 text-green-500 border-green-500/30" : "bg-red-500/10 text-red-500 border-red-500/30"
                    )}
                  >
                    {s.ready ? "sincronizado" : (s.status_reason || "não sincronizado")}
                  </Badge>
                </div>
                <div className="text-muted-foreground mt-0.5">
                  Origem: <span className="font-mono">{s.secret_store_kind}/{s.secret_store_name}</span>
                  {s.find_regexp && <> · critério: <span className="font-mono">{s.find_regexp}</span></>}
                </div>
              </button>
            ))}
          </div>
        </div>
      )}

      <div className="border rounded-lg p-3 space-y-3">
        <div>
          <Label className="text-xs">Tipo da origem</Label>
          <div className="flex gap-2 mt-1">
            {(["ClusterSecretStore", "SecretStore"] as const).map((kind) => (
              <button
                key={kind}
                type="button"
                onClick={() => setSecretStoreKind(kind)}
                className={cn(
                  "text-xs px-3 py-1.5 rounded border transition-colors",
                  secretStoreKind === kind ? "border-primary bg-primary/10 text-primary" : "border-border/60 text-muted-foreground hover:bg-muted/40"
                )}
              >
                {kind}
              </button>
            ))}
          </div>
        </div>
        <div>
          <Label className="text-xs">Nome da origem</Label>
          <Input
            className="mt-1 h-9 text-xs font-mono"
            placeholder="ex: akv-tms-prd"
            value={secretStoreName}
            onChange={(e) => setSecretStoreName(e.target.value)}
          />
        </div>
        <div>
          <Label className="text-xs">Critério de busca (padrão de nome)</Label>
          <div className="mt-1">
            <MatchPatternCombobox
              value={matchPattern}
              onChange={setMatchPattern}
              suggestions={matchPatternSuggestions}
              disabled={!cluster || !namespace || !secretStoreName.trim()}
            />
          </div>
          <p className="text-[11px] text-muted-foreground mt-1">
            <span className="font-mono">*</span> busca tudo na origem (regexp <span className="font-mono">.*</span>)
            — útil quando a secret não foi criada no namespace/local esperado. As demais sugestões
            vêm dos critérios já usados por ExternalSecrets existentes neste namespace; digite um
            regexp próprio quando nenhuma sugestão servir.
          </p>
        </div>
        <div>
          <Label className="text-xs">Motivo (registrado para auditoria)</Label>
          <Input
            className="mt-1 h-9 text-xs"
            placeholder="ex: conferir item ausente na configuração atual"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </div>

        {isUnscopedPattern && (
          <Alert variant="default" className="border-amber-500/40 bg-amber-500/5">
            <Info className="h-4 w-4 text-amber-500" />
            <AlertTitle className="text-xs">Critério sem restrição de nome</AlertTitle>
            <AlertDescription className="text-xs">
              A mesma origem costuma armazenar itens de mais de uma aplicação. Um critério sem
              restrição (ex: <code>.*</code>) retorna todos os itens correspondentes, não só os
              deste namespace. Prefira um critério mais específico quando possível.
            </AlertDescription>
          </Alert>
        )}
        {selectedRefSummary?.rewrite_source && (
          <p className="text-[11px] text-muted-foreground">
            Regra de renomeação usada por <span className="font-mono">{selectedRefSummary.name}</span> (referência apenas, não aplicada aqui):{" "}
            <span className="font-mono">{selectedRefSummary.rewrite_source} → {selectedRefSummary.rewrite_target}</span>
          </p>
        )}

        <ProtectedAction>
          <Button className="w-full" onClick={handleStart} disabled={!canStart || starting}>
            {starting ? <Loader2 className="h-4 w-4 mr-1.5 animate-spin" /> : <Play className="h-4 w-4 mr-1.5" />}
            Iniciar Consulta
          </Button>
        </ProtectedAction>
      </div>

      {activeSession && (
        <AKVDiscoveryModal
          cluster={activeSession.cluster}
          namespace={activeSession.namespace}
          name={activeSession.name}
          targetName={activeSession.targetName}
          defaultTargetSecretName={activeSession.defaultTargetSecretName}
          onClose={() => setActiveSession(null)}
        />
      )}
    </div>
  );
}
