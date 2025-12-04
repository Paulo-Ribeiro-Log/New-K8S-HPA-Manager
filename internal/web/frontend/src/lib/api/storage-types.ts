// Storage Types for PVs, PVCs and StorageClasses

export interface StorageClassInfo {
  name: string;
  provisioner: string;
  reclaim_policy: string;
  volume_bind_mode: string;
  allow_expansion: boolean;
  parameters: Record<string, string>;
  pv_count: number;
  total_capacity_bytes: number;
  used_capacity_bytes?: number;
  available_capacity_bytes?: number;
  usage_percentage?: number;
}

export interface PVCInfo {
  name: string;
  namespace: string;
  status: string;
  storage_class: string;
  capacity_bytes: number;
  access_modes: string[];
  bound_pv: string;
  node_name?: string;
  used_bytes?: number;
  available_bytes?: number;
  usage_percentage?: number;
}

export interface StorageOverview {
  storage_classes: StorageClassInfo[];
  pvcs: PVCInfo[];
  total_pvs: number;
  total_pvcs: number;
  total_capacity_bytes: number;
  used_capacity_bytes: number;
}
