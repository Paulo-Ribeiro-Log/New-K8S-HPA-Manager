import { useEffect, useRef, useState } from 'react';
import cytoscape, { Core, EdgeSingular, NodeSingular } from 'cytoscape';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Loader2, RefreshCw, ZoomIn, ZoomOut, Maximize2 } from 'lucide-react';
import { apiClient } from '@/lib/api/client';
import { ServiceGraphResponse, ServiceMeshFilters } from '@/types/servicemesh';
import { toast } from 'sonner';

interface ServiceMeshGraphProps {
  cluster: string;
}

export function ServiceMeshGraph({ cluster }: ServiceMeshGraphProps) {
  const cyRef = useRef<HTMLDivElement>(null);
  const cyInstance = useRef<Core | null>(null);
  const [loading, setLoading] = useState(false);
  const [graphData, setGraphData] = useState<ServiceGraphResponse | null>(null);
  const [namespaces, setNamespaces] = useState<string[]>([]);
  const [selectedNode, setSelectedNode] = useState<NodeSingular | null>(null);

  const [filters, setFilters] = useState<ServiceMeshFilters>({
    cluster: cluster,
    namespace: '',
    duration: '5m',
    graphType: 'workload',
  });

  // Carregar namespaces com Istio habilitado
  useEffect(() => {
    if (cluster) {
      loadNamespaces();
    }
  }, [cluster]);

  // Inicializar grafo quando há dados
  useEffect(() => {
    if (graphData && cyRef.current) {
      initializeGraph();
    }
  }, [graphData]);

  const loadNamespaces = async () => {
    try {
      const response = await apiClient.getServiceMeshNamespaces(cluster);
      setNamespaces(response.namespaces);
      
      if (response.namespaces.length > 0 && !filters.namespace) {
        setFilters(prev => ({ ...prev, namespace: response.namespaces[0] }));
      }
    } catch (error) {
      toast.error(`Erro ao carregar namespaces: ${error instanceof Error ? error.message : 'Erro desconhecido'}`);
    }
  };

  const loadServiceGraph = async () => {
    if (!filters.namespace) {
      toast.error('É necessário selecionar um namespace para visualizar o service mesh');
      return;
    }

    setLoading(true);
    try {
      const data = await apiClient.getServiceGraph(
        filters.cluster,
        filters.namespace,
        filters.duration,
        filters.graphType
      );
      setGraphData(data);
      
      toast.success(`Grafo atualizado: ${data.nodes.length} serviços, ${data.edges.length} conexões`);
    } catch (error) {
      toast.error(`Erro ao carregar service mesh: ${error instanceof Error ? error.message : 'Erro desconhecido'}`);
    } finally {
      setLoading(false);
    }
  };

  const initializeGraph = () => {
    if (!cyRef.current || !graphData) return;

    // Destruir instância anterior se existir
    if (cyInstance.current) {
      cyInstance.current.destroy();
    }

    // Converter dados para formato Cytoscape
    const elements = [
      // Nodes
      ...graphData.nodes.map(node => ({
        data: {
          id: node.id,
          label: node.label,
          type: node.type,
          namespace: node.namespace,
          app: node.app,
          version: node.version,
          isRoot: node.isRoot,
          isInaccessible: node.isInaccessible,
          isOutside: node.isOutside,
          requestRate: node.requestRate,
          errorRate: node.errorRate,
        },
      })),
      // Edges
      ...graphData.edges.map(edge => ({
        data: {
          id: edge.id,
          source: edge.source,
          target: edge.target,
          protocol: edge.protocol,
          requestRate: edge.requestRate,
          responseTime: edge.responseTime,
          errorRate: edge.errorRate,
        },
      })),
    ];

    // Inicializar Cytoscape
    cyInstance.current = cytoscape({
      container: cyRef.current,
      elements: elements,
      style: [
        {
          selector: 'node',
          style: {
            'background-color': '#3b82f6',
            'label': 'data(label)',
            'color': '#ffffff',
            'text-valign': 'center',
            'text-halign': 'center',
            'font-size': '12px',
            'width': '60px',
            'height': '60px',
            'border-width': '2px',
            'border-color': '#1e40af',
          },
        },
        {
          selector: 'node[type="service"]',
          style: {
            'background-color': '#10b981',
            'border-color': '#059669',
            'shape': 'rectangle',
          },
        },
        {
          selector: 'node[type="app"]',
          style: {
            'background-color': '#8b5cf6',
            'border-color': '#7c3aed',
            'shape': 'diamond',
          },
        },
        {
          selector: 'node[isRoot]',
          style: {
            'border-width': '4px',
            'border-color': '#f59e0b',
          },
        },
        {
          selector: 'node[isInaccessible]',
          style: {
            'background-color': '#ef4444',
            'border-color': '#dc2626',
          },
        },
        {
          selector: 'node[isOutside]',
          style: {
            'background-color': '#6b7280',
            'border-color': '#4b5563',
          },
        },
        {
          selector: 'node:selected',
          style: {
            'border-width': '4px',
            'border-color': '#fbbf24',
            'background-color': '#f59e0b',
          },
        },
        {
          selector: 'edge',
          style: {
            'width': 2,
            'line-color': '#9ca3af',
            'target-arrow-color': '#9ca3af',
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
            'arrow-scale': 1.5,
          },
        },
        {
          selector: 'edge[protocol="http"]',
          style: {
            'line-color': '#3b82f6',
            'target-arrow-color': '#3b82f6',
          },
        },
        {
          selector: 'edge[protocol="tcp"]',
          style: {
            'line-color': '#10b981',
            'target-arrow-color': '#10b981',
          },
        },
        {
          selector: 'edge[errorRate > 0]',
          style: {
            'line-color': '#ef4444',
            'target-arrow-color': '#ef4444',
            'line-style': 'dashed',
          },
        },
      ],
      layout: {
        name: 'cose',
        idealEdgeLength: 100,
        nodeOverlap: 20,
        refresh: 20,
        fit: true,
        padding: 30,
        randomize: false,
        componentSpacing: 100,
        nodeRepulsion: 400000,
        edgeElasticity: 100,
        nestingFactor: 5,
        gravity: 80,
        numIter: 1000,
        initialTemp: 200,
        coolingFactor: 0.95,
        minTemp: 1.0,
      },
    });

    // Event handlers
    cyInstance.current.on('tap', 'node', (event) => {
      const node = event.target;
      setSelectedNode(node);
    });

    cyInstance.current.on('tap', (event) => {
      if (event.target === cyInstance.current) {
        setSelectedNode(null);
      }
    });
  };

  const handleZoomIn = () => {
    if (cyInstance.current) {
      cyInstance.current.zoom(cyInstance.current.zoom() * 1.2);
      cyInstance.current.center();
    }
  };

  const handleZoomOut = () => {
    if (cyInstance.current) {
      cyInstance.current.zoom(cyInstance.current.zoom() * 0.8);
      cyInstance.current.center();
    }
  };

  const handleFitView = () => {
    if (cyInstance.current) {
      cyInstance.current.fit(undefined, 30);
    }
  };

  return (
    <div className="space-y-4">
      {/* Controles */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle>Service Mesh Topology</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
            <div>
              <label className="text-sm font-medium mb-2 block">Namespace</label>
              <Select
                value={filters.namespace}
                onValueChange={(value) => setFilters(prev => ({ ...prev, namespace: value }))}
              >
                <SelectTrigger>
                  <SelectValue placeholder="Selecione namespace" />
                </SelectTrigger>
                <SelectContent>
                  {namespaces.map(ns => (
                    <SelectItem key={ns} value={ns}>{ns}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div>
              <label className="text-sm font-medium mb-2 block">Duração</label>
              <Select
                value={filters.duration}
                onValueChange={(value: any) => setFilters(prev => ({ ...prev, duration: value }))}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="60s">1 minuto</SelectItem>
                  <SelectItem value="5m">5 minutos</SelectItem>
                  <SelectItem value="10m">10 minutos</SelectItem>
                  <SelectItem value="30m">30 minutos</SelectItem>
                  <SelectItem value="1h">1 hora</SelectItem>
                  <SelectItem value="6h">6 horas</SelectItem>
                  <SelectItem value="12h">12 horas</SelectItem>
                  <SelectItem value="24h">24 horas</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div>
              <label className="text-sm font-medium mb-2 block">Tipo de Grafo</label>
              <Select
                value={filters.graphType}
                onValueChange={(value: any) => setFilters(prev => ({ ...prev, graphType: value }))}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="workload">Workload</SelectItem>
                  <SelectItem value="app">App</SelectItem>
                  <SelectItem value="service">Service</SelectItem>
                  <SelectItem value="versioned_app">Versioned App</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-end gap-2">
              <Button
                onClick={loadServiceGraph}
                disabled={loading || !filters.namespace}
                className="flex-1"
              >
                {loading ? (
                  <><Loader2 className="mr-2 h-4 w-4 animate-spin" /> Carregando...</>
                ) : (
                  <><RefreshCw className="mr-2 h-4 w-4" /> Atualizar</>
                )}
              </Button>
            </div>
          </div>

          {graphData && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Badge variant="secondary">{graphData.nodes.length} nós</Badge>
              <Badge variant="secondary">{graphData.edges.length} conexões</Badge>
              <Badge variant="outline">
                Atualizado: {new Date(graphData.timestamp * 1000).toLocaleTimeString()}
              </Badge>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Grafo */}
      <Card className="relative">
        <CardContent className="p-0">
          <div className="relative">
            <div
              ref={cyRef}
              className="w-full bg-slate-50 dark:bg-slate-900 border rounded-lg"
              style={{ height: '600px' }}
            />
            
            {/* Controles de Zoom */}
            <div className="absolute top-4 right-4 flex flex-col gap-2">
              <Button size="icon" variant="secondary" onClick={handleZoomIn}>
                <ZoomIn className="h-4 w-4" />
              </Button>
              <Button size="icon" variant="secondary" onClick={handleZoomOut}>
                <ZoomOut className="h-4 w-4" />
              </Button>
              <Button size="icon" variant="secondary" onClick={handleFitView}>
                <Maximize2 className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Detalhes do Nó Selecionado */}
      {selectedNode && (
        <Card>
          <CardHeader>
            <CardTitle>Detalhes do Serviço</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <div className="text-sm font-medium text-muted-foreground">Nome</div>
                <div className="text-lg font-semibold">{selectedNode.data('label')}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">Tipo</div>
                <Badge>{selectedNode.data('type')}</Badge>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">Namespace</div>
                <div>{selectedNode.data('namespace')}</div>
              </div>
              {selectedNode.data('app') && (
                <div>
                  <div className="text-sm font-medium text-muted-foreground">App</div>
                  <div>{selectedNode.data('app')}</div>
                </div>
              )}
              {selectedNode.data('version') && (
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Versão</div>
                  <Badge variant="outline">{selectedNode.data('version')}</Badge>
                </div>
              )}
              {selectedNode.data('requestRate') && (
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Request Rate</div>
                  <div>{selectedNode.data('requestRate')}</div>
                </div>
              )}
              {selectedNode.data('isRoot') && (
                <div className="col-span-2">
                  <Badge variant="default">Root Service</Badge>
                </div>
              )}
              {selectedNode.data('isInaccessible') && (
                <div className="col-span-2">
                  <Badge variant="destructive">Inacessível</Badge>
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Legenda */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle>Legenda</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 md:grid-cols-3 gap-4 text-sm">
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded-full bg-blue-500 border-2 border-blue-700" />
              <span>Workload</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 bg-green-500 border-2 border-green-700" />
              <span>Service</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rotate-45 bg-purple-500 border-2 border-purple-700" />
              <span>App</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded-full bg-red-500 border-2 border-red-700" />
              <span>Inacessível</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded-full bg-orange-500 border-4 border-orange-600" />
              <span>Root</span>
            </div>
            <div className="flex items-center gap-2">
              <div className="w-4 h-4 rounded-full bg-gray-500 border-2 border-gray-700" />
              <span>Outside</span>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
