package engine

import (
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// TimeSlotManager gerencia distribuição temporal de clusters em janelas
type TimeSlotManager struct {
	clusters     []string      // Lista de clusters a escanear
	totalSlots   int           // Total de slots (N clusters / 2)
	slotDuration time.Duration // Duração de cada slot (dinâmica: 15-30s)

	// Estado atual
	currentSlot int       // Slot atual (0-based)
	slotStart   time.Time // Quando slot atual começou

	mu sync.RWMutex
}

// SlotAssignment representa atribuição de clusters para um slot
type SlotAssignment struct {
	SlotIndex int       // Índice do slot (0-based)
	StartTime time.Time // Quando slot começou
	EndTime   time.Time // Quando slot termina

	// Clusters ativos neste slot
	Port55553Cluster string // Cluster usando porta 55553 (índice ímpar)
	Port55554Cluster string // Cluster usando porta 55554 (índice par)
}

// NewTimeSlotManager cria gerenciador de time slots
func NewTimeSlotManager(clusters []string) *TimeSlotManager {
	if len(clusters) == 0 {
		log.Warn().Msg("TimeSlotManager criado sem clusters")
		return &TimeSlotManager{
			clusters:     []string{},
			totalSlots:   0,
			slotDuration: 30 * time.Second,
			currentSlot:  0,
			slotStart:    time.Now(),
		}
	}

	// Calcula total de slots (2 clusters por slot)
	totalSlots := (len(clusters) + 1) / 2 // Arredonda para cima

	// Calcula duração dinâmica do slot
	// - 2 clusters (1 slot): 30s
	// - 4 clusters (2 slots): 20s cada
	// - 6 clusters (3 slots): 15s cada
	slotDuration := calculateSlotDuration(len(clusters))

	tsm := &TimeSlotManager{
		clusters:     clusters,
		totalSlots:   totalSlots,
		slotDuration: slotDuration,
		currentSlot:  0,
		slotStart:    time.Now(),
	}

	log.Info().
		Strs("clusters", clusters).
		Int("total_slots", totalSlots).
		Dur("slot_duration", slotDuration).
		Msg("TimeSlotManager inicializado")

	return tsm
}

// calculateSlotDuration calcula duração dinâmica baseado em número de clusters
func calculateSlotDuration(clusterCount int) time.Duration {
	switch {
	case clusterCount <= 2:
		return 30 * time.Second
	case clusterCount <= 4:
		return 20 * time.Second
	case clusterCount <= 6:
		return 15 * time.Second
	default:
		// Para muitos clusters, mantém mínimo de 15s
		return 15 * time.Second
	}
}

// GetCurrentAssignment retorna atribuição do slot atual
func (tsm *TimeSlotManager) GetCurrentAssignment() SlotAssignment {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()

	if len(tsm.clusters) == 0 {
		return SlotAssignment{
			SlotIndex:        0,
			StartTime:        time.Now(),
			EndTime:          time.Now().Add(30 * time.Second),
			Port55553Cluster: "",
			Port55554Cluster: "",
		}
	}

	// Calcula slot atual baseado no tempo decorrido
	elapsed := time.Since(tsm.slotStart)
	currentSlotIndex := int(elapsed / tsm.slotDuration)

	// Loop circular (volta ao slot 0 após último)
	currentSlotIndex = currentSlotIndex % tsm.totalSlots

	// Calcula timestamps do slot
	slotStartTime := tsm.slotStart.Add(time.Duration(currentSlotIndex) * tsm.slotDuration)
	slotEndTime := slotStartTime.Add(tsm.slotDuration)

	// Atribui clusters para este slot
	// Slot 0: clusters[0] (55553) e clusters[1] (55554)
	// Slot 1: clusters[2] (55553) e clusters[3] (55554)
	// Slot 2: clusters[4] (55553) e clusters[5] (55554)
	port55553Index := currentSlotIndex * 2
	port55554Index := port55553Index + 1

	var port55553Cluster, port55554Cluster string

	if port55553Index < len(tsm.clusters) {
		port55553Cluster = tsm.clusters[port55553Index]
	}

	if port55554Index < len(tsm.clusters) {
		port55554Cluster = tsm.clusters[port55554Index]
	}

	return SlotAssignment{
		SlotIndex:        currentSlotIndex,
		StartTime:        slotStartTime,
		EndTime:          slotEndTime,
		Port55553Cluster: port55553Cluster,
		Port55554Cluster: port55554Cluster,
	}
}

// GetTimeUntilNextSlot retorna quanto tempo falta para próximo slot
func (tsm *TimeSlotManager) GetTimeUntilNextSlot() time.Duration {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()

	if len(tsm.clusters) == 0 {
		return 30 * time.Second
	}

	elapsed := time.Since(tsm.slotStart)
	currentSlotElapsed := elapsed % tsm.slotDuration
	return tsm.slotDuration - currentSlotElapsed
}

// GetAllSlots retorna informação de todos os slots
func (tsm *TimeSlotManager) GetAllSlots() []SlotAssignment {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()

	slots := make([]SlotAssignment, 0, tsm.totalSlots)

	for i := 0; i < tsm.totalSlots; i++ {
		slotStartTime := tsm.slotStart.Add(time.Duration(i) * tsm.slotDuration)
		slotEndTime := slotStartTime.Add(tsm.slotDuration)

		port55553Index := i * 2
		port55554Index := port55553Index + 1

		var port55553Cluster, port55554Cluster string

		if port55553Index < len(tsm.clusters) {
			port55553Cluster = tsm.clusters[port55553Index]
		}

		if port55554Index < len(tsm.clusters) {
			port55554Cluster = tsm.clusters[port55554Index]
		}

		slots = append(slots, SlotAssignment{
			SlotIndex:        i,
			StartTime:        slotStartTime,
			EndTime:          slotEndTime,
			Port55553Cluster: port55553Cluster,
			Port55554Cluster: port55554Cluster,
		})
	}

	return slots
}

// UpdateClusters atualiza lista de clusters e recalcula slots
func (tsm *TimeSlotManager) UpdateClusters(clusters []string) {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()

	tsm.clusters = clusters
	tsm.totalSlots = (len(clusters) + 1) / 2
	tsm.slotDuration = calculateSlotDuration(len(clusters))
	tsm.slotStart = time.Now() // Reinicia contagem
	tsm.currentSlot = 0

	log.Info().
		Strs("clusters", clusters).
		Int("total_slots", tsm.totalSlots).
		Dur("slot_duration", tsm.slotDuration).
		Msg("TimeSlotManager atualizado com novos clusters")
}

// GetConfig retorna configuração atual
func (tsm *TimeSlotManager) GetConfig() (clusters []string, totalSlots int, slotDuration time.Duration) {
	tsm.mu.RLock()
	defer tsm.mu.RUnlock()

	return tsm.clusters, tsm.totalSlots, tsm.slotDuration
}
