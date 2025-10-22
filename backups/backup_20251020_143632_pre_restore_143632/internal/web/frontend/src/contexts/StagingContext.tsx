import React, { createContext, useContext, useState, useCallback } from 'react';
import type { HPA, NodePool, Session } from '../lib/api/types';

// Tipos para staging area
interface StagingHPA extends HPA {
  isModified: boolean;
  originalValues: Partial<HPA>;
}

interface StagingNodePool extends NodePool {
  isModified: boolean;
  originalValues: Partial<NodePool>;
}

interface StagingContextType {
  // Estado atual da staging area
  stagedHPAs: StagingHPA[];
  stagedNodePools: StagingNodePool[];
  
  // Métodos para HPAs
  addHPAToStaging: (hpa: HPA) => void;
  updateHPAInStaging: (cluster: string, namespace: string, name: string, updates: Partial<HPA>) => void;
  removeHPAFromStaging: (cluster: string, namespace: string, name: string) => void;
  
  // Métodos para Node Pools
  addNodePoolToStaging: (nodePool: NodePool) => void;
  updateNodePoolInStaging: (cluster: string, name: string, updates: Partial<NodePool>) => void;
  removeNodePoolFromStaging: (cluster: string, name: string) => void;
  
  // Métodos gerais
  clearStaging: () => void;
  loadFromSession: (session: Session) => void;
  hasChanges: () => boolean;
  getChangesCount: () => { hpas: number; nodePools: number; total: number };
  
  // Preview de alterações para salvar
  getSessionData: () => {
    changes: any[];
    node_pool_changes: any[];
  };

  // Métodos adicionais
  hasPendingChanges: () => boolean;
  getChangesSummary: () => {
    totalChanges: number;
    hpaChanges: number;
    nodePoolChanges: number;
    hasChanges: boolean;
    canSaveSession: boolean;
    sessionPreview: {
      changes: any[];
      node_pool_changes: any[];
    };
  };
}

const StagingContext = createContext<StagingContextType | undefined>(undefined);

interface StagingProviderProps {
  children: React.ReactNode;
}

