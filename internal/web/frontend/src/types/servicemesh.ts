// Service Mesh Graph Types (Kiali Integration)

export interface SimplifiedNode {
  id: string;
  label: string;
  type: 'workload' | 'service' | 'app' | 'unknown';
  namespace: string;
  app?: string;
  version?: string;
  isRoot?: boolean;
  isInaccessible?: boolean;
  isOutside?: boolean;
  requestRate?: string;
  errorRate?: string;
}

export interface SimplifiedEdge {
  id: string;
  source: string;
  target: string;
  protocol?: string;
  requestRate?: string;
  responseTime?: string;
  errorRate?: number;
}

export interface ServiceGraphResponse {
  nodes: SimplifiedNode[];
  edges: SimplifiedEdge[];
  timestamp: number;
  duration: number;
}

export interface ServiceMeshNamespace {
  cluster: string;
  namespaces: string[];
  count: number;
}

export interface ServiceMeshMetrics {
  [key: string]: any;
}

export interface ServiceMeshFilters {
  cluster: string;
  namespace: string;
  duration: '60s' | '5m' | '10m' | '30m' | '1h' | '6h' | '12h' | '24h';
  graphType: 'workload' | 'app' | 'service' | 'versioned_app';
  // Advanced options
  injectServiceNodes?: boolean;
  includeIdleEdges?: boolean;
  includeIdleNodes?: boolean;
  appenders?: string;
  // Display options
  showEdgeLabels?: {
    responseTime?: boolean;
    trafficDistribution?: boolean;
    trafficRate?: boolean;
    throughput?: boolean;
  };
  showNodeOptions?: {
    operationNodes?: boolean;
    serviceNodes?: boolean;
    trafficAnimation?: boolean;
  };
  showBadges?: {
    missingSidecars?: boolean;
    security?: boolean;
    virtualServices?: boolean;
  };
}
