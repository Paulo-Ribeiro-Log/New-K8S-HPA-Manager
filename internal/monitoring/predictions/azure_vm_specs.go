package predictions

// VMSpec especificações de uma VM Azure
type VMSpec struct {
	Size      string
	VCPUs     int
	MemoryGiB int
	Family    string
}

// azureVMSpecs mapeamento completo de VM sizes do Azure
// Fonte: https://docs.microsoft.com/en-us/azure/virtual-machines/sizes
var azureVMSpecs = map[string]VMSpec{
	// === A-Series (General Purpose - Basic) ===
	"Standard_A1_v2":  {Size: "Standard_A1_v2", VCPUs: 1, MemoryGiB: 2, Family: "A-Series v2"},
	"Standard_A2_v2":  {Size: "Standard_A2_v2", VCPUs: 2, MemoryGiB: 4, Family: "A-Series v2"},
	"Standard_A4_v2":  {Size: "Standard_A4_v2", VCPUs: 4, MemoryGiB: 8, Family: "A-Series v2"},
	"Standard_A8_v2":  {Size: "Standard_A8_v2", VCPUs: 8, MemoryGiB: 16, Family: "A-Series v2"},
	"Standard_A2m_v2": {Size: "Standard_A2m_v2", VCPUs: 2, MemoryGiB: 16, Family: "A-Series v2"},
	"Standard_A4m_v2": {Size: "Standard_A4m_v2", VCPUs: 4, MemoryGiB: 32, Family: "A-Series v2"},
	"Standard_A8m_v2": {Size: "Standard_A8m_v2", VCPUs: 8, MemoryGiB: 64, Family: "A-Series v2"},

	// === B-Series (Burstable - Economical) ===
	"Standard_B1s":   {Size: "Standard_B1s", VCPUs: 1, MemoryGiB: 1, Family: "B-Series"},
	"Standard_B1ms":  {Size: "Standard_B1ms", VCPUs: 1, MemoryGiB: 2, Family: "B-Series"},
	"Standard_B2s":   {Size: "Standard_B2s", VCPUs: 2, MemoryGiB: 4, Family: "B-Series"},
	"Standard_B2ms":  {Size: "Standard_B2ms", VCPUs: 2, MemoryGiB: 8, Family: "B-Series"},
	"Standard_B4ms":  {Size: "Standard_B4ms", VCPUs: 4, MemoryGiB: 16, Family: "B-Series"},
	"Standard_B8ms":  {Size: "Standard_B8ms", VCPUs: 8, MemoryGiB: 32, Family: "B-Series"},
	"Standard_B12ms": {Size: "Standard_B12ms", VCPUs: 12, MemoryGiB: 48, Family: "B-Series"},
	"Standard_B16ms": {Size: "Standard_B16ms", VCPUs: 16, MemoryGiB: 64, Family: "B-Series"},
	"Standard_B20ms": {Size: "Standard_B20ms", VCPUs: 20, MemoryGiB: 80, Family: "B-Series"},

	// === D-Series v2 (General Purpose) ===
	"Standard_D1_v2":  {Size: "Standard_D1_v2", VCPUs: 1, MemoryGiB: 3, Family: "D-Series v2"}, // 3.5 arredondado
	"Standard_D2_v2":  {Size: "Standard_D2_v2", VCPUs: 2, MemoryGiB: 7, Family: "D-Series v2"},
	"Standard_D3_v2":  {Size: "Standard_D3_v2", VCPUs: 4, MemoryGiB: 14, Family: "D-Series v2"},
	"Standard_D4_v2":  {Size: "Standard_D4_v2", VCPUs: 8, MemoryGiB: 28, Family: "D-Series v2"},
	"Standard_D5_v2":  {Size: "Standard_D5_v2", VCPUs: 16, MemoryGiB: 56, Family: "D-Series v2"},
	"Standard_D11_v2": {Size: "Standard_D11_v2", VCPUs: 2, MemoryGiB: 14, Family: "D-Series v2"},
	"Standard_D12_v2": {Size: "Standard_D12_v2", VCPUs: 4, MemoryGiB: 28, Family: "D-Series v2"},
	"Standard_D13_v2": {Size: "Standard_D13_v2", VCPUs: 8, MemoryGiB: 56, Family: "D-Series v2"},
	"Standard_D14_v2": {Size: "Standard_D14_v2", VCPUs: 16, MemoryGiB: 112, Family: "D-Series v2"},
	"Standard_D15_v2": {Size: "Standard_D15_v2", VCPUs: 20, MemoryGiB: 140, Family: "D-Series v2"},

	// === Dsv3-Series (General Purpose with SSD) ===
	"Standard_D2s_v3":  {Size: "Standard_D2s_v3", VCPUs: 2, MemoryGiB: 8, Family: "Dsv3-Series"},
	"Standard_D4s_v3":  {Size: "Standard_D4s_v3", VCPUs: 4, MemoryGiB: 16, Family: "Dsv3-Series"},
	"Standard_D8s_v3":  {Size: "Standard_D8s_v3", VCPUs: 8, MemoryGiB: 32, Family: "Dsv3-Series"},
	"Standard_D16s_v3": {Size: "Standard_D16s_v3", VCPUs: 16, MemoryGiB: 64, Family: "Dsv3-Series"},
	"Standard_D32s_v3": {Size: "Standard_D32s_v3", VCPUs: 32, MemoryGiB: 128, Family: "Dsv3-Series"},
	"Standard_D48s_v3": {Size: "Standard_D48s_v3", VCPUs: 48, MemoryGiB: 192, Family: "Dsv3-Series"},
	"Standard_D64s_v3": {Size: "Standard_D64s_v3", VCPUs: 64, MemoryGiB: 256, Family: "Dsv3-Series"},

	// === Dsv4-Series (General Purpose - Latest) ===
	"Standard_D2s_v4":  {Size: "Standard_D2s_v4", VCPUs: 2, MemoryGiB: 8, Family: "Dsv4-Series"},
	"Standard_D4s_v4":  {Size: "Standard_D4s_v4", VCPUs: 4, MemoryGiB: 16, Family: "Dsv4-Series"},
	"Standard_D8s_v4":  {Size: "Standard_D8s_v4", VCPUs: 8, MemoryGiB: 32, Family: "Dsv4-Series"},
	"Standard_D16s_v4": {Size: "Standard_D16s_v4", VCPUs: 16, MemoryGiB: 64, Family: "Dsv4-Series"},
	"Standard_D32s_v4": {Size: "Standard_D32s_v4", VCPUs: 32, MemoryGiB: 128, Family: "Dsv4-Series"},
	"Standard_D48s_v4": {Size: "Standard_D48s_v4", VCPUs: 48, MemoryGiB: 192, Family: "Dsv4-Series"},
	"Standard_D64s_v4": {Size: "Standard_D64s_v4", VCPUs: 64, MemoryGiB: 256, Family: "Dsv4-Series"},

	// === Dsv5-Series (General Purpose - 5th Gen) ===
	"Standard_D2s_v5":  {Size: "Standard_D2s_v5", VCPUs: 2, MemoryGiB: 8, Family: "Dsv5-Series"},
	"Standard_D4s_v5":  {Size: "Standard_D4s_v5", VCPUs: 4, MemoryGiB: 16, Family: "Dsv5-Series"},
	"Standard_D8s_v5":  {Size: "Standard_D8s_v5", VCPUs: 8, MemoryGiB: 32, Family: "Dsv5-Series"},
	"Standard_D16s_v5": {Size: "Standard_D16s_v5", VCPUs: 16, MemoryGiB: 64, Family: "Dsv5-Series"},
	"Standard_D32s_v5": {Size: "Standard_D32s_v5", VCPUs: 32, MemoryGiB: 128, Family: "Dsv5-Series"},
	"Standard_D48s_v5": {Size: "Standard_D48s_v5", VCPUs: 48, MemoryGiB: 192, Family: "Dsv5-Series"},
	"Standard_D64s_v5": {Size: "Standard_D64s_v5", VCPUs: 64, MemoryGiB: 256, Family: "Dsv5-Series"},
	"Standard_D96s_v5": {Size: "Standard_D96s_v5", VCPUs: 96, MemoryGiB: 384, Family: "Dsv5-Series"},

	// === E-Series (Memory Optimized) ===
	"Standard_E2s_v3":  {Size: "Standard_E2s_v3", VCPUs: 2, MemoryGiB: 16, Family: "Esv3-Series"},
	"Standard_E4s_v3":  {Size: "Standard_E4s_v3", VCPUs: 4, MemoryGiB: 32, Family: "Esv3-Series"},
	"Standard_E8s_v3":  {Size: "Standard_E8s_v3", VCPUs: 8, MemoryGiB: 64, Family: "Esv3-Series"},
	"Standard_E16s_v3": {Size: "Standard_E16s_v3", VCPUs: 16, MemoryGiB: 128, Family: "Esv3-Series"},
	"Standard_E20s_v3": {Size: "Standard_E20s_v3", VCPUs: 20, MemoryGiB: 160, Family: "Esv3-Series"},
	"Standard_E32s_v3": {Size: "Standard_E32s_v3", VCPUs: 32, MemoryGiB: 256, Family: "Esv3-Series"},
	"Standard_E48s_v3": {Size: "Standard_E48s_v3", VCPUs: 48, MemoryGiB: 384, Family: "Esv3-Series"},
	"Standard_E64s_v3": {Size: "Standard_E64s_v3", VCPUs: 64, MemoryGiB: 432, Family: "Esv3-Series"},

	// === Esv4-Series (Memory Optimized - Latest) ===
	"Standard_E2s_v4":  {Size: "Standard_E2s_v4", VCPUs: 2, MemoryGiB: 16, Family: "Esv4-Series"},
	"Standard_E4s_v4":  {Size: "Standard_E4s_v4", VCPUs: 4, MemoryGiB: 32, Family: "Esv4-Series"},
	"Standard_E8s_v4":  {Size: "Standard_E8s_v4", VCPUs: 8, MemoryGiB: 64, Family: "Esv4-Series"},
	"Standard_E16s_v4": {Size: "Standard_E16s_v4", VCPUs: 16, MemoryGiB: 128, Family: "Esv4-Series"},
	"Standard_E20s_v4": {Size: "Standard_E20s_v4", VCPUs: 20, MemoryGiB: 160, Family: "Esv4-Series"},
	"Standard_E32s_v4": {Size: "Standard_E32s_v4", VCPUs: 32, MemoryGiB: 256, Family: "Esv4-Series"},
	"Standard_E48s_v4": {Size: "Standard_E48s_v4", VCPUs: 48, MemoryGiB: 384, Family: "Esv4-Series"},
	"Standard_E64s_v4": {Size: "Standard_E64s_v4", VCPUs: 64, MemoryGiB: 504, Family: "Esv4-Series"},

	// === Esv5-Series (Memory Optimized - 5th Gen) ===
	"Standard_E2s_v5":  {Size: "Standard_E2s_v5", VCPUs: 2, MemoryGiB: 16, Family: "Esv5-Series"},
	"Standard_E4s_v5":  {Size: "Standard_E4s_v5", VCPUs: 4, MemoryGiB: 32, Family: "Esv5-Series"},
	"Standard_E8s_v5":  {Size: "Standard_E8s_v5", VCPUs: 8, MemoryGiB: 64, Family: "Esv5-Series"},
	"Standard_E16s_v5": {Size: "Standard_E16s_v5", VCPUs: 16, MemoryGiB: 128, Family: "Esv5-Series"},
	"Standard_E20s_v5": {Size: "Standard_E20s_v5", VCPUs: 20, MemoryGiB: 160, Family: "Esv5-Series"},
	"Standard_E32s_v5": {Size: "Standard_E32s_v5", VCPUs: 32, MemoryGiB: 256, Family: "Esv5-Series"},
	"Standard_E48s_v5": {Size: "Standard_E48s_v5", VCPUs: 48, MemoryGiB: 384, Family: "Esv5-Series"},
	"Standard_E64s_v5": {Size: "Standard_E64s_v5", VCPUs: 64, MemoryGiB: 512, Family: "Esv5-Series"},
	"Standard_E96s_v5": {Size: "Standard_E96s_v5", VCPUs: 96, MemoryGiB: 672, Family: "Esv5-Series"},

	// === F-Series (Compute Optimized) ===
	"Standard_F2s_v2":  {Size: "Standard_F2s_v2", VCPUs: 2, MemoryGiB: 4, Family: "Fsv2-Series"},
	"Standard_F4s_v2":  {Size: "Standard_F4s_v2", VCPUs: 4, MemoryGiB: 8, Family: "Fsv2-Series"},  // ✅ CORRETO
	"Standard_F8s_v2":  {Size: "Standard_F8s_v2", VCPUs: 8, MemoryGiB: 16, Family: "Fsv2-Series"},
	"Standard_F16s_v2": {Size: "Standard_F16s_v2", VCPUs: 16, MemoryGiB: 32, Family: "Fsv2-Series"},
	"Standard_F32s_v2": {Size: "Standard_F32s_v2", VCPUs: 32, MemoryGiB: 64, Family: "Fsv2-Series"},
	"Standard_F48s_v2": {Size: "Standard_F48s_v2", VCPUs: 48, MemoryGiB: 96, Family: "Fsv2-Series"},
	"Standard_F64s_v2": {Size: "Standard_F64s_v2", VCPUs: 64, MemoryGiB: 128, Family: "Fsv2-Series"},
	"Standard_F72s_v2": {Size: "Standard_F72s_v2", VCPUs: 72, MemoryGiB: 144, Family: "Fsv2-Series"},

	// === L-Series (Storage Optimized) ===
	"Standard_L4s":  {Size: "Standard_L4s", VCPUs: 4, MemoryGiB: 32, Family: "L-Series"},
	"Standard_L8s":  {Size: "Standard_L8s", VCPUs: 8, MemoryGiB: 64, Family: "L-Series"},
	"Standard_L16s": {Size: "Standard_L16s", VCPUs: 16, MemoryGiB: 128, Family: "L-Series"},
	"Standard_L32s": {Size: "Standard_L32s", VCPUs: 32, MemoryGiB: 256, Family: "L-Series"},

	// === Lsv2-Series (Storage Optimized - v2) ===
	"Standard_L8s_v2":  {Size: "Standard_L8s_v2", VCPUs: 8, MemoryGiB: 64, Family: "Lsv2-Series"},
	"Standard_L16s_v2": {Size: "Standard_L16s_v2", VCPUs: 16, MemoryGiB: 128, Family: "Lsv2-Series"},
	"Standard_L32s_v2": {Size: "Standard_L32s_v2", VCPUs: 32, MemoryGiB: 256, Family: "Lsv2-Series"},
	"Standard_L48s_v2": {Size: "Standard_L48s_v2", VCPUs: 48, MemoryGiB: 384, Family: "Lsv2-Series"},
	"Standard_L64s_v2": {Size: "Standard_L64s_v2", VCPUs: 64, MemoryGiB: 512, Family: "Lsv2-Series"},
	"Standard_L80s_v2": {Size: "Standard_L80s_v2", VCPUs: 80, MemoryGiB: 640, Family: "Lsv2-Series"},

	// === M-Series (Memory Optimized - Large) ===
	"Standard_M8ms":   {Size: "Standard_M8ms", VCPUs: 8, MemoryGiB: 219, Family: "M-Series"},    // 218.75 arredondado
	"Standard_M16ms":  {Size: "Standard_M16ms", VCPUs: 16, MemoryGiB: 438, Family: "M-Series"},  // 437.5 arredondado
	"Standard_M32ms":  {Size: "Standard_M32ms", VCPUs: 32, MemoryGiB: 875, Family: "M-Series"},
	"Standard_M64ms":  {Size: "Standard_M64ms", VCPUs: 64, MemoryGiB: 1750, Family: "M-Series"},
	"Standard_M128ms": {Size: "Standard_M128ms", VCPUs: 128, MemoryGiB: 3800, Family: "M-Series"},

	// === NC-Series (GPU - Compute) ===
	"Standard_NC6":  {Size: "Standard_NC6", VCPUs: 6, MemoryGiB: 56, Family: "NC-Series"},
	"Standard_NC12": {Size: "Standard_NC12", VCPUs: 12, MemoryGiB: 112, Family: "NC-Series"},
	"Standard_NC24": {Size: "Standard_NC24", VCPUs: 24, MemoryGiB: 224, Family: "NC-Series"},

	// === NCv3-Series (GPU - V100) ===
	"Standard_NC6s_v3":  {Size: "Standard_NC6s_v3", VCPUs: 6, MemoryGiB: 112, Family: "NCv3-Series"},
	"Standard_NC12s_v3": {Size: "Standard_NC12s_v3", VCPUs: 12, MemoryGiB: 224, Family: "NCv3-Series"},
	"Standard_NC24s_v3": {Size: "Standard_NC24s_v3", VCPUs: 24, MemoryGiB: 448, Family: "NCv3-Series"},

	// === NV-Series (GPU - Visualization) ===
	"Standard_NV6":  {Size: "Standard_NV6", VCPUs: 6, MemoryGiB: 56, Family: "NV-Series"},
	"Standard_NV12": {Size: "Standard_NV12", VCPUs: 12, MemoryGiB: 112, Family: "NV-Series"},
	"Standard_NV24": {Size: "Standard_NV24", VCPUs: 24, MemoryGiB: 224, Family: "NV-Series"},
}

// GetVMSpecs retorna as especificações de uma VM Azure pelo tamanho
// Se não encontrar no mapeamento, retorna nil
func GetVMSpecs(vmSize string) *VMSpec {
	if spec, exists := azureVMSpecs[vmSize]; exists {
		return &spec
	}
	return nil
}

// GetAllVMSpecs retorna uma cópia de todos os VMSpecs conhecidos.
// Usado para análise de SKUs alternativos sem acessar o mapa diretamente.
func GetAllVMSpecs() map[string]VMSpec {
	result := make(map[string]VMSpec, len(azureVMSpecs))
	for k, v := range azureVMSpecs {
		result[k] = v
	}
	return result
}