export function StagingProvider({ children }: StagingProviderProps) {
  const [stagedHPAs, setStagedHPAs] = useState<StagingHPA[]>([]);
  const [stagedNodePools, setStagedNodePools] = useState<StagingNodePool[]>([]);

  // HPAs
  const addHPAToStaging = useCallback((hpa: HPA) => {
    setStagedHPAs(prev => {
      const existing = prev.find(
        h => h.cluster === hpa.cluster && h.namespace === hpa.namespace && h.name === hpa.name
      );
      
      if (existing) {
        return prev; // Já existe
      }
      
      const stagingHPA: StagingHPA = {
        ...hpa,
        isModified: false,
        originalValues: { ...hpa }
      };
      
      return [...prev, stagingHPA];
    });
  }, []);

  const updateHPAInStaging = useCallback((cluster: string, namespace: string, name: string, updates: Partial<HPA>) => {
    setStagedHPAs(prev => prev.map(hpa => {
      if (hpa.cluster === cluster && hpa.namespace === namespace && hpa.name === name) {
        const updatedHPA = { ...hpa, ...updates, isModified: true };
        return updatedHPA;
      }
      return hpa;
    }));
  }, []);

  const removeHPAFromStaging = useCallback((cluster: string, namespace: string, name: string) => {
    setStagedHPAs(prev => prev.filter(
      hpa => !(hpa.cluster === cluster && hpa.namespace === namespace && hpa.name === name)
    ));
  }, []);

  // Node Pools
  const addNodePoolToStaging = useCallback((nodePool: NodePool) => {
    setStagedNodePools(prev => {
      const existing = prev.find(
        np => np.cluster_name === nodePool.cluster_name && np.name === nodePool.name
      );
      
      if (existing) {
        return prev; // Já existe
      }
      
      const stagingNodePool: StagingNodePool = {
        ...nodePool,
        isModified: false,
        originalValues: { ...nodePool }
      };
      
      return [...prev, stagingNodePool];
    });
  }, []);

  const updateNodePoolInStaging = useCallback((cluster: string, name: string, updates: Partial<NodePool>) => {
    setStagedNodePools(prev => prev.map(nodePool => {
      if (nodePool.cluster_name === cluster && nodePool.name === name) {
        const updatedNodePool = { ...nodePool, ...updates, isModified: true };
        return updatedNodePool;
      }
      return nodePool;
    }));
  }, []);

  const removeNodePoolFromStaging = useCallback((cluster: string, name: string) => {
    setStagedNodePools(prev => prev.filter(
      nodePool => !(nodePool.cluster_name === cluster && nodePool.name === name)
    ));
  }, []);

  // Métodos gerais
  const clearStaging = useCallback(() => {
    setStagedHPAs([]);
    setStagedNodePools([]);
  }, []);

  const loadFromSession = useCallback((session: Session) => {
    // Converter HPAChanges para StagingHPAs
    const hpas: StagingHPA[] = session.changes.map(change => ({
      name: change.hpa_name,
      namespace: change.namespace,
      cluster: change.cluster,
      min_replicas: change.new_values?.min_replicas ?? null,
      max_replicas: change.new_values?.max_replicas ?? 0,
      current_replicas: change.original_values?.min_replicas ?? 0,
      target_cpu: change.new_values?.target_cpu ?? null,
      target_memory: change.new_values?.target_memory ?? null,
      target_cpu_request: change.new_values?.cpu_request,
      target_cpu_limit: change.new_values?.cpu_limit,
      target_memory_request: change.new_values?.memory_request,
      target_memory_limit: change.new_values?.memory_limit,
      perform_rollout: change.new_values?.perform_rollout ?? false,
      perform_daemonset_rollout: change.new_values?.perform_daemonset_rollout ?? false,
      perform_statefulset_rollout: change.new_values?.perform_statefulset_rollout ?? false,
      deployment_name: change.new_values?.deployment_name,
      isModified: true,
      originalValues: {
        min_replicas: change.original_values?.min_replicas,
        max_replicas: change.original_values?.max_replicas,
        target_cpu: change.original_values?.target_cpu,
        target_memory: change.original_values?.target_memory,
        target_cpu_request: change.original_values?.cpu_request,
        target_cpu_limit: change.original_values?.cpu_limit,
        target_memory_request: change.original_values?.memory_request,
        target_memory_limit: change.original_values?.memory_limit,
        perform_rollout: change.original_values?.perform_rollout,
        perform_daemonset_rollout: change.original_values?.perform_daemonset_rollout,
        perform_statefulset_rollout: change.original_values?.perform_statefulset_rollout,
        deployment_name: change.original_values?.deployment_name,
      }
    }));

    // Converter NodePoolChanges para StagingNodePools
    const nodePools: StagingNodePool[] = session.node_pool_changes.map(change => ({
      name: change.node_pool_name,
      cluster_name: change.cluster,
      resource_group: change.resource_group,
      subscription: change.subscription,
      vm_size: '', // Não disponível em NodePoolChange
      node_count: change.new_values.node_count,
      min_node_count: change.new_values.min_node_count,
      max_node_count: change.new_values.max_node_count,
      autoscaling_enabled: change.new_values.autoscaling_enabled,
      status: 'Succeeded', // Assumir status padrão
      is_system_pool: false, // Assumir padrão
      modified: true,
      selected: false,
      applied_count: 0,
      sequence_order: change.sequence_order,
      sequence_status: change.sequence_status,
      original_values: {
        node_count: change.original_values.node_count,
        min_node_count: change.original_values.min_node_count,
        max_node_count: change.original_values.max_node_count,
        autoscaling_enabled: change.original_values.autoscaling_enabled,
      },
      isModified: true,
      originalValues: {
        node_count: change.original_values.node_count,
        min_node_count: change.original_values.min_node_count,
        max_node_count: change.original_values.max_node_count,
        autoscaling_enabled: change.original_values.autoscaling_enabled,
      }
    }));

    setStagedHPAs(hpas);
    setStagedNodePools(nodePools);
  }, []);

  const hasChanges = useCallback(() => {
    return stagedHPAs.some(hpa => hpa.isModified) || 
           stagedNodePools.some(np => np.isModified);
  }, [stagedHPAs, stagedNodePools]);

  const getChangesCount = useCallback(() => {
    const hpas = stagedHPAs.filter(hpa => hpa.isModified).length;
    const nodePools = stagedNodePools.filter(np => np.isModified).length;
    return { hpas, nodePools, total: hpas + nodePools };
  }, [stagedHPAs, stagedNodePools]);

  const getSessionData = useCallback(() => {
    // Converter StagingHPAs para HPAChanges
    const changes = stagedHPAs
      .filter(hpa => hpa.isModified)
      .map(hpa => ({
        cluster: hpa.cluster,
        namespace: hpa.namespace,
        hpa_name: hpa.name,
        original_values: {
          min_replicas: hpa.originalValues.min_replicas,
          max_replicas: hpa.originalValues.max_replicas,
          target_cpu: hpa.originalValues.target_cpu,
          target_memory: hpa.originalValues.target_memory,
          cpu_request: hpa.originalValues.target_cpu_request,
          cpu_limit: hpa.originalValues.target_cpu_limit,
          memory_request: hpa.originalValues.target_memory_request,
          memory_limit: hpa.originalValues.target_memory_limit,
          deployment_name: hpa.originalValues.deployment_name,
          perform_rollout: hpa.originalValues.perform_rollout,
          perform_daemonset_rollout: hpa.originalValues.perform_daemonset_rollout,
          perform_statefulset_rollout: hpa.originalValues.perform_statefulset_rollout,
        },
        new_values: {
          min_replicas: hpa.min_replicas,
          max_replicas: hpa.max_replicas,
          target_cpu: hpa.target_cpu,
          target_memory: hpa.target_memory,
          cpu_request: hpa.target_cpu_request,
          cpu_limit: hpa.target_cpu_limit,
          memory_request: hpa.target_memory_request,
          memory_limit: hpa.target_memory_limit,
          deployment_name: hpa.deployment_name,
          perform_rollout: hpa.perform_rollout,
          perform_daemonset_rollout: hpa.perform_daemonset_rollout,
          perform_statefulset_rollout: hpa.perform_statefulset_rollout,
        },
        applied: false,
        rollout_triggered: false,
        daemonset_rollout_triggered: false,
        statefulset_rollout_triggered: false,
      }));

    // Converter StagingNodePools para NodePoolChanges
    const node_pool_changes = stagedNodePools
      .filter(np => np.isModified)
      .map(np => ({
        cluster: np.cluster_name,
        resource_group: np.resource_group,
        subscription: np.subscription,
        node_pool_name: np.name,
        original_values: {
          node_count: np.originalValues.node_count ?? np.original_values.node_count,
          min_node_count: np.originalValues.min_node_count ?? np.original_values.min_node_count,
          max_node_count: np.originalValues.max_node_count ?? np.original_values.max_node_count,
          autoscaling_enabled: np.originalValues.autoscaling_enabled ?? np.original_values.autoscaling_enabled,
        },
        new_values: {
          node_count: np.node_count,
          min_node_count: np.min_node_count,
          max_node_count: np.max_node_count,
          autoscaling_enabled: np.autoscaling_enabled,
        },
        applied: false,
        sequence_order: np.sequence_order,
        sequence_status: np.sequence_status,
      }));

    return { changes, node_pool_changes };
  }, [stagedHPAs, stagedNodePools]);

  // Método para verificar se há alterações pendentes
  const hasPendingChanges = useCallback(() => {
    return getChangesCount().total > 0;
  }, [getChangesCount]);

  // Método para obter estatísticas das alterações
  const getChangesSummary = useCallback(() => {
    const changesCount = getChangesCount();
    const sessionData = getSessionData();
    
    return {
      totalChanges: changesCount.total,
      hpaChanges: changesCount.hpas,
      nodePoolChanges: changesCount.nodePools,
      hasChanges: hasChanges(),
      canSaveSession: hasPendingChanges(),
      sessionPreview: sessionData,
    };
  }, [getChangesCount, getSessionData, hasChanges, hasPendingChanges]);

  const value: StagingContextType = {
    stagedHPAs,
    stagedNodePools,
    addHPAToStaging,
    updateHPAInStaging,
    removeHPAFromStaging,
    addNodePoolToStaging,
    updateNodePoolInStaging,
    removeNodePoolFromStaging,
    clearStaging,
    loadFromSession,
    hasChanges,
    getChangesCount,
    getSessionData,
    hasPendingChanges,
    getChangesSummary,
  };

  return (
    <StagingContext.Provider value={value}>
      {children}
    </StagingContext.Provider>
  );
}

export function useStaging() {
  const context = useContext(StagingContext);
  if (context === undefined) {
    throw new Error('useStaging must be used within a StagingProvider');
  }
  return context;
}
