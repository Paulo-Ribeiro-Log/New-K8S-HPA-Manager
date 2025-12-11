import { useEffect, useRef, useState } from 'react';
import cytoscape, { Core, EdgeSingular, NodeSingular } from 'cytoscape';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion';
import { Checkbox } from '@/components/ui/checkbox';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { Label } from '@/components/ui/label';
import { Loader2, RefreshCw, ZoomIn, ZoomOut, Maximize2, Minimize2, Settings, Clock, Pause, Play } from 'lucide-react';
import { apiClient } from '@/lib/api/client';
import { ServiceGraphResponse, ServiceMeshFilters } from '@/types/servicemesh';
import { toast } from 'sonner';

interface ServiceMeshGraphProps {
  cluster: string;
}

export function ServiceMeshGraph({ cluster }: ServiceMeshGraphProps) {
  const cyRef = useRef<HTMLDivElement>(null);
  const cyInstance = useRef<Core | null>(null);
  const refreshIntervalRef = useRef<NodeJS.Timeout | null>(null);
  const animationIntervalRef = useRef<NodeJS.Timeout | null>(null);
  const [loading, setLoading] = useState(false);
  const [graphData, setGraphData] = useState<ServiceGraphResponse | null>(null);
  const [namespaces, setNamespaces] = useState<string[]>([]);
  const [selectedNode, setSelectedNode] = useState<NodeSingular | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [showFullscreenControls, setShowFullscreenControls] = useState(true);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);
  const [autoRefreshInterval, setAutoRefreshInterval] = useState<number>(0); // 0 = off, tempo em ms

  const [filters, setFilters] = useState<ServiceMeshFilters>({
    cluster: cluster,
    namespace: '',
    duration: '5m',
    graphType: 'workload',
  });

  const [displayOptions, setDisplayOptions] = useState({
    showEdgeLabels: {
      responseTime: false,
      responseTimeType: '99th',
      throughput: false,
      trafficDistribution: false,
      trafficRate: true
    },
    show: {
      clusterBoxes: true,
      namespaceBoxes: true,
      compressedHide: true,
      idleEdges: false,
      idleNodes: false,
      operationNodes: true,
      rank: false,
      serviceNodes: true,
      trafficAnimation: true
    },
    showBadges: {
      missingSidecars: false,
      security: false,
      virtualServices: false
    }
  });

  // Carregar namespaces com Istio habilitado
  useEffect(() => {
    if (cluster) {
      loadNamespaces();
    }
  }, [cluster]);

  // Inicializar grafo quando há dados ou opções mudam
  useEffect(() => {
    if (graphData && cyRef.current) {
      initializeGraph();
    }
  }, [graphData, displayOptions]);

  // Recriar grafo quando mudar fullscreen (porque muda de container)
  useEffect(() => {
    if (graphData && cyRef.current && cyInstance.current) {
      // Destruir instância atual
      cyInstance.current.destroy();
      cyInstance.current = null;
      
      // Aguardar DOM atualizar e recriar
      setTimeout(() => {
        if (cyRef.current) {
          initializeGraph();
        }
      }, 100);
    }
  }, [isFullscreen]);

  // Auto-refresh
  useEffect(() => {
    // Limpar intervalo anterior
    if (refreshIntervalRef.current) {
      clearInterval(refreshIntervalRef.current);
      refreshIntervalRef.current = null;
    }

    // Se auto-refresh estiver ativo, configurar novo intervalo
    if (autoRefreshInterval > 0 && filters.namespace) {
      refreshIntervalRef.current = setInterval(() => {
        loadServiceGraphSilent();
      }, autoRefreshInterval);
    }

    // Cleanup ao desmontar
    return () => {
      if (refreshIntervalRef.current) {
        clearInterval(refreshIntervalRef.current);
      }
    };
  }, [autoRefreshInterval, filters.namespace, filters.duration, filters.graphType, displayOptions]);

  // Controlar animação de tráfego
  useEffect(() => {
    // Limpar animação anterior
    if (animationIntervalRef.current) {
      clearInterval(animationIntervalRef.current);
      animationIntervalRef.current = null;
    }

    // Iniciar nova animação se habilitada
    if (displayOptions.show.trafficAnimation && cyInstance.current) {
      let offset = 0;
      animationIntervalRef.current = setInterval(() => {
        if (cyInstance.current) {
          offset -= 0.5; // Negativo para seguir direção das setas
          cyInstance.current.edges().style('line-dash-offset', offset);
        }
      }, 50);
    } else if (cyInstance.current) {
      // Reset dash offset quando desabilitado
      cyInstance.current.edges().style('line-dash-offset', 0);
    }

    // Cleanup
    return () => {
      if (animationIntervalRef.current) {
        clearInterval(animationIntervalRef.current);
      }
    };
  }, [displayOptions.show.trafficAnimation, graphData]);

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

  const loadServiceGraph = async (silent: boolean = false) => {
    if (!filters.namespace) {
      if (!silent) {
        toast.error('É necessário selecionar um namespace para visualizar o service mesh');
      }
      return;
    }

    setLoading(true);
    try {
      const data = await apiClient.getServiceGraph(
        filters.cluster,
        filters.namespace,
        filters.duration,
        filters.graphType,
        {
          injectServiceNodes: displayOptions.show.serviceNodes,
          includeIdleEdges: displayOptions.show.idleEdges,
          includeIdleNodes: displayOptions.show.idleNodes,
        }
      );
      setGraphData(data);
      setLastRefresh(new Date());
      
      if (!silent) {
        toast.success(`Grafo atualizado: ${data.nodes.length} serviços, ${data.edges.length} conexões`);
      }
    } catch (error) {
      if (!silent) {
        toast.error(`Erro ao carregar service mesh: ${error instanceof Error ? error.message : 'Erro desconhecido'}`);
      }
    } finally {
      setLoading(false);
    }
  };

  const loadServiceGraphSilent = () => {
    loadServiceGraph(true);
  };

  const updateGraphData = () => {
    if (!cyInstance.current || !graphData) return;

    // Atualizar apenas os dados dos elementos existentes sem recriar o grafo
    graphData.edges.forEach(edge => {
      const cyEdge = cyInstance.current?.getElementById(edge.id);
      if (cyEdge) {
        cyEdge.data({
          ...cyEdge.data(),
          requestRate: edge.requestRate,
          responseTime: edge.responseTime,
          errorRate: edge.errorRate,
          protocol: edge.protocol,
        });
      }
    });

    graphData.nodes.forEach(node => {
      const cyNode = cyInstance.current?.getElementById(node.id);
      if (cyNode) {
        cyNode.data({
          ...cyNode.data(),
          requestRate: node.requestRate,
          errorRate: node.errorRate,
        });
      }
    });

    // Forçar re-renderização das labels
    if (cyInstance.current) {
      cyInstance.current.style().update();
    }
  };

  const initializeGraph = () => {
    if (!cyRef.current || !graphData) return;

    // Se já existe e não está destruído, apenas atualizar dados
    if (cyInstance.current && cyInstance.current.container()) {
      updateGraphData();
      return;
    }

    // Limpar instância anterior se existir
    if (cyInstance.current) {
      try {
        cyInstance.current.destroy();
      } catch (e) {
        // Ignorar erro se já foi destruído
      }
      cyInstance.current = null;
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
            'label': function(ele: any) {
              const labels = [];
              const rate = ele.data('requestRate');
              const responseTime = ele.data('responseTime');
              const protocol = ele.data('protocol');

              if (displayOptions.showEdgeLabels.trafficRate && rate) {
                labels.push(`🔄 ${rate}`);
              }
              if (displayOptions.showEdgeLabels.responseTime && responseTime) {
                labels.push(`⏱️ ${responseTime}`);
              }
              if (displayOptions.showEdgeLabels.throughput && rate) {
                labels.push(`📊 ${rate}`);
              }
              
              // Se nenhum dado disponível, mostrar protocolo
              if (labels.length === 0 && protocol) {
                return protocol.toUpperCase();
              }
              
              return labels.join('  •  ');
            },
            'font-size': '11px',
            'color': '#1f2937',
            'text-background-color': '#ffffff',
            'text-background-opacity': 0.95,
            'text-background-padding': '4px',
            'text-background-shape': 'roundrectangle',
            'text-border-color': '#e5e7eb',
            'text-border-width': 1,
            'text-border-opacity': 0.8,
            'text-margin-y': -12,
            'text-wrap': 'none',
            'line-style': displayOptions.show.trafficAnimation ? 'dashed' : 'solid',
            'line-dash-pattern': displayOptions.show.trafficAnimation ? [6, 3] : [1, 0],
            'line-dash-offset': 0,
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

  const toggleFullscreen = () => {
    const wasFullscreen = isFullscreen;
    setIsFullscreen(!isFullscreen);
    
    // Dar tempo para o DOM atualizar antes de redimensionar
    setTimeout(() => {
      if (cyInstance.current) {
        cyInstance.current.resize();
        cyInstance.current.center();
        
        // Se estava saindo do fullscreen, forçar um refresh da renderização
        if (wasFullscreen) {
          cyInstance.current.fit(undefined, 30);
        }
      }
    }, 150);
  };

  return (
    <>
      {!isFullscreen ? (
        <div className="space-y-4">
          {/* Controles */}
          <Card>
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <CardTitle>Service Mesh Topology</CardTitle>
                <Button
                  onClick={toggleFullscreen}
                  variant="outline"
                  size="sm"
                  disabled={!graphData}
                >
                  <Maximize2 className="mr-2 h-4 w-4" /> Tela Cheia
                </Button>
              </div>
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
                onClick={() => loadServiceGraph(false)}
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

            {/* Auto-Refresh Controls */}
            <div className="flex items-center gap-2 pt-2">
              <div className="flex-1">
                <label className="text-xs font-medium text-muted-foreground mb-1 block">Auto-Refresh</label>
                <Select
                  value={autoRefreshInterval.toString()}
                  onValueChange={(value) => setAutoRefreshInterval(Number(value))}
                >
                  <SelectTrigger className="h-8 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">Off</SelectItem>
                    <SelectItem value="15000">Every 15s</SelectItem>
                    <SelectItem value="30000">Every 30s</SelectItem>
                    <SelectItem value="60000">Every 1m</SelectItem>
                    <SelectItem value="300000">Every 5m</SelectItem>
                    <SelectItem value="600000">Every 10m</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              {lastRefresh && (
                <div className="flex-1">
                  <label className="text-xs font-medium text-muted-foreground mb-1 block">Last Refresh</label>
                  <div className="flex items-center gap-1 text-xs text-muted-foreground h-8 px-2 bg-muted rounded-md">
                    <Clock className="h-3 w-3" />
                    <span>
                      {new Date().getTime() - lastRefresh.getTime() < 60000
                        ? `${Math.floor((new Date().getTime() - lastRefresh.getTime()) / 1000)}s ago`
                        : `${Math.floor((new Date().getTime() - lastRefresh.getTime()) / 60000)}m ago`}
                    </span>
                  </div>
                </div>
              )}
            </div>

            {/* Display Options */}
            <Accordion type="single" collapsible className="mt-4">
              <AccordionItem value="display">
                <AccordionTrigger className="text-sm font-medium">Display</AccordionTrigger>
                <AccordionContent>
                  <div className="space-y-4 pt-2 max-h-[400px] overflow-y-auto pr-2">
                    {/* Show Edge Labels */}
                    <div className="space-y-2">
                      <div className="text-xs font-semibold text-muted-foreground">Show Edge Labels</div>
                      
                      <div className="space-y-1.5">
                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="responseTime"
                            checked={displayOptions.showEdgeLabels.responseTime}
                            onCheckedChange={(checked) => 
                              setDisplayOptions(prev => ({
                                ...prev,
                                showEdgeLabels: { ...prev.showEdgeLabels, responseTime: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="responseTime" className="text-sm font-normal cursor-pointer">
                            Response Time
                          </Label>
                        </div>

                        {displayOptions.showEdgeLabels.responseTime && (
                          <RadioGroup
                            value={displayOptions.showEdgeLabels.responseTimeType}
                            onValueChange={(value) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                showEdgeLabels: { ...prev.showEdgeLabels, responseTimeType: value }
                              }))
                            }
                            className="ml-6 space-y-1"
                          >
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="average" id="average" />
                              <Label htmlFor="average" className="text-sm font-normal cursor-pointer">Average</Label>
                            </div>
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="median" id="median" />
                              <Label htmlFor="median" className="text-sm font-normal cursor-pointer">Median</Label>
                            </div>
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="95th" id="95th" />
                              <Label htmlFor="95th" className="text-sm font-normal cursor-pointer">95th Percentile</Label>
                            </div>
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="99th" id="99th" />
                              <Label htmlFor="99th" className="text-sm font-normal cursor-pointer">99th Percentile</Label>
                            </div>
                          </RadioGroup>
                        )}

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="throughput"
                            checked={displayOptions.showEdgeLabels.throughput}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                showEdgeLabels: { ...prev.showEdgeLabels, throughput: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="throughput" className="text-sm font-normal cursor-pointer">Throughput</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="trafficDistribution"
                            checked={displayOptions.showEdgeLabels.trafficDistribution}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                showEdgeLabels: { ...prev.showEdgeLabels, trafficDistribution: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="trafficDistribution" className="text-sm font-normal cursor-pointer">Traffic Distribution</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="trafficRate"
                            checked={displayOptions.showEdgeLabels.trafficRate}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                showEdgeLabels: { ...prev.showEdgeLabels, trafficRate: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="trafficRate" className="text-sm font-normal cursor-pointer">Traffic Rate</Label>
                        </div>
                      </div>
                    </div>

                    {/* Show */}
                    <div className="space-y-2 border-t pt-2">
                      <div className="text-xs font-semibold text-muted-foreground">Show</div>
                      
                      <div className="grid grid-cols-2 gap-2">
                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="clusterBoxes"
                            checked={displayOptions.show.clusterBoxes}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                show: { ...prev.show, clusterBoxes: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="clusterBoxes" className="text-sm font-normal cursor-pointer">Cluster Boxes</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="namespaceBoxes"
                            checked={displayOptions.show.namespaceBoxes}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                show: { ...prev.show, namespaceBoxes: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="namespaceBoxes" className="text-sm font-normal cursor-pointer">Namespace Boxes</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="compressedHide"
                            checked={displayOptions.show.compressedHide}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                show: { ...prev.show, compressedHide: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="compressedHide" className="text-sm font-normal cursor-pointer">Compressed Hide</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="idleEdges"
                            checked={displayOptions.show.idleEdges}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                show: { ...prev.show, idleEdges: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="idleEdges" className="text-sm font-normal cursor-pointer">Idle Edges</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="idleNodes"
                            checked={displayOptions.show.idleNodes}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                show: { ...prev.show, idleNodes: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="idleNodes" className="text-sm font-normal cursor-pointer">Idle Nodes</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="operationNodes"
                            checked={displayOptions.show.operationNodes}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                show: { ...prev.show, operationNodes: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="operationNodes" className="text-sm font-normal cursor-pointer">Operation Nodes</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="rank"
                            checked={displayOptions.show.rank}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                show: { ...prev.show, rank: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="rank" className="text-sm font-normal cursor-pointer">Rank</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="serviceNodes"
                            checked={displayOptions.show.serviceNodes}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                show: { ...prev.show, serviceNodes: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="serviceNodes" className="text-sm font-normal cursor-pointer">Service Nodes</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="trafficAnimation"
                            checked={displayOptions.show.trafficAnimation}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                show: { ...prev.show, trafficAnimation: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="trafficAnimation" className="text-sm font-normal cursor-pointer">Traffic Animation</Label>
                        </div>
                      </div>
                    </div>

                    {/* Show Badges */}
                    <div className="space-y-2 border-t pt-2">
                      <div className="text-xs font-semibold text-muted-foreground">Show Badges</div>
                      
                      <div className="space-y-1.5">
                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="missingSidecars"
                            checked={displayOptions.showBadges.missingSidecars}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                showBadges: { ...prev.showBadges, missingSidecars: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="missingSidecars" className="text-sm font-normal cursor-pointer">Missing Sidecars</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="security"
                            checked={displayOptions.showBadges.security}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                showBadges: { ...prev.showBadges, security: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="security" className="text-sm font-normal cursor-pointer">Security</Label>
                        </div>

                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="virtualServices"
                            checked={displayOptions.showBadges.virtualServices}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                showBadges: { ...prev.showBadges, virtualServices: !!checked }
                              }))
                            }
                          />
                          <Label htmlFor="virtualServices" className="text-sm font-normal cursor-pointer">Virtual Services</Label>
                        </div>
                      </div>
                    </div>
                  </div>
                </AccordionContent>
              </AccordionItem>
            </Accordion>
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
      ) : (
        <div className="fixed inset-0 z-50 bg-background flex flex-col">
          {/* Barra de Controles Flutuante */}
          {showFullscreenControls && (
            <div className="flex-shrink-0 p-2 bg-background/95 backdrop-blur border-b">
              <div className="flex items-center gap-2">
                <Select
                  value={filters.namespace}
                  onValueChange={(value) => setFilters(prev => ({ ...prev, namespace: value }))}
                >
                  <SelectTrigger className="w-48">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {namespaces.map(ns => (
                      <SelectItem key={ns} value={ns}>{ns}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>

                <Select
                  value={filters.duration}
                  onValueChange={(value: any) => setFilters(prev => ({ ...prev, duration: value }))}
                >
                  <SelectTrigger className="w-28">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="60s">1 min</SelectItem>
                    <SelectItem value="5m">5 min</SelectItem>
                    <SelectItem value="10m">10 min</SelectItem>
                    <SelectItem value="30m">30 min</SelectItem>
                    <SelectItem value="1h">1h</SelectItem>
                    <SelectItem value="6h">6h</SelectItem>
                    <SelectItem value="12h">12h</SelectItem>
                    <SelectItem value="24h">24h</SelectItem>
                  </SelectContent>
                </Select>

                <Select
                  value={filters.graphType}
                  onValueChange={(value: any) => setFilters(prev => ({ ...prev, graphType: value }))}
                >
                  <SelectTrigger className="w-36">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="workload">Workload</SelectItem>
                    <SelectItem value="app">App</SelectItem>
                    <SelectItem value="service">Service</SelectItem>
                    <SelectItem value="versioned_app">Versioned App</SelectItem>
                  </SelectContent>
                </Select>

                <Button
                  onClick={() => loadServiceGraph(false)}
                  disabled={loading || !filters.namespace}
                  size="sm"
                  variant="outline"
                >
                  {loading ? (
                    <Loader2 className="h-4 w-4 animate-spin" />
                  ) : (
                    <RefreshCw className="h-4 w-4" />
                  )}
                </Button>

                <div className="border-l h-6 mx-2" />

                {/* Auto-Refresh Controls */}
                <Select
                  value={autoRefreshInterval.toString()}
                  onValueChange={(value) => setAutoRefreshInterval(Number(value))}
                >
                  <SelectTrigger className="w-32 h-8">
                    <SelectValue placeholder="Auto-refresh" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">
                      <div className="flex items-center gap-2">
                        <Pause className="h-3 w-3" />
                        <span>Off</span>
                      </div>
                    </SelectItem>
                    <SelectItem value="15000">Every 15s</SelectItem>
                    <SelectItem value="30000">Every 30s</SelectItem>
                    <SelectItem value="60000">Every 1m</SelectItem>
                    <SelectItem value="300000">Every 5m</SelectItem>
                    <SelectItem value="600000">Every 10m</SelectItem>
                  </SelectContent>
                </Select>

                {lastRefresh && (
                  <Badge variant="outline" className="text-xs">
                    <Clock className="h-3 w-3 mr-1" />
                    {new Date().getTime() - lastRefresh.getTime() < 60000
                      ? `${Math.floor((new Date().getTime() - lastRefresh.getTime()) / 1000)}s ago`
                      : `${Math.floor((new Date().getTime() - lastRefresh.getTime()) / 60000)}m ago`}
                  </Badge>
                )}

                <div className="flex-1" />

                {graphData && (
                  <div className="flex items-center gap-2 text-sm">
                    <Badge variant="secondary">{graphData.nodes.length} nós</Badge>
                    <Badge variant="secondary">{graphData.edges.length} conexões</Badge>
                  </div>
                )}

                <Button
                  onClick={toggleFullscreen}
                  variant="outline"
                  size="sm"
                >
                  <Minimize2 className="mr-2 h-4 w-4" /> Sair
                </Button>
              </div>
            </div>
          )}

          {/* Grafo em Tela Cheia */}
          <div className="flex-1 relative">
            <div
              ref={cyRef}
              className="w-full h-full bg-slate-50 dark:bg-slate-900"
            />

            {/* Controles de Zoom Flutuantes */}
            <div className="absolute bottom-4 right-4 flex flex-col gap-2 z-10">
              <Button size="icon" variant="secondary" onClick={handleZoomIn}>
                <ZoomIn className="h-4 w-4" />
              </Button>
              <Button size="icon" variant="secondary" onClick={handleZoomOut}>
                <ZoomOut className="h-4 w-4" />
              </Button>
              <Button size="icon" variant="secondary" onClick={handleFitView}>
                <Maximize2 className="h-4 w-4" />
              </Button>
              <Button
                size="icon"
                variant="secondary"
                onClick={() => setShowFullscreenControls(!showFullscreenControls)}
              >
                <Settings className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
