// Azure VM Size Specifications
// Fonte: https://docs.microsoft.com/en-us/azure/virtual-machines/sizes

export interface VMSpec {
  size: string;
  vCPUs: number;
  memoryGiB: number;
  family: string;
  description?: string;
  tempDiskGiB?: number;      // Disco temporário local
  maxDataDisks?: number;     // Número máximo de discos de dados
  maxIOPS?: number;          // IOPS máximo (sem cache)
  maxThroughputMBps?: number; // Throughput máximo em MB/s
}

export const azureVMSpecs: Record<string, VMSpec> = {
  // === A-Series (General Purpose - Basic) ===
  "Standard_A1_v2": { size: "Standard_A1_v2", vCPUs: 1, memoryGiB: 2, family: "A-Series v2" },
  "Standard_A2_v2": { size: "Standard_A2_v2", vCPUs: 2, memoryGiB: 4, family: "A-Series v2" },
  "Standard_A4_v2": { size: "Standard_A4_v2", vCPUs: 4, memoryGiB: 8, family: "A-Series v2" },
  "Standard_A8_v2": { size: "Standard_A8_v2", vCPUs: 8, memoryGiB: 16, family: "A-Series v2" },
  "Standard_A2m_v2": { size: "Standard_A2m_v2", vCPUs: 2, memoryGiB: 16, family: "A-Series v2" },
  "Standard_A4m_v2": { size: "Standard_A4m_v2", vCPUs: 4, memoryGiB: 32, family: "A-Series v2" },
  "Standard_A8m_v2": { size: "Standard_A8m_v2", vCPUs: 8, memoryGiB: 64, family: "A-Series v2" },

  // === B-Series (Burstable - Economical) ===
  "Standard_B1s": { size: "Standard_B1s", vCPUs: 1, memoryGiB: 1, family: "B-Series" },
  "Standard_B1ms": { size: "Standard_B1ms", vCPUs: 1, memoryGiB: 2, family: "B-Series" },
  "Standard_B2s": { size: "Standard_B2s", vCPUs: 2, memoryGiB: 4, family: "B-Series" },
  "Standard_B2ms": { size: "Standard_B2ms", vCPUs: 2, memoryGiB: 8, family: "B-Series" },
  "Standard_B4ms": { size: "Standard_B4ms", vCPUs: 4, memoryGiB: 16, family: "B-Series" },
  "Standard_B8ms": { size: "Standard_B8ms", vCPUs: 8, memoryGiB: 32, family: "B-Series" },
  "Standard_B12ms": { size: "Standard_B12ms", vCPUs: 12, memoryGiB: 48, family: "B-Series" },
  "Standard_B16ms": { size: "Standard_B16ms", vCPUs: 16, memoryGiB: 64, family: "B-Series" },
  "Standard_B20ms": { size: "Standard_B20ms", vCPUs: 20, memoryGiB: 80, family: "B-Series" },

  // === D-Series v2 (General Purpose) ===
  "Standard_D1_v2": { size: "Standard_D1_v2", vCPUs: 1, memoryGiB: 3.5, family: "D-Series v2" },
  "Standard_D2_v2": { size: "Standard_D2_v2", vCPUs: 2, memoryGiB: 7, family: "D-Series v2" },
  "Standard_D3_v2": { size: "Standard_D3_v2", vCPUs: 4, memoryGiB: 14, family: "D-Series v2" },
  "Standard_D4_v2": { size: "Standard_D4_v2", vCPUs: 8, memoryGiB: 28, family: "D-Series v2" },
  "Standard_D5_v2": { size: "Standard_D5_v2", vCPUs: 16, memoryGiB: 56, family: "D-Series v2" },
  "Standard_D11_v2": { size: "Standard_D11_v2", vCPUs: 2, memoryGiB: 14, family: "D-Series v2" },
  "Standard_D12_v2": { size: "Standard_D12_v2", vCPUs: 4, memoryGiB: 28, family: "D-Series v2" },
  "Standard_D13_v2": { size: "Standard_D13_v2", vCPUs: 8, memoryGiB: 56, family: "D-Series v2" },
  "Standard_D14_v2": { size: "Standard_D14_v2", vCPUs: 16, memoryGiB: 112, family: "D-Series v2" },
  "Standard_D15_v2": { size: "Standard_D15_v2", vCPUs: 20, memoryGiB: 140, family: "D-Series v2" },

  // === Dsv3-Series (General Purpose with SSD) ===
  "Standard_D2s_v3": { size: "Standard_D2s_v3", vCPUs: 2, memoryGiB: 8, family: "Dsv3-Series", tempDiskGiB: 16, maxDataDisks: 4, maxIOPS: 3200, maxThroughputMBps: 48 },
  "Standard_D4s_v3": { size: "Standard_D4s_v3", vCPUs: 4, memoryGiB: 16, family: "Dsv3-Series", tempDiskGiB: 32, maxDataDisks: 8, maxIOPS: 6400, maxThroughputMBps: 96 },
  "Standard_D8s_v3": { size: "Standard_D8s_v3", vCPUs: 8, memoryGiB: 32, family: "Dsv3-Series", tempDiskGiB: 64, maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 192 },
  "Standard_D16s_v3": { size: "Standard_D16s_v3", vCPUs: 16, memoryGiB: 64, family: "Dsv3-Series", tempDiskGiB: 128, maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 384 },
  "Standard_D32s_v3": { size: "Standard_D32s_v3", vCPUs: 32, memoryGiB: 128, family: "Dsv3-Series", tempDiskGiB: 256, maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 768 },
  "Standard_D48s_v3": { size: "Standard_D48s_v3", vCPUs: 48, memoryGiB: 192, family: "Dsv3-Series", tempDiskGiB: 384, maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1152 },
  "Standard_D64s_v3": { size: "Standard_D64s_v3", vCPUs: 64, memoryGiB: 256, family: "Dsv3-Series", tempDiskGiB: 512, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200 },

  // === Dsv4-Series (General Purpose - Latest) ===
  "Standard_D2s_v4": { size: "Standard_D2s_v4", vCPUs: 2, memoryGiB: 8, family: "Dsv4-Series" },
  "Standard_D4s_v4": { size: "Standard_D4s_v4", vCPUs: 4, memoryGiB: 16, family: "Dsv4-Series" },
  "Standard_D8s_v4": { size: "Standard_D8s_v4", vCPUs: 8, memoryGiB: 32, family: "Dsv4-Series" },
  "Standard_D16s_v4": { size: "Standard_D16s_v4", vCPUs: 16, memoryGiB: 64, family: "Dsv4-Series" },
  "Standard_D32s_v4": { size: "Standard_D32s_v4", vCPUs: 32, memoryGiB: 128, family: "Dsv4-Series" },
  "Standard_D48s_v4": { size: "Standard_D48s_v4", vCPUs: 48, memoryGiB: 192, family: "Dsv4-Series" },
  "Standard_D64s_v4": { size: "Standard_D64s_v4", vCPUs: 64, memoryGiB: 256, family: "Dsv4-Series" },

  // === Dsv5-Series (General Purpose - 5th Gen) ===
  "Standard_D2s_v5": { size: "Standard_D2s_v5", vCPUs: 2, memoryGiB: 8, family: "Dsv5-Series", maxDataDisks: 4, maxIOPS: 3750, maxThroughputMBps: 85 },
  "Standard_D4s_v5": { size: "Standard_D4s_v5", vCPUs: 4, memoryGiB: 16, family: "Dsv5-Series", maxDataDisks: 8, maxIOPS: 6400, maxThroughputMBps: 145 },
  "Standard_D8s_v5": { size: "Standard_D8s_v5", vCPUs: 8, memoryGiB: 32, family: "Dsv5-Series", maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 290 },
  "Standard_D16s_v5": { size: "Standard_D16s_v5", vCPUs: 16, memoryGiB: 64, family: "Dsv5-Series", maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 600 },
  "Standard_D32s_v5": { size: "Standard_D32s_v5", vCPUs: 32, memoryGiB: 128, family: "Dsv5-Series", maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 865 },
  "Standard_D48s_v5": { size: "Standard_D48s_v5", vCPUs: 48, memoryGiB: 192, family: "Dsv5-Series", maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1315 },
  "Standard_D64s_v5": { size: "Standard_D64s_v5", vCPUs: 64, memoryGiB: 256, family: "Dsv5-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1735 },
  "Standard_D96s_v5": { size: "Standard_D96s_v5", vCPUs: 96, memoryGiB: 384, family: "Dsv5-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 2600 },

  // === E-Series (Memory Optimized) ===
  "Standard_E2s_v3": { size: "Standard_E2s_v3", vCPUs: 2, memoryGiB: 16, family: "Esv3-Series", tempDiskGiB: 32, maxDataDisks: 4, maxIOPS: 3200, maxThroughputMBps: 48 },
  "Standard_E4s_v3": { size: "Standard_E4s_v3", vCPUs: 4, memoryGiB: 32, family: "Esv3-Series", tempDiskGiB: 64, maxDataDisks: 8, maxIOPS: 6400, maxThroughputMBps: 96 },
  "Standard_E8s_v3": { size: "Standard_E8s_v3", vCPUs: 8, memoryGiB: 64, family: "Esv3-Series", tempDiskGiB: 128, maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 192 },
  "Standard_E16s_v3": { size: "Standard_E16s_v3", vCPUs: 16, memoryGiB: 128, family: "Esv3-Series", tempDiskGiB: 256, maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 384 },
  "Standard_E20s_v3": { size: "Standard_E20s_v3", vCPUs: 20, memoryGiB: 160, family: "Esv3-Series", tempDiskGiB: 320, maxDataDisks: 32, maxIOPS: 32000, maxThroughputMBps: 480 },
  "Standard_E32s_v3": { size: "Standard_E32s_v3", vCPUs: 32, memoryGiB: 256, family: "Esv3-Series", tempDiskGiB: 512, maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 768 },
  "Standard_E48s_v3": { size: "Standard_E48s_v3", vCPUs: 48, memoryGiB: 384, family: "Esv3-Series", tempDiskGiB: 768, maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1152 },
  "Standard_E64s_v3": { size: "Standard_E64s_v3", vCPUs: 64, memoryGiB: 432, family: "Esv3-Series", tempDiskGiB: 864, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200 },

  // === Esv4-Series (Memory Optimized - Latest) ===
  "Standard_E2s_v4": { size: "Standard_E2s_v4", vCPUs: 2, memoryGiB: 16, family: "Esv4-Series" },
  "Standard_E4s_v4": { size: "Standard_E4s_v4", vCPUs: 4, memoryGiB: 32, family: "Esv4-Series" },
  "Standard_E8s_v4": { size: "Standard_E8s_v4", vCPUs: 8, memoryGiB: 64, family: "Esv4-Series" },
  "Standard_E16s_v4": { size: "Standard_E16s_v4", vCPUs: 16, memoryGiB: 128, family: "Esv4-Series" },
  "Standard_E20s_v4": { size: "Standard_E20s_v4", vCPUs: 20, memoryGiB: 160, family: "Esv4-Series" },
  "Standard_E32s_v4": { size: "Standard_E32s_v4", vCPUs: 32, memoryGiB: 256, family: "Esv4-Series" },
  "Standard_E48s_v4": { size: "Standard_E48s_v4", vCPUs: 48, memoryGiB: 384, family: "Esv4-Series" },
  "Standard_E64s_v4": { size: "Standard_E64s_v4", vCPUs: 64, memoryGiB: 504, family: "Esv4-Series" },

  // === Esv5-Series (Memory Optimized - 5th Gen) ===
  "Standard_E2s_v5": { size: "Standard_E2s_v5", vCPUs: 2, memoryGiB: 16, family: "Esv5-Series" },
  "Standard_E4s_v5": { size: "Standard_E4s_v5", vCPUs: 4, memoryGiB: 32, family: "Esv5-Series" },
  "Standard_E8s_v5": { size: "Standard_E8s_v5", vCPUs: 8, memoryGiB: 64, family: "Esv5-Series" },
  "Standard_E16s_v5": { size: "Standard_E16s_v5", vCPUs: 16, memoryGiB: 128, family: "Esv5-Series" },
  "Standard_E20s_v5": { size: "Standard_E20s_v5", vCPUs: 20, memoryGiB: 160, family: "Esv5-Series" },
  "Standard_E32s_v5": { size: "Standard_E32s_v5", vCPUs: 32, memoryGiB: 256, family: "Esv5-Series" },
  "Standard_E48s_v5": { size: "Standard_E48s_v5", vCPUs: 48, memoryGiB: 384, family: "Esv5-Series" },
  "Standard_E64s_v5": { size: "Standard_E64s_v5", vCPUs: 64, memoryGiB: 512, family: "Esv5-Series" },
  "Standard_E96s_v5": { size: "Standard_E96s_v5", vCPUs: 96, memoryGiB: 672, family: "Esv5-Series" },

  // === F-Series (Compute Optimized) ===
  "Standard_F2s_v2": { size: "Standard_F2s_v2", vCPUs: 2, memoryGiB: 4, family: "Fsv2-Series" },
  "Standard_F4s_v2": { size: "Standard_F4s_v2", vCPUs: 4, memoryGiB: 8, family: "Fsv2-Series" },
  "Standard_F8s_v2": { size: "Standard_F8s_v2", vCPUs: 8, memoryGiB: 16, family: "Fsv2-Series" },
  "Standard_F16s_v2": { size: "Standard_F16s_v2", vCPUs: 16, memoryGiB: 32, family: "Fsv2-Series" },
  "Standard_F32s_v2": { size: "Standard_F32s_v2", vCPUs: 32, memoryGiB: 64, family: "Fsv2-Series" },
  "Standard_F48s_v2": { size: "Standard_F48s_v2", vCPUs: 48, memoryGiB: 96, family: "Fsv2-Series" },
  "Standard_F64s_v2": { size: "Standard_F64s_v2", vCPUs: 64, memoryGiB: 128, family: "Fsv2-Series" },
  "Standard_F72s_v2": { size: "Standard_F72s_v2", vCPUs: 72, memoryGiB: 144, family: "Fsv2-Series" },

  // === L-Series (Storage Optimized) ===
  "Standard_L4s": { size: "Standard_L4s", vCPUs: 4, memoryGiB: 32, family: "L-Series" },
  "Standard_L8s": { size: "Standard_L8s", vCPUs: 8, memoryGiB: 64, family: "L-Series" },
  "Standard_L16s": { size: "Standard_L16s", vCPUs: 16, memoryGiB: 128, family: "L-Series" },
  "Standard_L32s": { size: "Standard_L32s", vCPUs: 32, memoryGiB: 256, family: "L-Series" },

  // === Lsv2-Series (Storage Optimized - v2) ===
  "Standard_L8s_v2": { size: "Standard_L8s_v2", vCPUs: 8, memoryGiB: 64, family: "Lsv2-Series" },
  "Standard_L16s_v2": { size: "Standard_L16s_v2", vCPUs: 16, memoryGiB: 128, family: "Lsv2-Series" },
  "Standard_L32s_v2": { size: "Standard_L32s_v2", vCPUs: 32, memoryGiB: 256, family: "Lsv2-Series" },
  "Standard_L48s_v2": { size: "Standard_L48s_v2", vCPUs: 48, memoryGiB: 384, family: "Lsv2-Series" },
  "Standard_L64s_v2": { size: "Standard_L64s_v2", vCPUs: 64, memoryGiB: 512, family: "Lsv2-Series" },
  "Standard_L80s_v2": { size: "Standard_L80s_v2", vCPUs: 80, memoryGiB: 640, family: "Lsv2-Series" },

  // === M-Series (Memory Optimized - Large) ===
  "Standard_M8ms": { size: "Standard_M8ms", vCPUs: 8, memoryGiB: 218.75, family: "M-Series" },
  "Standard_M16ms": { size: "Standard_M16ms", vCPUs: 16, memoryGiB: 437.5, family: "M-Series" },
  "Standard_M32ms": { size: "Standard_M32ms", vCPUs: 32, memoryGiB: 875, family: "M-Series" },
  "Standard_M64ms": { size: "Standard_M64ms", vCPUs: 64, memoryGiB: 1750, family: "M-Series" },
  "Standard_M128ms": { size: "Standard_M128ms", vCPUs: 128, memoryGiB: 3800, family: "M-Series" },

  // === NC-Series (GPU - Compute) ===
  "Standard_NC6": { size: "Standard_NC6", vCPUs: 6, memoryGiB: 56, family: "NC-Series", description: "1x NVIDIA Tesla K80" },
  "Standard_NC12": { size: "Standard_NC12", vCPUs: 12, memoryGiB: 112, family: "NC-Series", description: "2x NVIDIA Tesla K80" },
  "Standard_NC24": { size: "Standard_NC24", vCPUs: 24, memoryGiB: 224, family: "NC-Series", description: "4x NVIDIA Tesla K80" },

  // === NCv3-Series (GPU - V100) ===
  "Standard_NC6s_v3": { size: "Standard_NC6s_v3", vCPUs: 6, memoryGiB: 112, family: "NCv3-Series", description: "1x NVIDIA Tesla V100" },
  "Standard_NC12s_v3": { size: "Standard_NC12s_v3", vCPUs: 12, memoryGiB: 224, family: "NCv3-Series", description: "2x NVIDIA Tesla V100" },
  "Standard_NC24s_v3": { size: "Standard_NC24s_v3", vCPUs: 24, memoryGiB: 448, family: "NCv3-Series", description: "4x NVIDIA Tesla V100" },

  // === NV-Series (GPU - Visualization) ===
  "Standard_NV6": { size: "Standard_NV6", vCPUs: 6, memoryGiB: 56, family: "NV-Series", description: "1x NVIDIA Tesla M60" },
  "Standard_NV12": { size: "Standard_NV12", vCPUs: 12, memoryGiB: 112, family: "NV-Series", description: "2x NVIDIA Tesla M60" },
  "Standard_NV24": { size: "Standard_NV24", vCPUs: 24, memoryGiB: 224, family: "NV-Series", description: "4x NVIDIA Tesla M60" },
};

/**
 * Obtém as especificações de uma VM Azure pelo tamanho
 * @param vmSize - Tamanho da VM (ex: "Standard_F4s_v2")
 * @returns Especificações da VM ou null se não encontrado
 */
export function getVMSpecs(vmSize: string): VMSpec | null {
  return azureVMSpecs[vmSize] || null;
}

/**
 * Formata as especificações da VM para exibição
 * @param vmSize - Tamanho da VM
 * @returns String formatada (ex: "4 vCPUs, 8 GiB") ou null
 */
export function formatVMSpecs(vmSize: string): string | null {
  const specs = getVMSpecs(vmSize);
  if (!specs) return null;

  const memoryFormatted = specs.memoryGiB >= 1000
    ? `${(specs.memoryGiB / 1000).toFixed(1)} TiB`
    : `${specs.memoryGiB} GiB`;

  return `${specs.vCPUs} vCPU${specs.vCPUs > 1 ? 's' : ''}, ${memoryFormatted}`;
}

/**
 * Formata as especificações de disco da VM para exibição
 * @param vmSize - Tamanho da VM
 * @returns String formatada com informações de disco ou null
 */
export function formatDiskSpecs(vmSize: string): string | null {
  const specs = getVMSpecs(vmSize);
  if (!specs) return null;

  const parts: string[] = [];

  // Temp disk
  if (specs.tempDiskGiB) {
    parts.push(`💾 Temp: ${specs.tempDiskGiB} GiB`);
  }

  // Max data disks
  if (specs.maxDataDisks) {
    parts.push(`📀 Max Disks: ${specs.maxDataDisks}`);
  }

  // IOPS
  if (specs.maxIOPS) {
    const iopsFormatted = specs.maxIOPS >= 1000
      ? `${(specs.maxIOPS / 1000).toFixed(0)}K`
      : specs.maxIOPS.toString();
    parts.push(`⚡ ${iopsFormatted} IOPS`);
  }

  // Throughput
  if (specs.maxThroughputMBps) {
    const throughputFormatted = specs.maxThroughputMBps >= 1000
      ? `${(specs.maxThroughputMBps / 1000).toFixed(1)} GB/s`
      : `${specs.maxThroughputMBps} MB/s`;
    parts.push(`📊 ${throughputFormatted}`);
  }

  return parts.length > 0 ? parts.join(' • ') : null;
}

/**
 * Busca VMs por família
 * @param family - Nome da família (ex: "Fsv2-Series")
 * @returns Array de especificações de VMs
 */
export function getVMsByFamily(family: string): VMSpec[] {
  return Object.values(azureVMSpecs).filter(vm => vm.family === family);
}

/**
 * Lista todas as famílias de VMs disponíveis
 * @returns Array com nomes únicos de famílias
 */
export function getVMFamilies(): string[] {
  const families = new Set(Object.values(azureVMSpecs).map(vm => vm.family));
  return Array.from(families).sort();
}
