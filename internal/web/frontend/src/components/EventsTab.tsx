import { useEffect, useState, useMemo } from "react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Search, RefreshCcw, Eye, EyeOff, AlertTriangle, Info, Filter, Calendar, Clock } from "lucide-react";
import { toast } from "sonner";
import type { Namespace, EventSummary } from "@/lib/api/types";
import { apiClient } from "@/lib/api/client";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

interface EventsTabProps {
  cluster: string;
  namespaces: Namespace[];
  selectedNamespace: string;
  onNamespaceChange: (namespace: string) => void;
  showSystemNamespaces: boolean;
  onToggleSystemNamespaces: () => void;
}

export const EventsTab = ({
  cluster,
  namespaces,
  selectedNamespace,
  onNamespaceChange,
  showSystemNamespaces,
  onToggleSystemNamespaces,
}: EventsTabProps) => {
  const [searchQuery, setSearchQuery] = useState("");
  const [events, setEvents] = useState<EventSummary[]>([]);
  const [loading, setLoading] = useState(false);
  const [eventTypeFilter, setEventTypeFilter] = useState<"all" | "Normal" | "Warning">("all");
  const [selectedEvent, setSelectedEvent] = useState<EventSummary | null>(null);
  const [showDetailsModal, setShowDetailsModal] = useState(false);

  // Handler para mudança de namespace (converte "__all__" para "")
  const handleNamespaceChange = (value: string) => {
    onNamespaceChange(value === "__all__" ? "" : value);
  };

  const filteredNamespaces = useMemo(() => {
    if (showSystemNamespaces) return namespaces;
    return namespaces.filter((ns) => !ns.isSystem);
  }, [namespaces, showSystemNamespaces]);

  // Carregar events
  const loadEvents = async () => {
    if (!cluster) {
      toast.error("Nenhum cluster selecionado");
      return;
    }

    setLoading(true);
    try {
      const namespacesToFetch = selectedNamespace && selectedNamespace !== "__all__" ? [selectedNamespace] : undefined;
      const typeFilter = eventTypeFilter !== "all" ? eventTypeFilter : undefined;

      const data = await apiClient.getEvents(
        cluster,
        namespacesToFetch,
        searchQuery || undefined,
        typeFilter,
        showSystemNamespaces,
        true // bypass cache
      );

      setEvents(data);
    } catch (error) {
      console.error("Erro ao carregar events:", error);
      toast.error("Erro ao carregar events");
    } finally {
      setLoading(false);
    }
  };

  // Carregar events ao montar e quando cluster/namespace mudarem
  useEffect(() => {
    loadEvents();
    // Auto-refresh a cada 30 segundos
    const interval = setInterval(loadEvents, 30000);
    return () => clearInterval(interval);
  }, [cluster, selectedNamespace, eventTypeFilter, showSystemNamespaces]);

  // Helper: Obter cor do badge baseado no tipo do evento
  const getEventBadgeVariant = (type: string) => {
    switch (type) {
      case "Warning":
        return "destructive";
      case "Normal":
        return "default";
      default:
        return "secondary";
    }
  };

  // Helper: Obter ícone baseado no tipo
  const getEventIcon = (type: string) => {
    return type === "Warning" ? (
      <AlertTriangle className="h-4 w-4 text-destructive" />
    ) : (
      <Info className="h-4 w-4 text-muted-foreground" />
    );
  };

  // Filtrar events por busca local (além do filtro do backend)
  const displayedEvents = useMemo(() => {
    if (!searchQuery) return events;

    const query = searchQuery.toLowerCase();
    return events.filter((event) => {
      return (
        event.name.toLowerCase().includes(query) ||
        event.namespace.toLowerCase().includes(query) ||
        event.reason.toLowerCase().includes(query) ||
        event.message.toLowerCase().includes(query) ||
        event.involvedObject.name.toLowerCase().includes(query) ||
        event.involvedObject.kind.toLowerCase().includes(query)
      );
    });
  }, [events, searchQuery]);

  // Helper: Formatar timestamp para exibição legível
  const formatTimestamp = (timestamp: string) => {
    if (!timestamp) return "-";
    const date = new Date(timestamp);
    return date.toLocaleString("pt-BR", {
      day: "2-digit",
      month: "2-digit",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  };

  // Handler: Abrir modal de detalhes
  const handleRowClick = (event: EventSummary) => {
    setSelectedEvent(event);
    setShowDetailsModal(true);
  };

  return (
    <div className="flex flex-col h-full">
      {/* Header - Filtros e controles */}
      <div className="p-4 border-b space-y-3 flex-shrink-0">
        {/* Linha 1: Namespace, Tipo, System Toggle, Refresh */}
        <div className="flex items-center gap-3">
          <div className="w-64">
            <Select value={selectedNamespace || "__all__"} onValueChange={handleNamespaceChange}>
              <SelectTrigger>
                <SelectValue placeholder="Todos os namespaces" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">Todos os namespaces</SelectItem>
                {filteredNamespaces.map((ns) => (
                  <SelectItem key={ns.name} value={ns.name}>
                    {ns.name}
                    {ns.isSystem && " (sistema)"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="w-48">
            <Select
              value={eventTypeFilter}
              onValueChange={(value) => setEventTypeFilter(value as "all" | "Normal" | "Warning")}
            >
              <SelectTrigger>
                <SelectValue placeholder="Tipo de evento" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">Todos os tipos</SelectItem>
                <SelectItem value="Normal">Normal</SelectItem>
                <SelectItem value="Warning">Warning</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={onToggleSystemNamespaces}
            className="gap-2"
          >
            {showSystemNamespaces ? (
              <>
                <EyeOff className="h-4 w-4" /> Ocultar Sistema
              </>
            ) : (
              <>
                <Eye className="h-4 w-4" /> Mostrar Sistema
              </>
            )}
          </Button>

          <Button
            variant="outline"
            size="sm"
            onClick={loadEvents}
            disabled={loading}
            className="gap-2 ml-auto"
          >
            <RefreshCcw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            Atualizar
          </Button>
        </div>

        {/* Linha 2: Busca */}
        <div className="relative">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Buscar eventos (nome, namespace, reason, mensagem, objeto...)"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-9"
          />
        </div>

        {/* Info - Contagem */}
        <div className="text-sm text-muted-foreground">
          Mostrando {displayedEvents.length} de {events.length} eventos
        </div>
      </div>

      {/* Tabela de Events */}
      <ScrollArea className="flex-1">
        <TooltipProvider>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[50px]">Tipo</TableHead>
                <TableHead className="w-[120px]">Namespace</TableHead>
                <TableHead className="w-[180px]">Objeto</TableHead>
                <TableHead className="w-[120px]">Reason</TableHead>
                <TableHead className="min-w-[250px]">Mensagem</TableHead>
                <TableHead className="w-[80px]">Count</TableHead>
                <TableHead className="w-[150px]">First Seen</TableHead>
                <TableHead className="w-[150px]">Last Seen</TableHead>
                <TableHead className="w-[120px]">Source</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && events.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={9} className="text-center py-8 text-muted-foreground">
                    Carregando eventos...
                  </TableCell>
                </TableRow>
              ) : displayedEvents.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={9} className="text-center py-8 text-muted-foreground">
                    Nenhum evento encontrado
                  </TableCell>
                </TableRow>
              ) : (
                displayedEvents.map((event, index) => (
                  <TableRow
                    key={`${event.cluster}-${event.namespace}-${event.name}-${index}`}
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => handleRowClick(event)}
                  >
                    {/* Tipo */}
                    <TableCell>
                      <div className="flex items-center justify-center">
                        {getEventIcon(event.type)}
                      </div>
                    </TableCell>

                    {/* Namespace */}
                    <TableCell>
                      <span className="font-mono text-sm">{event.namespace}</span>
                    </TableCell>

                    {/* Objeto Envolvido */}
                    <TableCell>
                      <div className="flex flex-col gap-1">
                        <Badge variant="outline" className="w-fit text-xs">
                          {event.involvedObject.kind}
                        </Badge>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="font-mono text-xs text-muted-foreground truncate max-w-[160px] cursor-help">
                              {event.involvedObject.name}
                            </span>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p className="font-mono text-xs">{event.involvedObject.name}</p>
                          </TooltipContent>
                        </Tooltip>
                      </div>
                    </TableCell>

                    {/* Reason */}
                    <TableCell>
                      <Badge variant={getEventBadgeVariant(event.type)} className="text-xs">
                        {event.reason}
                      </Badge>
                    </TableCell>

                    {/* Mensagem */}
                    <TableCell>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <div className="text-sm break-words max-w-[400px] line-clamp-2 cursor-help">
                            {event.message}
                          </div>
                        </TooltipTrigger>
                        <TooltipContent className="max-w-[500px]">
                          <p className="text-sm whitespace-pre-wrap">{event.message}</p>
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>

                    {/* Count */}
                    <TableCell>
                      <span className="font-mono text-sm">{event.count}</span>
                    </TableCell>

                    {/* First Seen */}
                    <TableCell>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <div className="flex items-center gap-1 text-xs text-muted-foreground cursor-help">
                            <Calendar className="h-3 w-3" />
                            <span>{event.firstTimestamp ? formatTimestamp(event.firstTimestamp).split(" ")[0] : "-"}</span>
                          </div>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p className="text-xs">{formatTimestamp(event.firstTimestamp)}</p>
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>

                    {/* Last Seen */}
                    <TableCell>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <div className="flex items-center gap-1 text-xs text-muted-foreground cursor-help">
                            <Clock className="h-3 w-3" />
                            <span>{event.lastTimestamp ? formatTimestamp(event.lastTimestamp).split(" ")[0] : "-"}</span>
                          </div>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p className="text-xs">{formatTimestamp(event.lastTimestamp)}</p>
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>

                    {/* Source Component */}
                    <TableCell>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="text-xs text-muted-foreground truncate max-w-[110px] cursor-help block">
                            {event.sourceComponent || "-"}
                          </span>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p className="text-xs">
                            Component: {event.sourceComponent || "-"}
                            {event.sourceHost && (
                              <>
                                <br />
                                Host: {event.sourceHost}
                              </>
                            )}
                          </p>
                        </TooltipContent>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TooltipProvider>
      </ScrollArea>

      {/* Modal de Detalhes do Evento */}
      <Dialog open={showDetailsModal} onOpenChange={setShowDetailsModal}>
        <DialogContent className="max-w-3xl max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              {selectedEvent && getEventIcon(selectedEvent.type)}
              Detalhes do Evento
            </DialogTitle>
            <DialogDescription>
              {selectedEvent && `${selectedEvent.involvedObject.kind}: ${selectedEvent.involvedObject.name}`}
            </DialogDescription>
          </DialogHeader>

          {selectedEvent && (
            <div className="space-y-4">
              {/* Informações Básicas */}
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <div className="text-sm font-medium text-muted-foreground">Cluster</div>
                  <div className="font-mono text-sm">{selectedEvent.cluster}</div>
                </div>
                <div className="space-y-2">
                  <div className="text-sm font-medium text-muted-foreground">Namespace</div>
                  <div className="font-mono text-sm">{selectedEvent.namespace}</div>
                </div>
                <div className="space-y-2">
                  <div className="text-sm font-medium text-muted-foreground">Tipo</div>
                  <Badge variant={getEventBadgeVariant(selectedEvent.type)}>
                    {selectedEvent.type}
                  </Badge>
                </div>
                <div className="space-y-2">
                  <div className="text-sm font-medium text-muted-foreground">Reason</div>
                  <Badge variant={getEventBadgeVariant(selectedEvent.type)}>
                    {selectedEvent.reason}
                  </Badge>
                </div>
              </div>

              {/* Objeto Envolvido */}
              <div className="space-y-2 border-t pt-4">
                <div className="text-sm font-medium text-muted-foreground">Objeto Envolvido</div>
                <div className="grid grid-cols-2 gap-2">
                  <div className="text-sm">
                    <span className="font-medium">Kind:</span> {selectedEvent.involvedObject.kind}
                  </div>
                  <div className="text-sm">
                    <span className="font-medium">Name:</span>{" "}
                    <span className="font-mono">{selectedEvent.involvedObject.name}</span>
                  </div>
                  {selectedEvent.involvedObject.uid && (
                    <div className="text-sm col-span-2">
                      <span className="font-medium">UID:</span>{" "}
                      <span className="font-mono text-xs">{selectedEvent.involvedObject.uid}</span>
                    </div>
                  )}
                </div>
              </div>

              {/* Timestamps */}
              <div className="space-y-2 border-t pt-4">
                <div className="text-sm font-medium text-muted-foreground">Timestamps</div>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-1">
                    <div className="flex items-center gap-2 text-sm">
                      <Calendar className="h-4 w-4 text-muted-foreground" />
                      <span className="font-medium">First Seen:</span>
                    </div>
                    <div className="font-mono text-sm">{formatTimestamp(selectedEvent.firstTimestamp)}</div>
                    <div className="text-xs text-muted-foreground">{selectedEvent.age} ago</div>
                  </div>
                  <div className="space-y-1">
                    <div className="flex items-center gap-2 text-sm">
                      <Clock className="h-4 w-4 text-muted-foreground" />
                      <span className="font-medium">Last Seen:</span>
                    </div>
                    <div className="font-mono text-sm">{formatTimestamp(selectedEvent.lastTimestamp)}</div>
                    <div className="text-xs text-muted-foreground">Count: {selectedEvent.count}x</div>
                  </div>
                </div>
              </div>

              {/* Source */}
              <div className="space-y-2 border-t pt-4">
                <div className="text-sm font-medium text-muted-foreground">Source</div>
                <div className="grid grid-cols-2 gap-2">
                  <div className="text-sm">
                    <span className="font-medium">Component:</span>{" "}
                    <span className="font-mono">{selectedEvent.sourceComponent || "-"}</span>
                  </div>
                  {selectedEvent.sourceHost && (
                    <div className="text-sm">
                      <span className="font-medium">Host:</span>{" "}
                      <span className="font-mono">{selectedEvent.sourceHost}</span>
                    </div>
                  )}
                </div>
              </div>

              {/* Mensagem Completa */}
              <div className="space-y-2 border-t pt-4">
                <div className="text-sm font-medium text-muted-foreground">Mensagem</div>
                <div className="bg-muted p-3 rounded-md">
                  <p className="text-sm whitespace-pre-wrap break-words">{selectedEvent.message}</p>
                </div>
              </div>
            </div>
          )}
        </DialogContent>
      </Dialog>
    </div>
  );
};
