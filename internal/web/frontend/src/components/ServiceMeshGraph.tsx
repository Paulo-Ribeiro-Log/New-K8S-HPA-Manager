import { useEffect, useRef, useState } from 'react';
import cytoscape, { Core, EdgeSingular, NodeSingular } from 'cytoscape';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Checkbox } from '@/components/ui/checkbox';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { Label } from '@/components/ui/label';
import { Loader2, RefreshCw, ZoomIn, ZoomOut, Maximize2, Minimize2, Clock, Pause, Play, Filter, Eye } from 'lucide-react';
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
  const nodePositions = useRef<Map<string, { x: number; y: number }>>(new Map());
  const [loading, setLoading] = useState(false);
  const [graphData, setGraphData] = useState<ServiceGraphResponse | null>(null);
  const [namespaces, setNamespaces] = useState<string[]>([]);
  const [istioNotAvailable, setIstioNotAvailable] = useState(false);
  const [selectedNode, setSelectedNode] = useState<NodeSingular | null>(null);
  const [isFullscreen, setIsFullscreen] = useState(false);
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
    },
    traffic: {
      grpc: {
        enabled: true,
        requests: true,
        receivedMessages: false,
        sentMessages: false,
        totalMessages: false
      },
      http: {
        enabled: true,
        requests: true
      },
      tcp: {
        enabled: true,
        sentBytes: true,
        receivedBytes: false,
        totalBytes: false
      }
    }
  });

  // Sincronizar filters.cluster quando prop cluster mudar
  useEffect(() => {
    if (cluster && cluster !== filters.cluster) {
      console.log('[ServiceMesh] Cluster mudou de', filters.cluster, 'para', cluster);
      setFilters(prev => ({ ...prev, cluster: cluster, namespace: '' }));
    }
  }, [cluster]);

  // Carregar namespaces quando filters.cluster mudar
  useEffect(() => {
    if (filters.cluster) {
      // Limpar lista de namespaces e gráfico ao trocar de cluster
      setNamespaces([]);
      setGraphData(null);
      
      // Limpar posições salvas
      nodePositions.current.clear();
      
      // Destruir instância do gráfico de forma segura
      if (cyInstance.current) {
        try {
          if (cyInstance.current.container()) {
            cyInstance.current.destroy();
          }
        } catch (e) {
          console.warn('[ServiceMesh] Erro ao destruir instância ao trocar cluster:', e);
        }
        cyInstance.current = null;
      }
      
      // Carregar novos namespaces
      console.log('[ServiceMesh] Carregando namespaces do cluster:', filters.cluster);
      loadNamespaces();
    }
  }, [filters.cluster]);

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

  // Recarregar grafo quando namespace mudar (COM notificação)
  useEffect(() => {
    if (filters.namespace) {
      // Limpar posições salvas ao trocar namespace
      nodePositions.current.clear();
      
      // Limpar dados do gráfico
      setGraphData(null);
      
      // Destruir gráfico completamente
      if (cyInstance.current) {
        try {
          cyInstance.current.destroy();
        } catch (e) {
          console.warn('Erro ao destruir instância:', e);
        }
        cyInstance.current = null;
      }
      
      loadServiceGraph(false); // false = mostra toast
    }
  }, [filters.namespace]);

  // Recarregar quando duration/graphType mudam (SILENCIOSO)
  useEffect(() => {
    if (filters.namespace && graphData) {
      loadServiceGraphSilent();
    }
  }, [filters.duration, filters.graphType]);

  // Recarregar com display options alterados (SILENCIOSO)
  useEffect(() => {
    if (filters.namespace && graphData) {
      loadServiceGraphSilent();
    }
  }, [displayOptions.show.serviceNodes, displayOptions.show.idleEdges, displayOptions.show.idleNodes]);

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
  }, [autoRefreshInterval, filters.namespace]);

  // Controlar animação de tráfego com velocidade variável baseada em requestRate
  useEffect(() => {
    // Limpar animação anterior
    if (animationIntervalRef.current) {
      clearInterval(animationIntervalRef.current);
      animationIntervalRef.current = null;
    }

    // Iniciar nova animação se habilitada
    if (displayOptions.show.trafficAnimation && cyInstance.current) {
      animationIntervalRef.current = setInterval(() => {
        if (cyInstance.current) {
          // Animar cada edge individualmente baseado em seu requestRate
          cyInstance.current.edges().forEach((edge: any) => {
            const requestRate = parseFloat(edge.data('requestRate') || '0');
            // Calcular velocidade: quanto maior o rate, mais rápido
            // Base speed: 0.5, max speed: 5.0 (para rates > 1000)
            const speed = Math.min(5.0, Math.max(0.5, requestRate / 200));
            const currentOffset = parseFloat(edge.style('line-dash-offset') || '0');
            edge.style('line-dash-offset', currentOffset - speed);
          });
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
      console.log('[ServiceMesh] Buscando namespaces do cluster:', filters.cluster);
      const response = await apiClient.getServiceMeshNamespaces(filters.cluster);
      console.log('[ServiceMesh] Namespaces encontrados:', response.namespaces);
      
      // Verificar se Istio está disponível
      if (!response.namespaces || response.namespaces.length === 0) {
        console.log('[ServiceMesh] Nenhum namespace encontrado - Istio provavelmente não disponível');
        setIstioNotAvailable(true);
        setNamespaces([]);
        toast.error('Istio/Kiali não está disponível neste cluster');
        return;
      }
      
      setIstioNotAvailable(false);
      setNamespaces(response.namespaces);
      
      if (response.namespaces.length > 0 && !filters.namespace) {
        setFilters(prev => ({ ...prev, namespace: response.namespaces[0] }));
      }
    } catch (error) {
      console.error('[ServiceMesh] Erro ao carregar namespaces:', error);
      const errorMsg = error instanceof Error ? error.message : 'Erro desconhecido';
      
      // Detectar se é erro do Istio
      if (errorMsg.toLowerCase().includes('kiali') || 
          errorMsg.toLowerCase().includes('service unavailable') ||
          errorMsg.toLowerCase().includes('503') ||
          errorMsg.toLowerCase().includes('istio')) {
        setIstioNotAvailable(true);
        toast.error('Istio/Kiali não está disponível neste cluster');
      } else {
        toast.error(`Erro ao carregar namespaces: ${errorMsg}`);
      }
      setNamespaces([]);
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
      console.log('[ServiceMesh] Dados recebidos do backend:', {
        nodes: data.nodes.length,
        edges: data.edges.length,
        nodesList: data.nodes.map(n => n.label || n.id)
      });
      setGraphData(data);
      setLastRefresh(new Date());
      setIstioNotAvailable(false);
      
      if (!silent) {
        toast.success(`Grafo atualizado: ${data.nodes.length} serviços, ${data.edges.length} conexões`);
      }
    } catch (error) {
      const errorMsg = error instanceof Error ? error.message : 'Erro desconhecido';
      
      console.error('[ServiceMesh] Erro capturado:', error);
      console.error('[ServiceMesh] Mensagem de erro:', errorMsg);
      
      // Detectar se o erro é devido ao Istio não estar instalado
      // Backend retorna StatusServiceUnavailable (503) com "Kiali não acessível"
      const isIstioError = errorMsg.toLowerCase().includes('kiali') || 
          errorMsg.toLowerCase().includes('service unavailable') ||
          errorMsg.toLowerCase().includes('503') ||
          errorMsg.includes('connection refused') || 
          errorMsg.includes('não encontrado') || 
          errorMsg.includes('not found') ||
          errorMsg.toLowerCase().includes('istio');
      
      console.log('[ServiceMesh] É erro de Istio?', isIstioError);
      
      if (isIstioError) {
        console.log('[ServiceMesh] Setando istioNotAvailable = true');
        setIstioNotAvailable(true);
        if (!silent) {
          toast.error('Istio/Kiali não está disponível neste cluster');
        }
      } else {
        setIstioNotAvailable(false);
        if (!silent) {
          toast.error(`Erro ao carregar service mesh: ${errorMsg}`);
        }
      }
    } finally {
      setLoading(false);
    }
  };

  const loadServiceGraphSilent = async () => {
    if (!filters.namespace) return;

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
      
      console.log('[ServiceMesh] Refresh silencioso - dados recebidos:', {
        nodes: data.nodes.length,
        edges: data.edges.length
      });
      
      // Atualizar apenas os dados sem recriar o gráfico
      if (cyInstance.current && cyInstance.current.container()) {
        updateGraphDataFromResponse(data);
        setLastRefresh(new Date());
      } else {
        // Se não existe gráfico, criar um novo
        setGraphData(data);
        setLastRefresh(new Date());
      }
    } catch (error) {
      console.error('[ServiceMesh] Erro no refresh silencioso:', error);
    }
  };

  const updateGraphDataFromResponse = (data: ServiceGraphResponse) => {
    if (!cyInstance.current) return;

    console.log('[ServiceMesh] Atualizando gráfico com novos dados');
    
    const existingNodeIds = new Set(cyInstance.current.nodes().map((n: any) => n.id()));
    const existingEdgeIds = new Set(cyInstance.current.edges().map((e: any) => e.id()));
    
    const newNodeIds = new Set(data.nodes.map(n => n.id));
    const newEdgeIds = new Set(data.edges.map(e => e.id));

    // 1. Atualizar nodes existentes
    data.nodes.forEach(node => {
      const cyNode = cyInstance.current?.getElementById(node.id);
      if (cyNode && cyNode.length > 0) {
        cyNode.data({
          ...cyNode.data(),
          requestRate: node.requestRate,
          errorRate: node.errorRate,
        });
      } else {
        // Adicionar novo node
        cyInstance.current?.add({
          data: {
            id: node.id,
            label: node.label,
            type: node.type,
            nodeType: node.type,
            namespace: node.namespace,
            workload: node.workload,
            app: node.app,
            version: node.version,
            service: node.service,
            isRoot: node.isRoot,
            isInaccessible: node.isInaccessible,
            isOutside: node.isOutside,
            requestRate: node.requestRate,
            errorRate: node.errorRate,
          },
        });
        console.log('[ServiceMesh] Novo node adicionado:', node.id);
      }
    });

    // 2. Atualizar edges existentes
    data.edges.forEach(edge => {
      const cyEdge = cyInstance.current?.getElementById(edge.id);
      if (cyEdge && cyEdge.length > 0) {
        cyEdge.data({
          ...cyEdge.data(),
          requestRate: edge.requestRate,
          responseTime: edge.responseTime,
          errorRate: edge.errorRate,
          protocol: edge.protocol,
        });
      } else {
        // Adicionar novo edge
        cyInstance.current?.add({
          data: {
            id: edge.id,
            source: edge.source,
            target: edge.target,
            protocol: edge.protocol,
            requestRate: edge.requestRate,
            responseTime: edge.responseTime,
            errorRate: edge.errorRate,
          },
        });
        console.log('[ServiceMesh] Novo edge adicionado:', edge.id);
      }
    });

    // 3. Remover nodes que não existem mais nos dados
    existingNodeIds.forEach(nodeId => {
      if (!newNodeIds.has(nodeId)) {
        const node = cyInstance.current?.getElementById(nodeId);
        if (node) {
          console.log('[ServiceMesh] Removendo node:', nodeId);
          node.remove();
          nodePositions.current.delete(nodeId);
        }
      }
    });

    // 4. Remover edges que não existem mais nos dados
    existingEdgeIds.forEach(edgeId => {
      if (!newEdgeIds.has(edgeId)) {
        const edge = cyInstance.current?.getElementById(edgeId);
        if (edge) {
          console.log('[ServiceMesh] Removendo edge:', edgeId);
          edge.remove();
        }
      }
    });

    // 5. Fazer layout apenas para nodes novos (sem posição salva)
    const newNodes = cyInstance.current.nodes().filter((node: any) => {
      return !nodePositions.current.has(node.id());
    });
    
    if (newNodes.length > 0) {
      console.log('[ServiceMesh] Fazendo layout para', newNodes.length, 'novos nodes');
      newNodes.layout({ name: 'cose', animate: false, fit: false }).run();
    }

    // 6. Forçar re-renderização
    cyInstance.current.style().update();
  };

  const initializeGraph = () => {
    if (!cyRef.current || !graphData) return;

    console.log('[ServiceMesh] Inicializando gráfico com', graphData.nodes.length, 'nodes e', graphData.edges.length, 'edges');

    // Se já existe instância válida, apenas atualizar elementos
    if (cyInstance.current && cyInstance.current.container()) {
      console.log('[ServiceMesh] Atualizando gráfico existente');
      
      // Salvar posições atuais dos nodes
      cyInstance.current.nodes().forEach((node: any) => {
        const pos = node.position();
        nodePositions.current.set(node.id(), { x: pos.x, y: pos.y });
      });
      
      // Remover elementos antigos
      cyInstance.current.elements().remove();
      
      // Adicionar novos elementos
      const elements = [
        ...graphData.nodes.map(node => {
          const savedPos = nodePositions.current.get(node.id);
          return {
            data: {
              id: node.id,
              label: node.label,
              type: node.type,
              nodeType: node.type,
              namespace: node.namespace,
              workload: node.workload,
              app: node.app,
              version: node.version,
              service: node.service,
              isRoot: node.isRoot,
              isInaccessible: node.isInaccessible,
              isOutside: node.isOutside,
              requestRate: node.requestRate,
              errorRate: node.errorRate,
            },
            // Restaurar posição salva se existir
            ...(savedPos ? { position: savedPos } : {}),
          };
        }),
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
      
      cyInstance.current.add(elements);
      
      // Apenas fazer layout para nodes novos (sem posição salva)
      const newNodes = cyInstance.current.nodes().filter((node: any) => {
        return !nodePositions.current.has(node.id());
      });
      
      if (newNodes.length > 0) {
        // Layout apenas para nodes novos
        newNodes.layout({ name: 'cose', animate: false }).run();
      }
      
      return;
    }

    // Limpar instância anterior se existir (mas não tem container)
    if (cyInstance.current) {
      try {
        cyInstance.current.destroy();
      } catch (e) {
        console.warn('Erro ao destruir instância anterior:', e);
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
          nodeType: node.type,
          namespace: node.namespace,
          workload: node.workload,
          app: node.app,
          version: node.version,
          service: node.service,
          isRoot: node.isRoot,
          isInaccessible: node.isInaccessible,
          isOutside: node.isOutside,
          requestRate: node.requestRate,
          errorRate: node.errorRate,
          hasSidecar: node.hasSidecar,
          hasVirtualService: node.hasVirtualService,
          mtlsEnabled: node.mtlsEnabled,
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
            'label': function(ele: any) {
              const workload = ele.data('workload') || ele.data('service') || ele.data('app');
              const version = ele.data('version');
              const nodeType = ele.data('nodeType');
              const hasSidecar = ele.data('hasSidecar');
              const hasVirtualService = ele.data('hasVirtualService');
              const mtlsEnabled = ele.data('mtlsEnabled');
              
              let label = workload || 'unknown';
              
              // Para workloads, mostrar nome e versão em linhas separadas
              if (nodeType === 'workload' && version) {
                label = `${workload}\nv${version.replace(/-/g, '.')}`;
              }
              
              // Adicionar badges visuais (se habilitados nas opções)
              const badges = [];
              if (displayOptions.showBadges.missingSidecars && hasSidecar === false) {
                badges.push('⚠️'); // Missing sidecar
              }
              if (displayOptions.showBadges.virtualServices && hasVirtualService) {
                badges.push('🔀'); // Virtual Service
              }
              if (displayOptions.showBadges.security && mtlsEnabled) {
                badges.push('🔒'); // mTLS
              }
              
              if (badges.length > 0) {
                label += '\n' + badges.join(' ');
              }
              
              return label;
            },
            'color': '#ffffff',
            'text-valign': 'center',
            'text-halign': 'center',
            'font-size': '11px',
            'text-wrap': 'wrap',
            'text-max-width': '80px',
            'width': '80px',
            'height': '80px',
            'border-width': '3px',
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
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
            'arrow-scale': 1.5,
            'label': function(ele: any) {
              const labels = [];
              const rate = ele.data('requestRate');
              const responseTime = ele.data('responseTime');
              const errorRate = ele.data('errorRate');
              const protocol = ele.data('protocol');

              if (displayOptions.showEdgeLabels.trafficRate && rate) {
                // Formatar como "XXX.XX req/s" (requests per second)
                const rateNum = parseFloat(rate);
                const formatted = rateNum >= 1000
                  ? `${(rateNum / 1000).toFixed(1)}k`
                  : rateNum >= 100
                  ? `${Math.round(rateNum)}`
                  : `${rateNum.toFixed(1)}`;
                labels.push(formatted + ' req/s');
              }
              
              // Mostrar taxa de erro se houver
              if (errorRate && parseFloat(errorRate) > 0) {
                const errorNum = parseFloat(errorRate);
                labels.push(`${errorNum.toFixed(1)}% err`);
              }
              
              if (displayOptions.showEdgeLabels.responseTime && responseTime) {
                labels.push(responseTime);
              }
              if (displayOptions.showEdgeLabels.throughput && rate) {
                // Throughput: converter para bps (bytes per second)
                const rateNum = parseFloat(rate);
                // Assumindo tamanho médio de request: 1KB
                const bps = rateNum * 1024;
                const formatted = bps >= 1024 * 1024
                  ? `${(bps / (1024 * 1024)).toFixed(1)} MB/s`
                  : bps >= 1024
                  ? `${(bps / 1024).toFixed(1)} KB/s`
                  : `${Math.round(bps)} B/s`;
                labels.push(formatted);
              }

              // Se nenhum dado disponível, mostrar protocolo
              if (labels.length === 0 && protocol) {
                return protocol.toUpperCase();
              }

              return labels.join('\n');
            },
            'line-color': function(ele: any) {
              const errorRate = parseFloat(ele.data('errorRate') || '0');
              const rate = parseFloat(ele.data('requestRate') || '0');
              
              // PRIORIDADE: Erros sempre aparecem primeiro
              // Vermelho para taxa de erro muito alta (>=5%)
              if (errorRate >= 5) {
                return '#ef4444'; // red-500
              }
              // Laranja para taxa de erro alta (1-5%)
              if (errorRate >= 1) {
                return '#f97316'; // orange-500
              }
              // Amarelo para taxa de erro média (0.1-1%)
              if (errorRate >= 0.1) {
                return '#eab308'; // yellow-500
              }
              // Vermelho claro para qualquer erro (>0%)
              if (errorRate > 0) {
                return '#fca5a5'; // red-300
              }
              
              // Sem erros: cores baseadas em tráfego e protocolo
              // Verde para tráfego alto e sem erros
              if (rate > 100) {
                return '#10b981'; // green-500
              }
              // Azul para HTTP
              if (ele.data('protocol') === 'http') {
                return '#3b82f6'; // blue-500
              }
              // Cinza padrão
              return '#9ca3af'; // gray-400
            },
            'target-arrow-color': function(ele: any) {
              const errorRate = parseFloat(ele.data('errorRate') || '0');
              const rate = parseFloat(ele.data('requestRate') || '0');
              
              // Mesma lógica de cores da linha
              if (errorRate >= 5) return '#ef4444';
              if (errorRate >= 1) return '#f97316';
              if (errorRate >= 0.1) return '#eab308';
              if (errorRate > 0) return '#fca5a5';
              if (rate > 100) return '#10b981';
              if (ele.data('protocol') === 'http') return '#3b82f6';
              return '#9ca3af';
            },
            'font-size': '10px',
            'color': '#f8fafc',
            'text-background-color': '#1e293b',
            'text-background-opacity': 0.95,
            'text-background-padding': '6px',
            'text-background-shape': 'roundrectangle',
            'text-border-color': '#475569',
            'text-border-width': 1,
            'text-border-opacity': 1,
            'text-margin-y': -12,
            'text-wrap': 'wrap',
            'text-max-width': '120px',
            'text-justification': 'center',
            'line-style': displayOptions.show.trafficAnimation ? 'dashed' : 'solid',
            'line-dash-pattern': displayOptions.show.trafficAnimation ? [6, 3] : [1, 0],
            'line-dash-offset': 0,
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
            <CardContent className="space-y-2 py-3">
          <div className="flex flex-wrap items-end gap-2">
            <div className="min-w-[130px] max-w-[160px]">
              <label className="text-xs font-medium mb-1 block text-muted-foreground">Namespace</label>
              <Select
                value={filters.namespace}
                onValueChange={(value) => setFilters(prev => ({ ...prev, namespace: value }))}
              >
                <SelectTrigger className="h-8 text-sm">
                  <SelectValue placeholder="Selecione" />
                </SelectTrigger>
                <SelectContent>
                  {namespaces.map(ns => (
                    <SelectItem key={ns} value={ns}>{ns}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="min-w-[100px] max-w-[120px]">
              <label className="text-xs font-medium mb-1 block text-muted-foreground">Duração</label>
              <Select
                value={filters.duration}
                onValueChange={(value: any) => setFilters(prev => ({ ...prev, duration: value }))}
              >
                <SelectTrigger className="h-8 text-sm">
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

            <div className="min-w-[100px] max-w-[120px]">
              <label className="text-xs font-medium mb-1 block text-muted-foreground">Tipo de Grafo</label>
              <Select
                value={filters.graphType}
                onValueChange={(value: any) => setFilters(prev => ({ ...prev, graphType: value }))}
              >
                <SelectTrigger className="h-8 text-sm">
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

            <div className="flex items-end">
              <Button
                onClick={() => loadServiceGraph(false)}
                disabled={loading || !filters.namespace}
                size="sm"
                className="h-8 text-xs px-3 min-w-[90px] max-w-[100px]"
              >
                {loading ? (
                  <><Loader2 className="mr-2 h-3 w-3 animate-spin" /> Carregando...</>
                ) : (
                  <><RefreshCw className="mr-2 h-3 w-3" /> Atualizar</>
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

            {/* Display Options - Popovers */}
            <div className="flex gap-2 mt-4">
              {/* Traffic Popover */}
              <Popover>
                <PopoverTrigger asChild>
                  <Button variant="outline" size="sm">
                    <Filter className="mr-2 h-4 w-4" />
                    Traffic
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-80" align="start">
                  <div className="space-y-4 max-h-[400px] overflow-y-auto pr-2">
                    <h4 className="font-semibold text-sm mb-3">Traffic Options</h4>
                    
                    {/* gRPC */}
                    <div className="space-y-2">
                      <div className="flex items-center space-x-2">
                        <Checkbox
                          id="grpc"
                          checked={displayOptions.traffic.grpc.enabled}
                          onCheckedChange={(checked) =>
                            setDisplayOptions(prev => ({
                              ...prev,
                              traffic: {
                                ...prev.traffic,
                                grpc: { ...prev.traffic.grpc, enabled: !!checked }
                              }
                            }))
                          }
                        />
                        <Label htmlFor="grpc" className="text-sm font-medium cursor-pointer">gRPC</Label>
                      </div>

                      {displayOptions.traffic.grpc.enabled && (
                        <RadioGroup
                          value={
                            displayOptions.traffic.grpc.requests ? 'requests' :
                            displayOptions.traffic.grpc.receivedMessages ? 'receivedMessages' :
                            displayOptions.traffic.grpc.sentMessages ? 'sentMessages' :
                            displayOptions.traffic.grpc.totalMessages ? 'totalMessages' : 'requests'
                          }
                          onValueChange={(value) =>
                            setDisplayOptions(prev => ({
                              ...prev,
                              traffic: {
                                ...prev.traffic,
                                grpc: {
                                  ...prev.traffic.grpc,
                                  requests: value === 'requests',
                                  receivedMessages: value === 'receivedMessages',
                                  sentMessages: value === 'sentMessages',
                                  totalMessages: value === 'totalMessages'
                                }
                              }
                            }))
                          }
                          className="ml-6 space-y-1"
                        >
                          <div className="flex items-center space-x-2">
                            <RadioGroupItem value="receivedMessages" id="grpc-receivedMessages" />
                            <Label htmlFor="grpc-receivedMessages" className="text-sm font-normal cursor-pointer">Received Messages</Label>
                          </div>
                          <div className="flex items-center space-x-2">
                            <RadioGroupItem value="requests" id="grpc-requests" />
                            <Label htmlFor="grpc-requests" className="text-sm font-normal cursor-pointer">Requests</Label>
                          </div>
                          <div className="flex items-center space-x-2">
                            <RadioGroupItem value="sentMessages" id="grpc-sentMessages" />
                            <Label htmlFor="grpc-sentMessages" className="text-sm font-normal cursor-pointer">Sent Messages</Label>
                          </div>
                          <div className="flex items-center space-x-2">
                            <RadioGroupItem value="totalMessages" id="grpc-totalMessages" />
                            <Label htmlFor="grpc-totalMessages" className="text-sm font-normal cursor-pointer">Total Messages</Label>
                          </div>
                        </RadioGroup>
                      )}
                    </div>

                    {/* HTTP */}
                    <div className="space-y-2 border-t pt-2">
                      <div className="flex items-center space-x-2">
                        <Checkbox
                          id="http"
                          checked={displayOptions.traffic.http.enabled}
                          onCheckedChange={(checked) =>
                            setDisplayOptions(prev => ({
                              ...prev,
                              traffic: {
                                ...prev.traffic,
                                http: { ...prev.traffic.http, enabled: !!checked }
                              }
                            }))
                          }
                        />
                        <Label htmlFor="http" className="text-sm font-medium cursor-pointer">Http</Label>
                      </div>

                      {displayOptions.traffic.http.enabled && (
                        <RadioGroup
                          value="requests"
                          className="ml-6 space-y-1"
                        >
                          <div className="flex items-center space-x-2">
                            <RadioGroupItem value="requests" id="http-requests" />
                            <Label htmlFor="http-requests" className="text-sm font-normal cursor-pointer">Requests</Label>
                          </div>
                        </RadioGroup>
                      )}
                    </div>

                    {/* TCP */}
                    <div className="space-y-2 border-t pt-2">
                      <div className="flex items-center space-x-2">
                        <Checkbox
                          id="tcp"
                          checked={displayOptions.traffic.tcp.enabled}
                          onCheckedChange={(checked) =>
                            setDisplayOptions(prev => ({
                              ...prev,
                              traffic: {
                                ...prev.traffic,
                                tcp: { ...prev.traffic.tcp, enabled: !!checked }
                              }
                            }))
                          }
                        />
                        <Label htmlFor="tcp" className="text-sm font-medium cursor-pointer">Tcp</Label>
                      </div>

                      {displayOptions.traffic.tcp.enabled && (
                        <RadioGroup
                          value={
                            displayOptions.traffic.tcp.receivedBytes ? 'receivedBytes' :
                            displayOptions.traffic.tcp.sentBytes ? 'sentBytes' :
                            displayOptions.traffic.tcp.totalBytes ? 'totalBytes' : 'sentBytes'
                          }
                          onValueChange={(value) =>
                            setDisplayOptions(prev => ({
                              ...prev,
                              traffic: {
                                ...prev.traffic,
                                tcp: {
                                  ...prev.traffic.tcp,
                                  receivedBytes: value === 'receivedBytes',
                                  sentBytes: value === 'sentBytes',
                                  totalBytes: value === 'totalBytes'
                                }
                              }
                            }))
                          }
                          className="ml-6 space-y-1"
                        >
                          <div className="flex items-center space-x-2">
                            <RadioGroupItem value="receivedBytes" id="tcp-receivedBytes" />
                            <Label htmlFor="tcp-receivedBytes" className="text-sm font-normal cursor-pointer">Received Bytes</Label>
                          </div>
                          <div className="flex items-center space-x-2">
                            <RadioGroupItem value="sentBytes" id="tcp-sentBytes" />
                            <Label htmlFor="tcp-sentBytes" className="text-sm font-normal cursor-pointer">Sent Bytes</Label>
                          </div>
                          <div className="flex items-center space-x-2">
                            <RadioGroupItem value="totalBytes" id="tcp-totalBytes" />
                            <Label htmlFor="tcp-totalBytes" className="text-sm font-normal cursor-pointer">Total Bytes</Label>
                          </div>
                        </RadioGroup>
                      )}
                    </div>
                  </div>
                </PopoverContent>
              </Popover>

              {/* Display Popover */}
              <Popover>
                <PopoverTrigger asChild>
                  <Button variant="outline" size="sm">
                    <Eye className="mr-2 h-4 w-4" />
                    Display
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-80" align="start">
                  <div className="space-y-4 max-h-[400px] overflow-y-auto pr-2">
                    <h4 className="font-semibold text-sm mb-3">Display Options</h4>
                    
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
                          <Label htmlFor="missingSidecars" className="text-sm font-normal cursor-pointer">⚠️ Missing Sidecars</Label>
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
                          <Label htmlFor="security" className="text-sm font-normal cursor-pointer">🔒 Security (mTLS)</Label>
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
                          <Label htmlFor="virtualServices" className="text-sm font-normal cursor-pointer">🔀 Virtual Services</Label>
                        </div>
                      </div>
                    </div>
                  </div>
                </PopoverContent>
              </Popover>
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
            {istioNotAvailable ? (
              <div 
                className="w-full bg-slate-50 dark:bg-slate-900 border rounded-lg flex items-center justify-center"
                style={{ height: '600px' }}
              >
                <div className="text-center space-y-4 max-w-md px-6">
                  <div className="text-6xl">🚫</div>
                  <h3 className="text-xl font-semibold text-slate-700 dark:text-slate-300">
                    Istio não disponível
                  </h3>
                  <p className="text-sm text-muted-foreground">
                    O Istio Service Mesh não está instalado ou não está acessível neste cluster.
                  </p>
                  <p className="text-xs text-muted-foreground">
                    Para visualizar a topologia do Service Mesh, é necessário ter o Istio e o Kiali instalados no cluster <strong>{cluster}</strong>.
                  </p>
                  <Button
                    onClick={() => loadServiceGraph(false)}
                    disabled={loading}
                    variant="outline"
                    size="sm"
                    className="mt-4"
                  >
                    {loading ? (
                      <><Loader2 className="mr-2 h-4 w-4 animate-spin" /> Tentando reconectar...</>
                    ) : (
                      <><RefreshCw className="mr-2 h-4 w-4" /> Tentar novamente</>
                    )}
                  </Button>
                </div>
              </div>
            ) : (
              <div
                ref={cyRef}
                className="w-full bg-slate-50 dark:bg-slate-900 border rounded-lg"
                style={{ height: '600px' }}
              />
            )}
            
            {/* Controles de Zoom */}
            {!istioNotAvailable && (
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
            )}
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

                {/* Traffic Popover */}
                <Popover>
                  <PopoverTrigger asChild>
                    <Button variant="outline" size="sm">
                      <Filter className="mr-2 h-4 w-4" />
                      Traffic
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent className="w-80" align="end">
                    <div className="space-y-4 max-h-[400px] overflow-y-auto pr-2">
                      <h4 className="font-semibold text-sm mb-3">Traffic Options</h4>
                      
                      {/* gRPC */}
                      <div className="space-y-2">
                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="grpc-fullscreen"
                            checked={displayOptions.traffic.grpc.enabled}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                traffic: {
                                  ...prev.traffic,
                                  grpc: { ...prev.traffic.grpc, enabled: !!checked }
                                }
                              }))
                            }
                          />
                          <Label htmlFor="grpc-fullscreen" className="text-sm font-medium cursor-pointer">gRPC</Label>
                        </div>

                        {displayOptions.traffic.grpc.enabled && (
                          <RadioGroup
                            value={
                              displayOptions.traffic.grpc.requests ? 'requests' :
                              displayOptions.traffic.grpc.receivedMessages ? 'receivedMessages' :
                              displayOptions.traffic.grpc.sentMessages ? 'sentMessages' :
                              displayOptions.traffic.grpc.totalMessages ? 'totalMessages' : 'requests'
                            }
                            onValueChange={(value) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                traffic: {
                                  ...prev.traffic,
                                  grpc: {
                                    ...prev.traffic.grpc,
                                    requests: value === 'requests',
                                    receivedMessages: value === 'receivedMessages',
                                    sentMessages: value === 'sentMessages',
                                    totalMessages: value === 'totalMessages'
                                  }
                                }
                              }))
                            }
                            className="ml-6 space-y-1"
                          >
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="receivedMessages" id="grpc-receivedMessages-fullscreen" />
                              <Label htmlFor="grpc-receivedMessages-fullscreen" className="text-sm font-normal cursor-pointer">Received Messages</Label>
                            </div>
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="requests" id="grpc-requests-fullscreen" />
                              <Label htmlFor="grpc-requests-fullscreen" className="text-sm font-normal cursor-pointer">Requests</Label>
                            </div>
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="sentMessages" id="grpc-sentMessages-fullscreen" />
                              <Label htmlFor="grpc-sentMessages-fullscreen" className="text-sm font-normal cursor-pointer">Sent Messages</Label>
                            </div>
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="totalMessages" id="grpc-totalMessages-fullscreen" />
                              <Label htmlFor="grpc-totalMessages-fullscreen" className="text-sm font-normal cursor-pointer">Total Messages</Label>
                            </div>
                          </RadioGroup>
                        )}
                      </div>

                      {/* HTTP */}
                      <div className="space-y-2 border-t pt-2">
                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="http-fullscreen"
                            checked={displayOptions.traffic.http.enabled}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                traffic: {
                                  ...prev.traffic,
                                  http: { ...prev.traffic.http, enabled: !!checked }
                                }
                              }))
                            }
                          />
                          <Label htmlFor="http-fullscreen" className="text-sm font-medium cursor-pointer">Http</Label>
                        </div>

                        {displayOptions.traffic.http.enabled && (
                          <RadioGroup
                            value="requests"
                            className="ml-6 space-y-1"
                          >
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="requests" id="http-requests-fullscreen" />
                              <Label htmlFor="http-requests-fullscreen" className="text-sm font-normal cursor-pointer">Requests</Label>
                            </div>
                          </RadioGroup>
                        )}
                      </div>

                      {/* TCP */}
                      <div className="space-y-2 border-t pt-2">
                        <div className="flex items-center space-x-2">
                          <Checkbox
                            id="tcp-fullscreen"
                            checked={displayOptions.traffic.tcp.enabled}
                            onCheckedChange={(checked) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                traffic: {
                                  ...prev.traffic,
                                  tcp: { ...prev.traffic.tcp, enabled: !!checked }
                                }
                              }))
                            }
                          />
                          <Label htmlFor="tcp-fullscreen" className="text-sm font-medium cursor-pointer">Tcp</Label>
                        </div>

                        {displayOptions.traffic.tcp.enabled && (
                          <RadioGroup
                            value={
                              displayOptions.traffic.tcp.receivedBytes ? 'receivedBytes' :
                              displayOptions.traffic.tcp.sentBytes ? 'sentBytes' :
                              displayOptions.traffic.tcp.totalBytes ? 'totalBytes' : 'sentBytes'
                            }
                            onValueChange={(value) =>
                              setDisplayOptions(prev => ({
                                ...prev,
                                traffic: {
                                  ...prev.traffic,
                                  tcp: {
                                    ...prev.traffic.tcp,
                                    receivedBytes: value === 'receivedBytes',
                                    sentBytes: value === 'sentBytes',
                                    totalBytes: value === 'totalBytes'
                                  }
                                }
                              }))
                            }
                            className="ml-6 space-y-1"
                          >
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="receivedBytes" id="tcp-receivedBytes-fullscreen" />
                              <Label htmlFor="tcp-receivedBytes-fullscreen" className="text-sm font-normal cursor-pointer">Received Bytes</Label>
                            </div>
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="sentBytes" id="tcp-sentBytes-fullscreen" />
                              <Label htmlFor="tcp-sentBytes-fullscreen" className="text-sm font-normal cursor-pointer">Sent Bytes</Label>
                            </div>
                            <div className="flex items-center space-x-2">
                              <RadioGroupItem value="totalBytes" id="tcp-totalBytes-fullscreen" />
                              <Label htmlFor="tcp-totalBytes-fullscreen" className="text-sm font-normal cursor-pointer">Total Bytes</Label>
                            </div>
                          </RadioGroup>
                        )}
                      </div>
                    </div>
                  </PopoverContent>
                </Popover>

                {/* Display Popover */}
                <Popover>
                  <PopoverTrigger asChild>
                    <Button variant="outline" size="sm">
                      <Eye className="mr-2 h-4 w-4" />
                      Display
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent className="w-80" align="end">
                    <div className="space-y-4 max-h-[400px] overflow-y-auto pr-2">
                      <h4 className="font-semibold text-sm mb-3">Display Options</h4>
                      
                      {/* Show Edge Labels */}
                      <div className="space-y-2">
                        <div className="text-xs font-semibold text-muted-foreground">Show Edge Labels</div>
                        
                        <div className="space-y-1.5">
                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="responseTime-fullscreen"
                              checked={displayOptions.showEdgeLabels.responseTime}
                              onCheckedChange={(checked) => 
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  showEdgeLabels: { ...prev.showEdgeLabels, responseTime: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="responseTime-fullscreen" className="text-sm font-normal cursor-pointer">
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
                                <RadioGroupItem value="average" id="average-fullscreen" />
                                <Label htmlFor="average-fullscreen" className="text-sm font-normal cursor-pointer">Average</Label>
                              </div>
                              <div className="flex items-center space-x-2">
                                <RadioGroupItem value="median" id="median-fullscreen" />
                                <Label htmlFor="median-fullscreen" className="text-sm font-normal cursor-pointer">Median</Label>
                              </div>
                              <div className="flex items-center space-x-2">
                                <RadioGroupItem value="95th" id="95th-fullscreen" />
                                <Label htmlFor="95th-fullscreen" className="text-sm font-normal cursor-pointer">95th Percentile</Label>
                              </div>
                              <div className="flex items-center space-x-2">
                                <RadioGroupItem value="99th" id="99th-fullscreen" />
                                <Label htmlFor="99th-fullscreen" className="text-sm font-normal cursor-pointer">99th Percentile</Label>
                              </div>
                            </RadioGroup>
                          )}

                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="throughput-fullscreen"
                              checked={displayOptions.showEdgeLabels.throughput}
                              onCheckedChange={(checked) =>
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  showEdgeLabels: { ...prev.showEdgeLabels, throughput: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="throughput-fullscreen" className="text-sm font-normal cursor-pointer">Throughput</Label>
                          </div>

                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="trafficDistribution-fullscreen"
                              checked={displayOptions.showEdgeLabels.trafficDistribution}
                              onCheckedChange={(checked) =>
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  showEdgeLabels: { ...prev.showEdgeLabels, trafficDistribution: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="trafficDistribution-fullscreen" className="text-sm font-normal cursor-pointer">Traffic Distribution</Label>
                          </div>

                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="trafficRate-fullscreen"
                              checked={displayOptions.showEdgeLabels.trafficRate}
                              onCheckedChange={(checked) =>
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  showEdgeLabels: { ...prev.showEdgeLabels, trafficRate: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="trafficRate-fullscreen" className="text-sm font-normal cursor-pointer">Traffic Rate</Label>
                          </div>
                        </div>
                      </div>

                      {/* Show */}
                      <div className="space-y-2 border-t pt-2">
                        <div className="text-xs font-semibold text-muted-foreground">Show</div>
                        
                        <div className="grid grid-cols-2 gap-2">
                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="clusterBoxes-fullscreen"
                              checked={displayOptions.show.clusterBoxes}
                              onCheckedChange={(checked) =>
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  show: { ...prev.show, clusterBoxes: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="clusterBoxes-fullscreen" className="text-sm font-normal cursor-pointer">Cluster Boxes</Label>
                          </div>

                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="namespaceBoxes-fullscreen"
                              checked={displayOptions.show.namespaceBoxes}
                              onCheckedChange={(checked) =>
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  show: { ...prev.show, namespaceBoxes: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="namespaceBoxes-fullscreen" className="text-sm font-normal cursor-pointer">Namespace Boxes</Label>
                          </div>

                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="serviceNodes-fullscreen"
                              checked={displayOptions.show.serviceNodes}
                              onCheckedChange={(checked) =>
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  show: { ...prev.show, serviceNodes: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="serviceNodes-fullscreen" className="text-sm font-normal cursor-pointer">Service Nodes</Label>
                          </div>

                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="trafficAnimation-fullscreen"
                              checked={displayOptions.show.trafficAnimation}
                              onCheckedChange={(checked) =>
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  show: { ...prev.show, trafficAnimation: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="trafficAnimation-fullscreen" className="text-sm font-normal cursor-pointer">Traffic Animation</Label>
                          </div>
                        </div>
                      </div>

                      {/* Show Badges */}
                      <div className="space-y-2 border-t pt-2">
                        <div className="text-xs font-semibold text-muted-foreground">Show Badges</div>
                        
                        <div className="space-y-1.5">
                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="missingSidecars-fullscreen"
                              checked={displayOptions.showBadges.missingSidecars}
                              onCheckedChange={(checked) =>
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  showBadges: { ...prev.showBadges, missingSidecars: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="missingSidecars-fullscreen" className="text-sm font-normal cursor-pointer">⚠️ Missing Sidecars</Label>
                          </div>

                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="security-fullscreen"
                              checked={displayOptions.showBadges.security}
                              onCheckedChange={(checked) =>
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  showBadges: { ...prev.showBadges, security: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="security-fullscreen" className="text-sm font-normal cursor-pointer">🔒 Security (mTLS)</Label>
                          </div>

                          <div className="flex items-center space-x-2">
                            <Checkbox
                              id="virtualServices-fullscreen"
                              checked={displayOptions.showBadges.virtualServices}
                              onCheckedChange={(checked) =>
                                setDisplayOptions(prev => ({
                                  ...prev,
                                  showBadges: { ...prev.showBadges, virtualServices: !!checked }
                                }))
                              }
                            />
                            <Label htmlFor="virtualServices-fullscreen" className="text-sm font-normal cursor-pointer">🔀 Virtual Services</Label>
                          </div>
                        </div>
                      </div>
                    </div>
                  </PopoverContent>
                </Popover>

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

          {/* Grafo em Tela Cheia */}
          <div className="flex-1 relative">
            {istioNotAvailable ? (
              <div className="w-full h-full bg-slate-50 dark:bg-slate-900 flex items-center justify-center">
                <div className="text-center space-y-4 max-w-md px-6">
                  <div className="text-6xl">🚫</div>
                  <h3 className="text-xl font-semibold text-slate-700 dark:text-slate-300">
                    Istio não disponível
                  </h3>
                  <p className="text-sm text-muted-foreground">
                    O Istio Service Mesh não está instalado ou não está acessível neste cluster.
                  </p>
                  <p className="text-xs text-muted-foreground">
                    Para visualizar a topologia do Service Mesh, é necessário ter o Istio e o Kiali instalados no cluster <strong>{cluster}</strong>.
                  </p>
                  <Button
                    onClick={() => loadServiceGraph(false)}
                    disabled={loading}
                    variant="outline"
                    size="sm"
                    className="mt-4"
                  >
                    {loading ? (
                      <><Loader2 className="mr-2 h-4 w-4 animate-spin" /> Tentando reconectar...</>
                    ) : (
                      <><RefreshCw className="mr-2 h-4 w-4" /> Tentar novamente</>
                    )}
                  </Button>
                </div>
              </div>
            ) : (
              <div
                ref={cyRef}
                className="w-full h-full bg-slate-50 dark:bg-slate-900"
              />
            )}

            {/* Controles de Zoom Flutuantes */}
            {!istioNotAvailable && (
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
              </div>
            )}
          </div>
        </div>
      )}
    </>
  );
}
