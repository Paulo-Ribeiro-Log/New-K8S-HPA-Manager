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
  supportsEphemeralOS?: boolean; // Suporta disco de SO efêmero
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

  // === Bsv2-Series (Burstable v2 - Intel Ice Lake/Sapphire Rapids) ===
  // Sem disco temporário local — Intel Xeon Platinum 8370C/8473C/8573C
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/general-purpose/bsv2-series
  "Standard_B2ts_v2":  { size: "Standard_B2ts_v2",  vCPUs: 2,  memoryGiB: 1,   family: "Bsv2-Series", maxDataDisks: 4,  maxIOPS: 3750,  maxThroughputMBps: 85  },
  "Standard_B2ls_v2":  { size: "Standard_B2ls_v2",  vCPUs: 2,  memoryGiB: 4,   family: "Bsv2-Series", maxDataDisks: 4,  maxIOPS: 3750,  maxThroughputMBps: 85  },
  "Standard_B2s_v2":   { size: "Standard_B2s_v2",   vCPUs: 2,  memoryGiB: 8,   family: "Bsv2-Series", maxDataDisks: 4,  maxIOPS: 3750,  maxThroughputMBps: 85  },
  "Standard_B4ls_v2":  { size: "Standard_B4ls_v2",  vCPUs: 4,  memoryGiB: 8,   family: "Bsv2-Series", maxDataDisks: 4,  maxIOPS: 3750,  maxThroughputMBps: 85  },
  "Standard_B4s_v2":   { size: "Standard_B4s_v2",   vCPUs: 4,  memoryGiB: 16,  family: "Bsv2-Series", maxDataDisks: 8,  maxIOPS: 6400,  maxThroughputMBps: 145 },
  "Standard_B8ls_v2":  { size: "Standard_B8ls_v2",  vCPUs: 8,  memoryGiB: 16,  family: "Bsv2-Series", maxDataDisks: 8,  maxIOPS: 6400,  maxThroughputMBps: 145 },
  "Standard_B8s_v2":   { size: "Standard_B8s_v2",   vCPUs: 8,  memoryGiB: 32,  family: "Bsv2-Series", maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 290 },
  "Standard_B16ls_v2": { size: "Standard_B16ls_v2", vCPUs: 16, memoryGiB: 32,  family: "Bsv2-Series", maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 290 },
  "Standard_B16s_v2":  { size: "Standard_B16s_v2",  vCPUs: 16, memoryGiB: 64,  family: "Bsv2-Series", maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 600 },
  "Standard_B32ls_v2": { size: "Standard_B32ls_v2", vCPUs: 32, memoryGiB: 64,  family: "Bsv2-Series", maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 600 },
  "Standard_B32s_v2":  { size: "Standard_B32s_v2",  vCPUs: 32, memoryGiB: 128, family: "Bsv2-Series", maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 600 },

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
  "Standard_D2s_v3": { size: "Standard_D2s_v3", vCPUs: 2, memoryGiB: 8, family: "Dsv3-Series", tempDiskGiB: 16, maxDataDisks: 4, maxIOPS: 3200, maxThroughputMBps: 48, supportsEphemeralOS: true },
  "Standard_D4s_v3": { size: "Standard_D4s_v3", vCPUs: 4, memoryGiB: 16, family: "Dsv3-Series", tempDiskGiB: 32, maxDataDisks: 8, maxIOPS: 6400, maxThroughputMBps: 96, supportsEphemeralOS: true },
  "Standard_D8s_v3": { size: "Standard_D8s_v3", vCPUs: 8, memoryGiB: 32, family: "Dsv3-Series", tempDiskGiB: 64, maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 192, supportsEphemeralOS: true },
  "Standard_D16s_v3": { size: "Standard_D16s_v3", vCPUs: 16, memoryGiB: 64, family: "Dsv3-Series", tempDiskGiB: 128, maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 384, supportsEphemeralOS: true },
  "Standard_D32s_v3": { size: "Standard_D32s_v3", vCPUs: 32, memoryGiB: 128, family: "Dsv3-Series", tempDiskGiB: 256, maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 768, supportsEphemeralOS: true },
  "Standard_D48s_v3": { size: "Standard_D48s_v3", vCPUs: 48, memoryGiB: 192, family: "Dsv3-Series", tempDiskGiB: 384, maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1152, supportsEphemeralOS: true },
  "Standard_D64s_v3": { size: "Standard_D64s_v3", vCPUs: 64, memoryGiB: 256, family: "Dsv3-Series", tempDiskGiB: 512, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200, supportsEphemeralOS: true },

  // === Dsv4-Series (General Purpose - Latest) ===
  "Standard_D2s_v4": { size: "Standard_D2s_v4", vCPUs: 2, memoryGiB: 8, family: "Dsv4-Series", maxDataDisks: 4, maxIOPS: 3200, maxThroughputMBps: 48 },
  "Standard_D4s_v4": { size: "Standard_D4s_v4", vCPUs: 4, memoryGiB: 16, family: "Dsv4-Series", maxDataDisks: 8, maxIOPS: 6400, maxThroughputMBps: 96 },
  "Standard_D8s_v4": { size: "Standard_D8s_v4", vCPUs: 8, memoryGiB: 32, family: "Dsv4-Series", maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 192 },
  "Standard_D16s_v4": { size: "Standard_D16s_v4", vCPUs: 16, memoryGiB: 64, family: "Dsv4-Series", maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 384 },
  "Standard_D32s_v4": { size: "Standard_D32s_v4", vCPUs: 32, memoryGiB: 128, family: "Dsv4-Series", maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 768 },
  "Standard_D48s_v4": { size: "Standard_D48s_v4", vCPUs: 48, memoryGiB: 192, family: "Dsv4-Series", maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1152 },
  "Standard_D64s_v4": { size: "Standard_D64s_v4", vCPUs: 64, memoryGiB: 256, family: "Dsv4-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200 },

  // === Dsv5-Series (General Purpose - 5th Gen) ===
  "Standard_D2s_v5": { size: "Standard_D2s_v5", vCPUs: 2, memoryGiB: 8, family: "Dsv5-Series", maxDataDisks: 4, maxIOPS: 3750, maxThroughputMBps: 85 },
  "Standard_D4s_v5": { size: "Standard_D4s_v5", vCPUs: 4, memoryGiB: 16, family: "Dsv5-Series", maxDataDisks: 8, maxIOPS: 6400, maxThroughputMBps: 145 },
  "Standard_D8s_v5": { size: "Standard_D8s_v5", vCPUs: 8, memoryGiB: 32, family: "Dsv5-Series", maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 290 },
  "Standard_D16s_v5": { size: "Standard_D16s_v5", vCPUs: 16, memoryGiB: 64, family: "Dsv5-Series", maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 600 },
  "Standard_D32s_v5": { size: "Standard_D32s_v5", vCPUs: 32, memoryGiB: 128, family: "Dsv5-Series", maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 865 },
  "Standard_D48s_v5": { size: "Standard_D48s_v5", vCPUs: 48, memoryGiB: 192, family: "Dsv5-Series", maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1315 },
  "Standard_D64s_v5": { size: "Standard_D64s_v5", vCPUs: 64, memoryGiB: 256, family: "Dsv5-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1735 },
  "Standard_D96s_v5": { size: "Standard_D96s_v5", vCPUs: 96, memoryGiB: 384, family: "Dsv5-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 2600 },

  // === Ddsv5-Series (General Purpose com disco local SSD) ===
  // Intel Ice Lake/Emerald Rapids — versão do Dsv5 com disco temporário NVMe local
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/general-purpose/ddsv5-series
  "Standard_D2ds_v5":  { size: "Standard_D2ds_v5",  vCPUs: 2,  memoryGiB: 8,   family: "Ddsv5-Series", tempDiskGiB: 75,   maxDataDisks: 4,  maxIOPS: 3750,  maxThroughputMBps: 85,   supportsEphemeralOS: true },
  "Standard_D4ds_v5":  { size: "Standard_D4ds_v5",  vCPUs: 4,  memoryGiB: 16,  family: "Ddsv5-Series", tempDiskGiB: 150,  maxDataDisks: 8,  maxIOPS: 6400,  maxThroughputMBps: 145,  supportsEphemeralOS: true },
  "Standard_D8ds_v5":  { size: "Standard_D8ds_v5",  vCPUs: 8,  memoryGiB: 32,  family: "Ddsv5-Series", tempDiskGiB: 300,  maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 290,  supportsEphemeralOS: true },
  "Standard_D16ds_v5": { size: "Standard_D16ds_v5", vCPUs: 16, memoryGiB: 64,  family: "Ddsv5-Series", tempDiskGiB: 600,  maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 600,  supportsEphemeralOS: true },
  "Standard_D32ds_v5": { size: "Standard_D32ds_v5", vCPUs: 32, memoryGiB: 128, family: "Ddsv5-Series", tempDiskGiB: 1200, maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 865,  supportsEphemeralOS: true },
  "Standard_D48ds_v5": { size: "Standard_D48ds_v5", vCPUs: 48, memoryGiB: 192, family: "Ddsv5-Series", tempDiskGiB: 1800, maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1315, supportsEphemeralOS: true },
  "Standard_D64ds_v5": { size: "Standard_D64ds_v5", vCPUs: 64, memoryGiB: 256, family: "Ddsv5-Series", tempDiskGiB: 2400, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1735, supportsEphemeralOS: true },
  "Standard_D96ds_v5": { size: "Standard_D96ds_v5", vCPUs: 96, memoryGiB: 384, family: "Ddsv5-Series", tempDiskGiB: 3600, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 2600, supportsEphemeralOS: true },

  // === Dasv4-Series (General Purpose - AMD EPYC, com disco temporário) ===
  // AMD EPYC 7452 (Rome) / 7763v (Milan) — equivalente ao Dsv4 mas com CPU AMD
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/general-purpose/dasv4-series
  "Standard_D2as_v4": { size: "Standard_D2as_v4", vCPUs: 2, memoryGiB: 8, family: "Dasv4-Series", tempDiskGiB: 16, maxDataDisks: 4, maxIOPS: 3200, maxThroughputMBps: 48, supportsEphemeralOS: true },
  "Standard_D4as_v4": { size: "Standard_D4as_v4", vCPUs: 4, memoryGiB: 16, family: "Dasv4-Series", tempDiskGiB: 32, maxDataDisks: 8, maxIOPS: 6400, maxThroughputMBps: 96, supportsEphemeralOS: true },
  "Standard_D8as_v4": { size: "Standard_D8as_v4", vCPUs: 8, memoryGiB: 32, family: "Dasv4-Series", tempDiskGiB: 64, maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 192, supportsEphemeralOS: true },
  "Standard_D16as_v4": { size: "Standard_D16as_v4", vCPUs: 16, memoryGiB: 64, family: "Dasv4-Series", tempDiskGiB: 128, maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 384, supportsEphemeralOS: true },
  "Standard_D32as_v4": { size: "Standard_D32as_v4", vCPUs: 32, memoryGiB: 128, family: "Dasv4-Series", tempDiskGiB: 256, maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 768, supportsEphemeralOS: true },
  "Standard_D48as_v4": { size: "Standard_D48as_v4", vCPUs: 48, memoryGiB: 192, family: "Dasv4-Series", tempDiskGiB: 384, maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1152, supportsEphemeralOS: true },
  "Standard_D64as_v4": { size: "Standard_D64as_v4", vCPUs: 64, memoryGiB: 256, family: "Dasv4-Series", tempDiskGiB: 512, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200, supportsEphemeralOS: true },
  "Standard_D96as_v4": { size: "Standard_D96as_v4", vCPUs: 96, memoryGiB: 384, family: "Dasv4-Series", tempDiskGiB: 768, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200, supportsEphemeralOS: true },

  // === Dasv5-Series (General Purpose - AMD EPYC, sem disco local) ===
  // AMD EPYC 7763v (Milan) / 9004 (Genoa) — equivalente ao Dsv5 mas com CPU AMD
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/general-purpose/dasv5-series
  "Standard_D2as_v5": { size: "Standard_D2as_v5", vCPUs: 2, memoryGiB: 8, family: "Dasv5-Series", maxDataDisks: 4, maxIOPS: 3750, maxThroughputMBps: 82 },
  "Standard_D4as_v5": { size: "Standard_D4as_v5", vCPUs: 4, memoryGiB: 16, family: "Dasv5-Series", maxDataDisks: 8, maxIOPS: 6400, maxThroughputMBps: 145 },
  "Standard_D8as_v5": { size: "Standard_D8as_v5", vCPUs: 8, memoryGiB: 32, family: "Dasv5-Series", maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 290 },
  "Standard_D16as_v5": { size: "Standard_D16as_v5", vCPUs: 16, memoryGiB: 64, family: "Dasv5-Series", maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 600 },
  "Standard_D32as_v5": { size: "Standard_D32as_v5", vCPUs: 32, memoryGiB: 128, family: "Dasv5-Series", maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 865 },
  "Standard_D48as_v5": { size: "Standard_D48as_v5", vCPUs: 48, memoryGiB: 192, family: "Dasv5-Series", maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1315 },
  "Standard_D64as_v5": { size: "Standard_D64as_v5", vCPUs: 64, memoryGiB: 256, family: "Dasv5-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1735 },
  "Standard_D96as_v5": { size: "Standard_D96as_v5", vCPUs: 96, memoryGiB: 384, family: "Dasv5-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 2600 },

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

  // === Easv4-Series (Memory Optimized - AMD EPYC, com disco temporário) ===
  // AMD EPYC 7452 (Rome) / 7763v (Milan) — equivalente ao Esv4 mas com CPU AMD
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/memory-optimized/easv4-series
  "Standard_E2as_v4":  { size: "Standard_E2as_v4",  vCPUs: 2,  memoryGiB: 16,  family: "Easv4-Series", tempDiskGiB: 32,   maxDataDisks: 4,  maxIOPS: 3200,  maxThroughputMBps: 48,   supportsEphemeralOS: true },
  "Standard_E4as_v4":  { size: "Standard_E4as_v4",  vCPUs: 4,  memoryGiB: 32,  family: "Easv4-Series", tempDiskGiB: 64,   maxDataDisks: 8,  maxIOPS: 6400,  maxThroughputMBps: 96,   supportsEphemeralOS: true },
  "Standard_E8as_v4":  { size: "Standard_E8as_v4",  vCPUs: 8,  memoryGiB: 64,  family: "Easv4-Series", tempDiskGiB: 128,  maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 192,  supportsEphemeralOS: true },
  "Standard_E16as_v4": { size: "Standard_E16as_v4", vCPUs: 16, memoryGiB: 128, family: "Easv4-Series", tempDiskGiB: 256,  maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 384,  supportsEphemeralOS: true },
  "Standard_E20as_v4": { size: "Standard_E20as_v4", vCPUs: 20, memoryGiB: 160, family: "Easv4-Series", tempDiskGiB: 320,  maxDataDisks: 32, maxIOPS: 32000, maxThroughputMBps: 480,  supportsEphemeralOS: true },
  "Standard_E32as_v4": { size: "Standard_E32as_v4", vCPUs: 32, memoryGiB: 256, family: "Easv4-Series", tempDiskGiB: 512,  maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 768,  supportsEphemeralOS: true },
  "Standard_E48as_v4": { size: "Standard_E48as_v4", vCPUs: 48, memoryGiB: 384, family: "Easv4-Series", tempDiskGiB: 768,  maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1152, supportsEphemeralOS: true },
  "Standard_E64as_v4": { size: "Standard_E64as_v4", vCPUs: 64, memoryGiB: 512, family: "Easv4-Series", tempDiskGiB: 1024, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200, supportsEphemeralOS: true },
  "Standard_E96as_v4": { size: "Standard_E96as_v4", vCPUs: 96, memoryGiB: 672, family: "Easv4-Series", tempDiskGiB: 1344, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200 },

  // === Easv5-Series (Memory Optimized - AMD EPYC, sem disco local) ===
  // AMD EPYC 7763v (Milan) / 9004 (Genoa) — equivalente ao Esv5 mas com CPU AMD
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/memory-optimized/easv5-series
  "Standard_E2as_v5":    { size: "Standard_E2as_v5",    vCPUs: 2,   memoryGiB: 16,  family: "Easv5-Series", maxDataDisks: 4,  maxIOPS: 3750,   maxThroughputMBps: 82   },
  "Standard_E4as_v5":    { size: "Standard_E4as_v5",    vCPUs: 4,   memoryGiB: 32,  family: "Easv5-Series", maxDataDisks: 8,  maxIOPS: 6400,   maxThroughputMBps: 145  },
  "Standard_E8as_v5":    { size: "Standard_E8as_v5",    vCPUs: 8,   memoryGiB: 64,  family: "Easv5-Series", maxDataDisks: 16, maxIOPS: 12800,  maxThroughputMBps: 290  },
  "Standard_E16as_v5":   { size: "Standard_E16as_v5",   vCPUs: 16,  memoryGiB: 128, family: "Easv5-Series", maxDataDisks: 32, maxIOPS: 25600,  maxThroughputMBps: 600  },
  "Standard_E20as_v5":   { size: "Standard_E20as_v5",   vCPUs: 20,  memoryGiB: 160, family: "Easv5-Series", maxDataDisks: 32, maxIOPS: 32000,  maxThroughputMBps: 750  },
  "Standard_E32as_v5":   { size: "Standard_E32as_v5",   vCPUs: 32,  memoryGiB: 256, family: "Easv5-Series", maxDataDisks: 32, maxIOPS: 51200,  maxThroughputMBps: 865  },
  "Standard_E48as_v5":   { size: "Standard_E48as_v5",   vCPUs: 48,  memoryGiB: 384, family: "Easv5-Series", maxDataDisks: 32, maxIOPS: 76800,  maxThroughputMBps: 1315 },
  "Standard_E64as_v5":   { size: "Standard_E64as_v5",   vCPUs: 64,  memoryGiB: 512, family: "Easv5-Series", maxDataDisks: 32, maxIOPS: 80000,  maxThroughputMBps: 1735 },
  "Standard_E96as_v5":   { size: "Standard_E96as_v5",   vCPUs: 96,  memoryGiB: 672, family: "Easv5-Series", maxDataDisks: 32, maxIOPS: 80000,  maxThroughputMBps: 2600 },
  "Standard_E112ias_v5": { size: "Standard_E112ias_v5", vCPUs: 112, memoryGiB: 672, family: "Easv5-Series", maxDataDisks: 64, maxIOPS: 120000, maxThroughputMBps: 2000 },

  // === Edsv5-Series (Memory Optimized - Intel, com disco local SSD) ===
  // Intel Ice Lake/Emerald Rapids — versão do Esv5 com disco temporário NVMe local
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/memory-optimized/edsv5-series
  "Standard_E2ds_v5":    { size: "Standard_E2ds_v5",    vCPUs: 2,   memoryGiB: 16,  family: "Edsv5-Series", tempDiskGiB: 75,   maxDataDisks: 4,  maxIOPS: 3750,   maxThroughputMBps: 85,   supportsEphemeralOS: true },
  "Standard_E4ds_v5":    { size: "Standard_E4ds_v5",    vCPUs: 4,   memoryGiB: 32,  family: "Edsv5-Series", tempDiskGiB: 150,  maxDataDisks: 8,  maxIOPS: 6400,   maxThroughputMBps: 145,  supportsEphemeralOS: true },
  "Standard_E8ds_v5":    { size: "Standard_E8ds_v5",    vCPUs: 8,   memoryGiB: 64,  family: "Edsv5-Series", tempDiskGiB: 300,  maxDataDisks: 16, maxIOPS: 12800,  maxThroughputMBps: 290,  supportsEphemeralOS: true },
  "Standard_E16ds_v5":   { size: "Standard_E16ds_v5",   vCPUs: 16,  memoryGiB: 128, family: "Edsv5-Series", tempDiskGiB: 600,  maxDataDisks: 32, maxIOPS: 25600,  maxThroughputMBps: 600,  supportsEphemeralOS: true },
  "Standard_E20ds_v5":   { size: "Standard_E20ds_v5",   vCPUs: 20,  memoryGiB: 160, family: "Edsv5-Series", tempDiskGiB: 750,  maxDataDisks: 32, maxIOPS: 32000,  maxThroughputMBps: 750,  supportsEphemeralOS: true },
  "Standard_E32ds_v5":   { size: "Standard_E32ds_v5",   vCPUs: 32,  memoryGiB: 256, family: "Edsv5-Series", tempDiskGiB: 1200, maxDataDisks: 32, maxIOPS: 51200,  maxThroughputMBps: 865,  supportsEphemeralOS: true },
  "Standard_E48ds_v5":   { size: "Standard_E48ds_v5",   vCPUs: 48,  memoryGiB: 384, family: "Edsv5-Series", tempDiskGiB: 1800, maxDataDisks: 32, maxIOPS: 76800,  maxThroughputMBps: 1315, supportsEphemeralOS: true },
  "Standard_E64ds_v5":   { size: "Standard_E64ds_v5",   vCPUs: 64,  memoryGiB: 512, family: "Edsv5-Series", tempDiskGiB: 2400, maxDataDisks: 32, maxIOPS: 80000,  maxThroughputMBps: 1735, supportsEphemeralOS: true },
  "Standard_E96ds_v5":   { size: "Standard_E96ds_v5",   vCPUs: 96,  memoryGiB: 672, family: "Edsv5-Series", tempDiskGiB: 3600, maxDataDisks: 32, maxIOPS: 80000,  maxThroughputMBps: 2600, supportsEphemeralOS: true },
  "Standard_E104ids_v5": { size: "Standard_E104ids_v5", vCPUs: 104, memoryGiB: 672, family: "Edsv5-Series", tempDiskGiB: 3800, maxDataDisks: 64, maxIOPS: 120000, maxThroughputMBps: 4000, supportsEphemeralOS: true },

  // === F-Series (Compute Optimized) ===
  "Standard_F2s_v2": { size: "Standard_F2s_v2", vCPUs: 2, memoryGiB: 4, family: "Fsv2-Series", tempDiskGiB: 16, maxDataDisks: 4, maxIOPS: 3200, maxThroughputMBps: 48 },
  "Standard_F4s_v2": { size: "Standard_F4s_v2", vCPUs: 4, memoryGiB: 8, family: "Fsv2-Series", tempDiskGiB: 32, maxDataDisks: 8, maxIOPS: 6400, maxThroughputMBps: 96 },
  "Standard_F8s_v2": { size: "Standard_F8s_v2", vCPUs: 8, memoryGiB: 16, family: "Fsv2-Series", tempDiskGiB: 64, maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 192 },
  "Standard_F16s_v2": { size: "Standard_F16s_v2", vCPUs: 16, memoryGiB: 32, family: "Fsv2-Series", tempDiskGiB: 128, maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 384 },
  "Standard_F32s_v2": { size: "Standard_F32s_v2", vCPUs: 32, memoryGiB: 64, family: "Fsv2-Series", tempDiskGiB: 256, maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 768 },
  "Standard_F48s_v2": { size: "Standard_F48s_v2", vCPUs: 48, memoryGiB: 96, family: "Fsv2-Series", tempDiskGiB: 384, maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1152 },
  "Standard_F64s_v2": { size: "Standard_F64s_v2", vCPUs: 64, memoryGiB: 128, family: "Fsv2-Series", tempDiskGiB: 512, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200 },
  "Standard_F72s_v2": { size: "Standard_F72s_v2", vCPUs: 72, memoryGiB: 144, family: "Fsv2-Series", tempDiskGiB: 576, maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200 },

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

  // === Lsv3-Series (Storage Optimized - Intel NVMe) ===
  // Intel Xeon Platinum 8370C (Ice Lake) — NVMe local de alta throughput
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/storage-optimized/lsv3-series
  "Standard_L8s_v3":  { size: "Standard_L8s_v3",  vCPUs: 8,  memoryGiB: 64,  family: "Lsv3-Series", maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 290,  description: "1x 1.92TB NVMe SSD local" },
  "Standard_L16s_v3": { size: "Standard_L16s_v3", vCPUs: 16, memoryGiB: 128, family: "Lsv3-Series", maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 500,  description: "2x 1.92TB NVMe SSD local" },
  "Standard_L32s_v3": { size: "Standard_L32s_v3", vCPUs: 32, memoryGiB: 256, family: "Lsv3-Series", maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 860,  description: "4x 1.92TB NVMe SSD local" },
  "Standard_L48s_v3": { size: "Standard_L48s_v3", vCPUs: 48, memoryGiB: 384, family: "Lsv3-Series", maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1200, description: "6x 1.92TB NVMe SSD local" },
  "Standard_L64s_v3": { size: "Standard_L64s_v3", vCPUs: 64, memoryGiB: 512, family: "Lsv3-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1600, description: "8x 1.92TB NVMe SSD local" },
  "Standard_L80s_v3": { size: "Standard_L80s_v3", vCPUs: 80, memoryGiB: 640, family: "Lsv3-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 2160, description: "10x 1.92TB NVMe SSD local" },

  // === Lasv3-Series (Storage Optimized - AMD NVMe) ===
  // AMD EPYC 7763v (Genoa) — NVMe local de alta throughput com CPU AMD
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/storage-optimized/lasv3-series
  "Standard_L8as_v3":  { size: "Standard_L8as_v3",  vCPUs: 8,  memoryGiB: 64,  family: "Lasv3-Series", maxDataDisks: 16, maxIOPS: 12800, maxThroughputMBps: 200,  description: "1x 1.92TB NVMe SSD local (AMD)" },
  "Standard_L16as_v3": { size: "Standard_L16as_v3", vCPUs: 16, memoryGiB: 128, family: "Lasv3-Series", maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 400,  description: "2x 1.92TB NVMe SSD local (AMD)" },
  "Standard_L32as_v3": { size: "Standard_L32as_v3", vCPUs: 32, memoryGiB: 256, family: "Lasv3-Series", maxDataDisks: 32, maxIOPS: 51200, maxThroughputMBps: 800,  description: "4x 1.92TB NVMe SSD local (AMD)" },
  "Standard_L48as_v3": { size: "Standard_L48as_v3", vCPUs: 48, memoryGiB: 384, family: "Lasv3-Series", maxDataDisks: 32, maxIOPS: 76800, maxThroughputMBps: 1000, description: "6x 1.92TB NVMe SSD local (AMD)" },
  "Standard_L64as_v3": { size: "Standard_L64as_v3", vCPUs: 64, memoryGiB: 512, family: "Lasv3-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200, description: "8x 1.92TB NVMe SSD local (AMD)" },
  "Standard_L80as_v3": { size: "Standard_L80as_v3", vCPUs: 80, memoryGiB: 640, family: "Lasv3-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1400, description: "10x 1.92TB NVMe SSD local (AMD)" },

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

  // === NCasT4_v3-Series (GPU - NVIDIA Tesla T4, AMD EPYC Rome) ===
  // AMD EPYC 7V12 (Rome) + 1-4x NVIDIA Tesla T4 (16GB each) — inferência IA e visualização
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/gpu-accelerated/ncast4v3-series
  "Standard_NC4as_T4_v3":  { size: "Standard_NC4as_T4_v3",  vCPUs: 4,  memoryGiB: 28,  family: "NCasT4_v3-Series", maxDataDisks: 8,  description: "1x NVIDIA Tesla T4 (16GB)" },
  "Standard_NC8as_T4_v3":  { size: "Standard_NC8as_T4_v3",  vCPUs: 8,  memoryGiB: 56,  family: "NCasT4_v3-Series", maxDataDisks: 16, description: "1x NVIDIA Tesla T4 (16GB)" },
  "Standard_NC16as_T4_v3": { size: "Standard_NC16as_T4_v3", vCPUs: 16, memoryGiB: 110, family: "NCasT4_v3-Series", maxDataDisks: 32, description: "1x NVIDIA Tesla T4 (16GB)" },
  "Standard_NC64as_T4_v3": { size: "Standard_NC64as_T4_v3", vCPUs: 64, memoryGiB: 440, family: "NCasT4_v3-Series", maxDataDisks: 32, description: "4x NVIDIA Tesla T4 (64GB total)" },

  // === NVadsA10_v5-Series (GPU - NVIDIA A10, AMD EPYC Milan) ===
  // AMD EPYC 74F3V (Milan) + NVIDIA A10 (24GB) parcial ou completo — VDI e cloud gaming
  // Fonte: https://learn.microsoft.com/en-us/azure/virtual-machines/sizes/gpu-accelerated/nvadsA10v5-series
  "Standard_NV6ads_A10_v5":   { size: "Standard_NV6ads_A10_v5",   vCPUs: 6,  memoryGiB: 55,  family: "NVadsA10_v5-Series", maxDataDisks: 4,  maxIOPS: 6400,  maxThroughputMBps: 100,  description: "1/6x NVIDIA A10 (4GB VRAM)" },
  "Standard_NV12ads_A10_v5":  { size: "Standard_NV12ads_A10_v5",  vCPUs: 12, memoryGiB: 110, family: "NVadsA10_v5-Series", maxDataDisks: 4,  maxIOPS: 6400,  maxThroughputMBps: 100,  description: "1/3x NVIDIA A10 (8GB VRAM)" },
  "Standard_NV18ads_A10_v5":  { size: "Standard_NV18ads_A10_v5",  vCPUs: 18, memoryGiB: 220, family: "NVadsA10_v5-Series", maxDataDisks: 8,  maxIOPS: 12800, maxThroughputMBps: 200,  description: "1/2x NVIDIA A10 (12GB VRAM)" },
  "Standard_NV36ads_A10_v5":  { size: "Standard_NV36ads_A10_v5",  vCPUs: 36, memoryGiB: 440, family: "NVadsA10_v5-Series", maxDataDisks: 16, maxIOPS: 25600, maxThroughputMBps: 400,  description: "1x NVIDIA A10 (24GB VRAM)" },
  "Standard_NV36adms_A10_v5": { size: "Standard_NV36adms_A10_v5", vCPUs: 36, memoryGiB: 880, family: "NVadsA10_v5-Series", maxDataDisks: 32, maxIOPS: 25600, maxThroughputMBps: 400,  description: "1x NVIDIA A10 (24GB VRAM) + memória extra" },
  "Standard_NV72ads_A10_v5":  { size: "Standard_NV72ads_A10_v5",  vCPUs: 72, memoryGiB: 880, family: "NVadsA10_v5-Series", maxDataDisks: 32, maxIOPS: 80000, maxThroughputMBps: 1200, description: "2x NVIDIA A10 (48GB VRAM total)" },
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
    parts.push(`Temp: ${specs.tempDiskGiB} GiB`);
  }

  // Max data disks
  if (specs.maxDataDisks) {
    parts.push(`Max Disks: ${specs.maxDataDisks}`);
  }

  // IOPS
  if (specs.maxIOPS) {
    const iopsFormatted = specs.maxIOPS >= 1000
      ? `${(specs.maxIOPS / 1000).toFixed(0)}K`
      : specs.maxIOPS.toString();
    parts.push(`${iopsFormatted} IOPS`);
  }

  // Throughput
  if (specs.maxThroughputMBps) {
    const throughputFormatted = specs.maxThroughputMBps >= 1000
      ? `${(specs.maxThroughputMBps / 1000).toFixed(1)} GB/s`
      : `${specs.maxThroughputMBps} MB/s`;
    parts.push(`${throughputFormatted}`);
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
