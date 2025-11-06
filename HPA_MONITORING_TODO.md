# TODO: Sistema de Monitoramento HPA com Prometheus

**Data:** 05 de novembro de 2025
**Status:** 🔴 Aguardando Implementação
**Branch:** k8s-hpa-watchdog-integration-dev

---

## 🎯 Objetivo Geral

Implementar sistema de monitoramento de HPAs em tempo real seguindo o fluxo:
1. **Port-forward Prometheus** → namespace "monitoring" porta 9090
2. **Coleta histórica** → 3 dias de dados do HPA via Prometheus
3. **Persistência SQLite** → Salvar baseline histórico
4. **Monitoramento real** → Comparação com baseline para detecção de anomalias

---

## 📋 Fluxo Detalhado Definido

### 1. Inicialização do Port-Forward
```
Usuário adiciona HPA → 
Port-forward prometheus:9090 (namespace monitoring) → 
Verificar conectividade Prometheus → 
Atribuir porta (55553 ou 55554) ao cluster
```

### 2. Coleta Histórica Obrigatória
```
HPA adicionado → 
Query Prometheus últimos 3 dias → 
Processar ~864 snapshots (step 5min) → 
Salvar no SQLite → 
Baseline estabelecido
```

### 3. Monitoramento Contínuo
```
Baseline pronto → 
Scan a cada 1 minuto → 
Comparar com baseline → 
Detectar anomalias → 
Salvar novos dados
```

---

## 🏗️ Arquitetura de Implementação

### Componente 1: Port-Forward Manager Refinado

**Localização:** `internal/monitoring/portforward/manager.go`

**Funcionalidades:**
- ✅ Port-forward para `prometheus-server:9090` (namespace monitoring)
- ✅ Duas portas simultâneas: 55553 (ímpar) e 55554 (par)
- ✅ Atribuição fixa de porta por cluster (sem alternância durante execução)
- ✅ Port-forwards persistentes (criados uma vez, mantidos até shutdown)
- ✅ Cleanup garantido no shutdown da aplicação

**Estrutura:**
```go
type PortForwardManager struct {
    forwards      map[string]*PortForward  // cluster → port-forward
    clusterPorts  map[string]int           // cluster → porta (55553 ou 55554)
    portUsage     map[int][]string         // porta → lista de clusters
    mu            sync.RWMutex
}
```

### Componente 2: Historical Data Collector

**Localização:** `internal/monitoring/collector/historical.go`

**Funcionalidades:**
- ✅ Query Prometheus para range de 3 dias
- ✅ Step de 5 minutos (864 snapshots por HPA)
- ✅ Métricas coletadas: CPU, Memory, Replicas
- ✅ Validação de dados (mínimo 70% de cobertura)
- ✅ Salvamento direto no SQLite

**Queries Prometheus:**
```promql
# CPU utilization (últimos 3 dias)
avg(rate(container_cpu_usage_seconds_total{namespace="$NAMESPACE",pod=~"$HPA.*"}[5m])) * 100

# Memory utilization (últimos 3 dias) 
avg(container_memory_working_set_bytes{namespace="$NAMESPACE",pod=~"$HPA.*"}) / 1024 / 1024

# Replica count (últimos 3 dias)
kube_deployment_status_replicas{namespace="$NAMESPACE",deployment="$HPA"}
```

### Componente 3: Scan Engine com Fila Alternada

**Localização:** `internal/monitoring/engine/scanner.go`

**Funcionalidades:**
- ✅ Fila alternada entre portas 55553 e 55554
- ✅ Port-forwards reutilizados (não recriados)
- ✅ Scan a cada 1 minuto após baseline estabelecido
- ✅ Detecção de port-forward inativo com reconexão automática

**Lógica de Alternância:**
```go
type ScanQueue struct {
    port55553Clusters []string
    port55554Clusters []string
    currentIndex      int
    mu               sync.RWMutex
}

// GetNext retorna próximo cluster alternando portas
func (sq *ScanQueue) GetNext() (cluster string, port int) {
    // Scan 1: cluster da porta 55553
    // Scan 2: cluster da porta 55554  
    // Scan 3: próximo cluster da porta 55553
    // ...
}
```

### Componente 4: SQLite Persistence Layer

**Localização:** `internal/monitoring/storage/sqlite.go`

**Funcionalidades:**
- ✅ Schema otimizado para time-series data
- ✅ Índices para queries rápidas por HPA
- ✅ Cleanup automático (dados > 30 dias)
- ✅ Transações batch para performance

**Schema:**
```sql
CREATE TABLE hpa_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cluster TEXT NOT NULL,
    namespace TEXT NOT NULL,
    hpa_name TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    cpu_current REAL,
    memory_current REAL,
    replicas_current INTEGER,
    cpu_target REAL,
    memory_target REAL,
    replicas_min INTEGER,
    replicas_max INTEGER,
    is_baseline BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(cluster, namespace, hpa_name, timestamp)
);

CREATE INDEX idx_hpa_time ON hpa_snapshots(cluster, namespace, hpa_name, timestamp);
CREATE INDEX idx_baseline ON hpa_snapshots(is_baseline, timestamp);
```

---

## 📝 Implementação Detalhada por Fase

### Fase 1: Port-Forward Manager Persistente

**Objetivo:** Port-forwards criados uma vez e mantidos até shutdown

#### 1.1. Criar PortForwardManager Refinado
```go
// internal/monitoring/portforward/manager.go

type Config struct {
    PrometheusNamespace string // "monitoring"
    PrometheusService   string // "prometheus-server"  
    PrometheusPort      int    // 9090
    LocalPortOdd       int    // 55553
    LocalPortEven      int    // 55554
}

type PortForwardManager struct {
    config       Config
    forwards     map[string]*PortForward
    clusterPorts map[string]int
    portUsage    map[int][]string
    mu           sync.RWMutex
}

// StartForCluster inicia port-forward persistente para um cluster
func (m *PortForwardManager) StartForCluster(cluster string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    // Se já existe, verifica se está ativo
    if pf, exists := m.forwards[cluster]; exists {
        if pf.IsHealthy() {
            return nil // Já ativo
        }
        // Se não está saudável, remove e recria
        pf.Stop()
        delete(m.forwards, cluster)
    }
    
    // Determina porta baseada em quantidade de clusters
    port := m.assignPort(cluster)
    
    // Cria port-forward para prometheus-server:9090
    pf := NewPortForward(PortForwardConfig{
        Cluster:         cluster + "-admin", // Contexto kubectl
        Namespace:       m.config.PrometheusNamespace,
        Service:         m.config.PrometheusService,
        ServicePort:     m.config.PrometheusPort,
        LocalPort:      port,
    })
    
    if err := pf.Start(); err != nil {
        return fmt.Errorf("falha ao iniciar port-forward para %s: %w", cluster, err)
    }
    
    m.forwards[cluster] = pf
    m.clusterPorts[cluster] = port
    
    return nil
}

// assignPort atribui porta baseada em quantidade de clusters já ativos
func (m *PortForwardManager) assignPort(cluster string) int {
    // Conta clusters em cada porta
    oddCount := len(m.portUsage[m.config.LocalPortOdd])
    evenCount := len(m.portUsage[m.config.LocalPortEven])
    
    // Atribui à porta com menos clusters
    if oddCount <= evenCount {
        m.portUsage[m.config.LocalPortOdd] = append(m.portUsage[m.config.LocalPortOdd], cluster)
        return m.config.LocalPortOdd
    } else {
        m.portUsage[m.config.LocalPortEven] = append(m.portUsage[m.config.LocalPortEven], cluster)
        return m.config.LocalPortEven
    }
}

// StopAll para todos os port-forwards (chamado no shutdown)
func (m *PortForwardManager) StopAll() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    var errors []error
    
    for cluster, pf := range m.forwards {
        if err := pf.Stop(); err != nil {
            errors = append(errors, fmt.Errorf("erro ao parar %s: %w", cluster, err))
        }
    }
    
    // Limpa mapas
    m.forwards = make(map[string]*PortForward)
    m.clusterPorts = make(map[string]int)
    m.portUsage = map[int][]string{
        m.config.LocalPortOdd:  {},
        m.config.LocalPortEven: {},
    }
    
    if len(errors) > 0 {
        return fmt.Errorf("erros ao parar port-forwards: %v", errors)
    }
    
    return nil
}

// GetPrometheusURL retorna URL do Prometheus para um cluster
func (m *PortForwardManager) GetPrometheusURL(cluster string) string {
    m.mu.RLock()
    defer m.mu.RUnlock()
    
    port, exists := m.clusterPorts[cluster]
    if !exists {
        return ""
    }
    
    return fmt.Sprintf("http://localhost:%d", port)
}
```

#### 1.2. Implementar PortForward Individual
```go
// internal/monitoring/portforward/portforward.go

type PortForward struct {
    cluster     string
    namespace   string
    service     string
    servicePort int
    localPort   int
    cmd         *exec.Cmd
    mu          sync.RWMutex
    running     bool
}

func (pf *PortForward) Start() error {
    pf.mu.Lock()
    defer pf.mu.Unlock()
    
    if pf.running {
        return nil
    }
    
    // Comando kubectl port-forward
    args := []string{
        "--context", pf.cluster,
        "-n", pf.namespace,
        "port-forward",
        fmt.Sprintf("service/%s", pf.service),
        fmt.Sprintf("%d:%d", pf.localPort, pf.servicePort),
    }
    
    pf.cmd = exec.Command("kubectl", args...)
    
    // Redireciona saída para evitar spam no console
    pf.cmd.Stdout = nil
    pf.cmd.Stderr = nil
    
    if err := pf.cmd.Start(); err != nil {
        return fmt.Errorf("falha ao executar kubectl port-forward: %w", err)
    }
    
    // Aguarda 2 segundos para port-forward estabelecer
    time.Sleep(2 * time.Second)
    
    // Verifica se processo ainda está rodando
    if pf.cmd.Process == nil {
        return fmt.Errorf("processo port-forward morreu imediatamente")
    }
    
    pf.running = true
    return nil
}

func (pf *PortForward) IsHealthy() bool {
    pf.mu.RLock()
    defer pf.mu.RUnlock()
    
    if !pf.running || pf.cmd == nil || pf.cmd.Process == nil {
        return false
    }
    
    // Verifica se processo ainda existe
    err := pf.cmd.Process.Signal(syscall.Signal(0))
    return err == nil
}

func (pf *PortForward) Stop() error {
    pf.mu.Lock()
    defer pf.mu.Unlock()
    
    if !pf.running || pf.cmd == nil || pf.cmd.Process == nil {
        return nil
    }
    
    // Mata processo kubectl
    if err := pf.cmd.Process.Kill(); err != nil {
        return fmt.Errorf("falha ao matar processo: %w", err)
    }
    
    // Aguarda processo terminar
    pf.cmd.Wait()
    
    pf.running = false
    pf.cmd = nil
    
    return nil
}
```

### Fase 2: Historical Data Collector

**Objetivo:** Coletar 3 dias de dados antes de iniciar monitoramento

#### 2.1. Implementar Collector Histórico
```go
// internal/monitoring/collector/historical.go

type HistoricalCollector struct {
    prometheusURL string
    client        *http.Client
    storage       *storage.SQLite
}

// CollectHistoricalData coleta dados históricos de 3 dias
func (hc *HistoricalCollector) CollectHistoricalData(ctx context.Context, cluster, namespace, hpaName string) error {
    log.Info().
        Str("cluster", cluster).
        Str("namespace", namespace).
        Str("hpa", hpaName).
        Msg("Iniciando coleta histórica de 3 dias")
    
    // Configuração temporal
    endTime := time.Now()
    startTime := endTime.Add(-72 * time.Hour) // 3 dias
    step := 5 * time.Minute                   // Step de 5 minutos
    
    // Queries Prometheus
    queries := map[string]string{
        "cpu_usage": fmt.Sprintf(
            `avg(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*"}[5m])) * 100`,
            namespace, hpaName,
        ),
        "memory_usage": fmt.Sprintf(
            `avg(container_memory_working_set_bytes{namespace="%s",pod=~"%s-.*"}) / 1024 / 1024`,
            namespace, hpaName,
        ),
        "replica_count": fmt.Sprintf(
            `kube_deployment_status_replicas{namespace="%s",deployment="%s"}`,
            namespace, hpaName,
        ),
    }
    
    snapshots := make([]*models.HPASnapshot, 0, 864) // ~864 snapshots esperados
    
    // Itera por timestamps (step de 5min)
    for ts := startTime; ts.Before(endTime); ts = ts.Add(step) {
        snapshot := &models.HPASnapshot{
            Cluster:     cluster,
            Namespace:   namespace,
            HPAName:     hpaName,
            Timestamp:   ts,
            IsBaseline:  true, // Marca como dados de baseline
        }
        
        // Executa queries para este timestamp
        for metricName, query := range queries {
            value, err := hc.queryPrometheusAtTime(ctx, query, ts)
            if err != nil {
                log.Debug().
                    Err(err).
                    Str("metric", metricName).
                    Time("timestamp", ts).
                    Msg("Falha ao coletar métrica histórica")
                continue
            }
            
            // Preenche snapshot baseado na métrica
            switch metricName {
            case "cpu_usage":
                snapshot.CPUCurrent = &value
            case "memory_usage":
                snapshot.MemoryCurrent = &value
            case "replica_count":
                replicaCount := int32(value)
                snapshot.ReplicasCurrent = &replicaCount
            }
        }
        
        snapshots = append(snapshots, snapshot)
    }
    
    // Validação de cobertura mínima
    validSnapshots := 0
    for _, s := range snapshots {
        if s.CPUCurrent != nil || s.MemoryCurrent != nil || s.ReplicasCurrent != nil {
            validSnapshots++
        }
    }
    
    coverage := float64(validSnapshots) / float64(len(snapshots)) * 100
    if coverage < 70.0 {
        return fmt.Errorf("cobertura de dados insuficiente: %.1f%% (mínimo 70%%)", coverage)
    }
    
    // Salva no SQLite em batch
    if err := hc.storage.SaveSnapshotsBatch(snapshots); err != nil {
        return fmt.Errorf("falha ao salvar snapshots: %w", err)
    }
    
    log.Info().
        Str("cluster", cluster).
        Str("namespace", namespace).
        Str("hpa", hpaName).
        Int("snapshots", len(snapshots)).
        Float64("coverage", coverage).
        Msg("Coleta histórica concluída com sucesso")
    
    return nil
}

// queryPrometheusAtTime executa query Prometheus para um timestamp específico
func (hc *HistoricalCollector) queryPrometheusAtTime(ctx context.Context, query string, timestamp time.Time) (float64, error) {
    // Constrói URL da query
    params := url.Values{}
    params.Add("query", query)
    params.Add("time", strconv.FormatInt(timestamp.Unix(), 10))
    
    reqURL := fmt.Sprintf("%s/api/v1/query?%s", hc.prometheusURL, params.Encode())
    
    // Executa request HTTP
    req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
    if err != nil {
        return 0, err
    }
    
    resp, err := hc.client.Do(req)
    if err != nil {
        return 0, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
        return 0, fmt.Errorf("prometheus retornou status %d", resp.StatusCode)
    }
    
    // Parse resposta JSON
    var promResp PrometheusResponse
    if err := json.NewDecoder(resp.Body).Decode(&promResp); err != nil {
        return 0, err
    }
    
    if promResp.Status != "success" {
        return 0, fmt.Errorf("prometheus erro: %s", promResp.Error)
    }
    
    if len(promResp.Data.Result) == 0 {
        return 0, fmt.Errorf("nenhum resultado encontrado")
    }
    
    // Extrai valor numérico
    valueStr := promResp.Data.Result[0].Value[1].(string)
    value, err := strconv.ParseFloat(valueStr, 64)
    if err != nil {
        return 0, fmt.Errorf("valor inválido: %s", valueStr)
    }
    
    return value, nil
}

type PrometheusResponse struct {
    Status string `json:"status"`
    Data   struct {
        Result []struct {
            Value []interface{} `json:"value"`
        } `json:"result"`
    } `json:"data"`
    Error string `json:"error,omitempty"`
}
```

### Fase 3: Time Slot Manager com Janelas Paralelas

**Objetivo:** Alternância temporal de clusters com 2 portas simultâneas

#### 3.1. Implementar TimeSlotManager
```go
// internal/monitoring/engine/timeslot.go

type TimeSlotManager struct {
    clusters     []string
    slotDuration time.Duration    // Duração de cada janela (15-30s)
    totalSlots   int             // ceil(clusters/2) 
    currentSlot  int             // Janela atual (0-based)
    slotStart    time.Time       // Quando janela atual começou
    cycleStart   time.Time       // Quando ciclo completo começou
    mu           sync.RWMutex
}

type SlotAssignment struct {
    Port55553Cluster string        // Cluster na porta ímpar
    Port55554Cluster string        // Cluster na porta par
    SlotIndex        int           // Índice da janela (0-based)
    StartTime        time.Time     // Início desta janela
    EndTime          time.Time     // Fim desta janela
    IsActive         bool          // Se esta janela está ativa
}

func NewTimeSlotManager(clusters []string) *TimeSlotManager {
    // Calcula duração otimizada da janela
    slotDuration := calculateOptimalSlotDuration(len(clusters))
    
    return &TimeSlotManager{
        clusters:     clusters,
        slotDuration: slotDuration,
        totalSlots:   int(math.Ceil(float64(len(clusters)) / 2.0)),
        currentSlot:  0,
        slotStart:    time.Now(),
        cycleStart:   time.Now(),
    }
}

// calculateOptimalSlotDuration calcula duração ideal baseada no número de clusters
func calculateOptimalSlotDuration(clusterCount int) time.Duration {
    switch {
    case clusterCount <= 2:
        return 30 * time.Second  // 1 janela de 30s
    case clusterCount <= 4:
        return 25 * time.Second  // 2 janelas de 25s = 50s ciclo
    case clusterCount <= 6:
        return 20 * time.Second  // 3 janelas de 20s = 60s ciclo
    case clusterCount <= 8:
        return 15 * time.Second  // 4 janelas de 15s = 60s ciclo
    default:
        // Para muitos clusters, mantém janelas de 15s
        return 15 * time.Second
    }
}

// GetCurrentAssignment retorna clusters atualmente atribuídos às portas
func (tsm *TimeSlotManager) GetCurrentAssignment() SlotAssignment {
    tsm.mu.Lock()
    defer tsm.mu.Unlock()
    
    now := time.Now()
    elapsed := now.Sub(tsm.slotStart)
    
    // Verifica se precisa avançar para próxima janela
    if elapsed >= tsm.slotDuration {
        tsm.advanceSlot(now)
    }
    
    // Calcula clusters da janela atual
    return tsm.calculateSlotAssignment(tsm.currentSlot, now)
}

// advanceSlot avança para próxima janela temporalmente
func (tsm *TimeSlotManager) advanceSlot(now time.Time) {
    // Avança para próxima janela
    tsm.currentSlot = (tsm.currentSlot + 1) % tsm.totalSlots
    tsm.slotStart = now
    
    // Se voltou ao slot 0, reinicia ciclo
    if tsm.currentSlot == 0 {
        tsm.cycleStart = now
        log.Info().
            Dur("cycle_duration", now.Sub(tsm.cycleStart)).
            Int("total_slots", tsm.totalSlots).
            Msg("Ciclo completo de slots finalizado, reiniciando")
    }
    
    log.Debug().
        Int("slot", tsm.currentSlot).
        Time("slot_start", tsm.slotStart).
        Msg("Avançado para próxima janela temporal")
}

// calculateSlotAssignment calcula quais clusters estão ativos em uma janela
func (tsm *TimeSlotManager) calculateSlotAssignment(slotIndex int, now time.Time) SlotAssignment {
    assignment := SlotAssignment{
        SlotIndex: slotIndex,
        StartTime: tsm.slotStart,
        EndTime:   tsm.slotStart.Add(tsm.slotDuration),
        IsActive:  true,
    }
    
    // Calcula índices dos clusters para esta janela
    cluster1Index := (slotIndex * 2) % len(tsm.clusters)
    cluster2Index := (slotIndex*2 + 1) % len(tsm.clusters)
    
    // Atribui clusters às portas
    assignment.Port55553Cluster = tsm.clusters[cluster1Index]
    
    // Se há cluster suficiente para segunda porta
    if cluster2Index < len(tsm.clusters) {
        assignment.Port55554Cluster = tsm.clusters[cluster2Index]
    } else {
        // Número ímpar de clusters: segunda porta fica vazia nesta janela
        assignment.Port55554Cluster = ""
    }
    
    return assignment
}

// GetAllSlots retorna todos os slots do ciclo atual para análise
func (tsm *TimeSlotManager) GetAllSlots() []SlotAssignment {
    tsm.mu.RLock()
    defer tsm.mu.RUnlock()
    
    slots := make([]SlotAssignment, tsm.totalSlots)
    baseTime := tsm.cycleStart
    
    for i := 0; i < tsm.totalSlots; i++ {
        slotStart := baseTime.Add(time.Duration(i) * tsm.slotDuration)
        
        slots[i] = SlotAssignment{
            SlotIndex: i,
            StartTime: slotStart,
            EndTime:   slotStart.Add(tsm.slotDuration),
            IsActive:  i == tsm.currentSlot,
        }
        
        // Calcula clusters para este slot
        cluster1Index := (i * 2) % len(tsm.clusters)
        cluster2Index := (i*2 + 1) % len(tsm.clusters)
        
        slots[i].Port55553Cluster = tsm.clusters[cluster1Index]
        if cluster2Index < len(tsm.clusters) {
            slots[i].Port55554Cluster = tsm.clusters[cluster2Index]
        }
    }
    
    return slots
}

// GetTimeUntilNextSlot retorna tempo restante até próxima janela
func (tsm *TimeSlotManager) GetTimeUntilNextSlot() time.Duration {
    tsm.mu.RLock()
    defer tsm.mu.RUnlock()
    
    elapsed := time.Since(tsm.slotStart)
    remaining := tsm.slotDuration - elapsed
    
    if remaining < 0 {
        return 0
    }
    
    return remaining
}

// AddCluster adiciona novo cluster ao gerenciador
func (tsm *TimeSlotManager) AddCluster(cluster string) {
    tsm.mu.Lock()
    defer tsm.mu.Unlock()
    
    // Evita duplicatas
    for _, existing := range tsm.clusters {
        if existing == cluster {
            return
        }
    }
    
    tsm.clusters = append(tsm.clusters, cluster)
    
    // Recalcula slots baseado no novo número de clusters
    oldTotalSlots := tsm.totalSlots
    tsm.totalSlots = int(math.Ceil(float64(len(tsm.clusters)) / 2.0))
    tsm.slotDuration = calculateOptimalSlotDuration(len(tsm.clusters))
    
    log.Info().
        Str("cluster", cluster).
        Int("old_slots", oldTotalSlots).
        Int("new_slots", tsm.totalSlots).
        Dur("new_slot_duration", tsm.slotDuration).
        Msg("Cluster adicionado, slots recalculados")
}

// RemoveCluster remove cluster do gerenciador  
func (tsm *TimeSlotManager) RemoveCluster(cluster string) {
    tsm.mu.Lock()
    defer tsm.mu.Unlock()
    
    for i, existing := range tsm.clusters {
        if existing == cluster {
            // Remove cluster da slice
            tsm.clusters = append(tsm.clusters[:i], tsm.clusters[i+1:]...)
            
            // Recalcula slots
            tsm.totalSlots = int(math.Ceil(float64(len(tsm.clusters)) / 2.0))
            tsm.slotDuration = calculateOptimalSlotDuration(len(tsm.clusters))
            
            log.Info().
                Str("cluster", cluster).
                Int("remaining_clusters", len(tsm.clusters)).
                Int("new_slots", tsm.totalSlots).
                Msg("Cluster removido, slots recalculados")
            
            break
        }
    }
}
```

#### 3.2. Modificar ScanEngine com Time Slots
```go
// internal/monitoring/engine/engine.go

type ScanEngine struct {
    config              *Config
    pfManager           *portforward.PortForwardManager
    timeSlotManager     *TimeSlotManager
    storage             *storage.SQLite
    historicalCollector *collector.HistoricalCollector
    
    running             bool
    mu                  sync.RWMutex
    stopChan            chan struct{}
    wg                  sync.WaitGroup
}

// Start inicia engine COM port-forwards persistentes e time slots
func (se *ScanEngine) Start() error {
    se.mu.Lock()
    if se.running {
        se.mu.Unlock()
        return nil
    }
    se.running = true
    se.mu.Unlock()
    
    log.Info().Msg("Iniciando Scan Engine com Time Slots")
    
    // Coleta todos os clusters configurados
    var allClusters []string
    for _, target := range se.config.Targets {
        allClusters = append(allClusters, target.Cluster)
    }
    
    // Inicializa gerenciador de time slots
    se.timeSlotManager = NewTimeSlotManager(allClusters)
    
    log.Info().
        Strs("clusters", allClusters).
        Int("total_slots", se.timeSlotManager.totalSlots).
        Dur("slot_duration", se.timeSlotManager.slotDuration).
        Msg("Time Slot Manager configurado")
    
    // Inicia port-forwards persistentes para TODAS as portas
    for _, cluster := range allClusters {
        log.Info().
            Str("cluster", cluster).
            Msg("Iniciando port-forward persistente")
        
        if err := se.pfManager.StartForCluster(cluster); err != nil {
            log.Error().
                Err(err).
                Str("cluster", cluster).
                Msg("Falha ao iniciar port-forward, continuando...")
            continue
        }
        
        log.Info().
            Str("cluster", cluster).
            Msg("Port-forward persistente ativo")
    }
    
    // Inicia loop de scan baseado em time slots
    se.wg.Add(1)
    go se.timeSlotScanLoop()
    
    log.Info().Msg("Scan Engine iniciado com time slots")
    return nil
}

// Stop para engine E todos os port-forwards
func (se *ScanEngine) Stop() error {
    se.mu.Lock()
    if !se.running {
        se.mu.Unlock()
        return nil
    }
    se.running = false
    se.mu.Unlock()
    
    log.Info().Msg("Parando Scan Engine")
    
    // Sinaliza parada
    close(se.stopChan)
    
    // Aguarda scan loop terminar
    se.wg.Wait()
    
    // Para TODOS os port-forwards
    log.Info().Msg("Parando todos os port-forwards...")
    if err := se.pfManager.StopAll(); err != nil {
        log.Error().Err(err).Msg("Erro ao parar port-forwards")
    } else {
        log.Info().Msg("Port-forwards parados com sucesso")
    }
    
    log.Info().Msg("Scan Engine parado completamente")
    return nil
}

// timeSlotScanLoop executa scans baseado em janelas temporais
func (se *ScanEngine) timeSlotScanLoop() {
    defer se.wg.Done()
    
    // Ticker mais frequente para verificar mudanças de slot
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    
    var lastSlotIndex = -1
    
    for {
        select {
        case <-se.stopChan:
            log.Info().Msg("Time slot scan loop terminado")
            return
            
        case <-ticker.C:
            // Verifica slot atual
            assignment := se.timeSlotManager.GetCurrentAssignment()
            
            // Se mudou de slot, executa scan dos clusters da nova janela
            if assignment.SlotIndex != lastSlotIndex {
                lastSlotIndex = assignment.SlotIndex
                se.executeSlotScan(assignment)
            }
        }
    }
}

// executeSlotScan executa scan dos clusters ativos na janela atual
func (se *ScanEngine) executeSlotScan(assignment SlotAssignment) {
    log.Info().
        Int("slot_index", assignment.SlotIndex).
        Str("port_55553_cluster", assignment.Port55553Cluster).
        Str("port_55554_cluster", assignment.Port55554Cluster).
        Time("slot_end", assignment.EndTime).
        Msg("Executando scan da janela temporal")
    
    // Scan paralelo dos 2 clusters (se ambos existem)
    var wg sync.WaitGroup
    
    // Cluster da porta 55553
    if assignment.Port55553Cluster != "" {
        wg.Add(1)
        go func() {
            defer wg.Done()
            se.scanClusterInSlot(assignment.Port55553Cluster, 55553)
        }()
    }
    
    // Cluster da porta 55554 (se existe)
    if assignment.Port55554Cluster != "" {
        wg.Add(1)
        go func() {
            defer wg.Done()
            se.scanClusterInSlot(assignment.Port55554Cluster, 55554)
        }()
    }
    
    // Aguarda ambos os scans terminarem
    wg.Wait()
    
    timeUntilNext := se.timeSlotManager.GetTimeUntilNextSlot()
    log.Info().
        Int("slot_index", assignment.SlotIndex).
        Dur("time_until_next", timeUntilNext).
        Msg("Scan da janela temporal concluído")
}

// scanClusterInSlot executa scan de um cluster específico em sua janela
func (se *ScanEngine) scanClusterInSlot(cluster string, expectedPort int) {
    // Verifica se port-forward está ativo
    prometheusURL := se.pfManager.GetPrometheusURL(cluster)
    if prometheusURL == "" {
        log.Warn().
            Str("cluster", cluster).
            Int("expected_port", expectedPort).
            Msg("Port-forward não disponível, tentando reconectar...")
        
        // Tenta reconectar
        if err := se.pfManager.StartForCluster(cluster); err != nil {
            log.Error().
                Err(err).
                Str("cluster", cluster).
                Msg("Falha ao reconectar port-forward")
            return
        }
        
        prometheusURL = se.pfManager.GetPrometheusURL(cluster)
        if prometheusURL == "" {
            log.Error().
                Str("cluster", cluster).
                Msg("Port-forward ainda não disponível após reconexão")
            return
        }
    }
    
    log.Debug().
        Str("cluster", cluster).
        Int("port", expectedPort).
        Str("prometheus_url", prometheusURL).
        Msg("Iniciando scan do cluster")
    
    // Executa scan do cluster
    se.scanCluster(cluster, prometheusURL)
}

// scanCluster executa scan de um cluster específico (mantido igual)
func (se *ScanEngine) scanCluster(cluster, prometheusURL string) {
    // Busca HPAs deste cluster no SQLite
    hpas, err := se.storage.GetHPAsByCluster(cluster)
    if err != nil {
        log.Error().
            Err(err).
            Str("cluster", cluster).
            Msg("Falha ao buscar HPAs do cluster")
        return
    }
    
    if len(hpas) == 0 {
        log.Debug().
            Str("cluster", cluster).
            Msg("Nenhum HPA configurado para este cluster")
        return
    }
    
    // Para cada HPA, coleta dados atuais
    for _, hpa := range hpas {
        se.scanHPA(cluster, hpa.Namespace, hpa.Name, prometheusURL)
    }
}

// scanHPA coleta dados atuais de um HPA específico (mantido igual)
func (se *ScanEngine) scanHPA(cluster, namespace, hpaName, prometheusURL string) {
    // Verifica se tem baseline
    hasBaseline, err := se.storage.HasBaseline(cluster, namespace, hpaName)
    if err != nil {
        log.Error().
            Err(err).
            Str("cluster", cluster).
            Str("namespace", namespace).
            Str("hpa", hpaName).
            Msg("Erro ao verificar baseline")
        return
    }
    
    if !hasBaseline {
        log.Debug().
            Str("cluster", cluster).
            Str("namespace", namespace).
            Str("hpa", hpaName).
            Msg("Baseline não disponível, pulando scan")
        return
    }
    
    // Coleta dados atuais do Prometheus
    snapshot, err := se.collectCurrentSnapshot(cluster, namespace, hpaName, prometheusURL)
    if err != nil {
        log.Error().
            Err(err).
            Str("cluster", cluster).
            Str("namespace", namespace).
            Str("hpa", hpaName).
            Msg("Falha ao coletar snapshot atual")
        return
    }
    
    // Salva no SQLite
    if err := se.storage.SaveSnapshot(snapshot); err != nil {
        log.Error().
            Err(err).
            Str("cluster", cluster).
            Str("namespace", namespace).
            Str("hpa", hpaName).
            Msg("Falha ao salvar snapshot")
        return
    }
    
    log.Debug().
        Str("cluster", cluster).
        Str("namespace", namespace).
        Str("hpa", hpaName).
        Msg("Snapshot coletado e salvo")
}

// GetCurrentSlotInfo retorna informações da janela atual para API
func (se *ScanEngine) GetCurrentSlotInfo() SlotAssignment {
    return se.timeSlotManager.GetCurrentAssignment()
}

// GetAllSlotsInfo retorna todas as janelas do ciclo para API  
func (se *ScanEngine) GetAllSlotsInfo() []SlotAssignment {
    return se.timeSlotManager.GetAllSlots()
}
```

### Fase 4: SQLite Storage Layer

**Objetivo:** Persistência otimizada para time-series data

#### 4.1. Implementar Storage SQLite
```go
// internal/monitoring/storage/sqlite.go

type SQLite struct {
    db *sql.DB
    mu sync.RWMutex
}

func NewSQLite(dbPath string) (*SQLite, error) {
    db, err := sql.Open("sqlite3", dbPath)
    if err != nil {
        return nil, err
    }
    
    storage := &SQLite{db: db}
    
    if err := storage.createTables(); err != nil {
        return nil, err
    }
    
    return storage, nil
}

func (s *SQLite) createTables() error {
    schema := `
    CREATE TABLE IF NOT EXISTS hpa_snapshots (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        cluster TEXT NOT NULL,
        namespace TEXT NOT NULL,
        hpa_name TEXT NOT NULL,
        timestamp DATETIME NOT NULL,
        cpu_current REAL,
        memory_current REAL,
        replicas_current INTEGER,
        cpu_target REAL,
        memory_target REAL,
        replicas_min INTEGER,
        replicas_max INTEGER,
        is_baseline BOOLEAN DEFAULT FALSE,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(cluster, namespace, hpa_name, timestamp)
    );
    
    CREATE INDEX IF NOT EXISTS idx_hpa_time ON hpa_snapshots(cluster, namespace, hpa_name, timestamp);
    CREATE INDEX IF NOT EXISTS idx_baseline ON hpa_snapshots(is_baseline, timestamp);
    CREATE INDEX IF NOT EXISTS idx_cluster ON hpa_snapshots(cluster);
    `
    
    _, err := s.db.Exec(schema)
    return err
}

// SaveSnapshotsBatch salva múltiplos snapshots em uma transação
func (s *SQLite) SaveSnapshotsBatch(snapshots []*models.HPASnapshot) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    tx, err := s.db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()
    
    stmt, err := tx.Prepare(`
        INSERT OR REPLACE INTO hpa_snapshots 
        (cluster, namespace, hpa_name, timestamp, cpu_current, memory_current, 
         replicas_current, is_baseline) 
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)
    `)
    if err != nil {
        return err
    }
    defer stmt.Close()
    
    for _, snapshot := range snapshots {
        _, err := stmt.Exec(
            snapshot.Cluster,
            snapshot.Namespace,
            snapshot.HPAName,
            snapshot.Timestamp,
            snapshot.CPUCurrent,
            snapshot.MemoryCurrent,
            snapshot.ReplicasCurrent,
            snapshot.IsBaseline,
        )
        if err != nil {
            return err
        }
    }
    
    return tx.Commit()
}

// HasBaseline verifica se HPA tem baseline de pelo menos 2 dias
func (s *SQLite) HasBaseline(cluster, namespace, hpaName string) (bool, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    query := `
        SELECT COUNT(*) FROM hpa_snapshots 
        WHERE cluster = ? AND namespace = ? AND hpa_name = ? 
        AND is_baseline = TRUE 
        AND timestamp <= datetime('now', '-48 hours')
    `
    
    var count int
    err := s.db.QueryRow(query, cluster, namespace, hpaName).Scan(&count)
    if err != nil {
        return false, err
    }
    
    return count > 0, nil
}

// GetHPAsByCluster retorna todos os HPAs de um cluster
func (s *SQLite) GetHPAsByCluster(cluster string) ([]*models.HPAInfo, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    query := `
        SELECT DISTINCT namespace, hpa_name 
        FROM hpa_snapshots 
        WHERE cluster = ?
    `
    
    rows, err := s.db.Query(query, cluster)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var hpas []*models.HPAInfo
    
    for rows.Next() {
        var hpa models.HPAInfo
        if err := rows.Scan(&hpa.Namespace, &hpa.Name); err != nil {
            return nil, err
        }
        hpa.Cluster = cluster
        hpas = append(hpas, &hpa)
    }
    
    return hpas, rows.Err()
}
```

---

## 🔄 Fluxo de Execução com Time Slots

### 1. Inicialização da Aplicação
```
Server Start → 
Monitoring Engine Start → 
TimeSlotManager Init (calcula janelas) → 
PortForward Manager Start (todas as portas) → 
SQLite Database Open
```

### 2. Configuração de Time Slots
```
4 Clusters Exemplo:
├── Slot 0 (0-25s):   Cluster1→55553, Cluster2→55554
├── Slot 1 (25-50s):  Cluster3→55553, Cluster4→55554
└── Ciclo: 50 segundos total

6 Clusters Exemplo:  
├── Slot 0 (0-20s):   Cluster1→55553, Cluster2→55554
├── Slot 1 (20-40s):  Cluster3→55553, Cluster4→55554  
├── Slot 2 (40-60s):  Cluster5→55553, Cluster6→55554
└── Ciclo: 60 segundos total
```

### 3. Adicionar HPA para Monitoramento
```
POST /api/v1/monitoring/hpa → 
Verificar se cluster tem port-forward → 
Se não, criar port-forward persistente → 
Adicionar cluster ao TimeSlotManager → 
Recalcular janelas temporais → 
Verificar se HPA tem baseline no SQLite → 
Se não, iniciar coleta histórica em background → 
Retornar status (com/sem baseline)
```

### 4. Coleta Histórica (Background - Paralela)
```
Query Prometheus últimos 3 dias → 
864 snapshots com step 5min → 
Validar cobertura mínima 70% → 
Salvar batch no SQLite com is_baseline=true → 
Log conclusão da coleta
```

### 5. Monitoramento por Janelas Temporais
```
Timer 5 segundos (verificação de mudança de slot) →
Verificar slot atual do TimeSlotManager →
Se mudou de slot:
├── Pegar clusters da janela atual
├── Scan PARALELO:
│   ├── Cluster A na porta 55553 (goroutine 1)
│   └── Cluster B na porta 55554 (goroutine 2)  
├── Para cada cluster:
│   ├── Verificar se port-forward está ativo
│   ├── Se não, reconectar
│   └── Para cada HPA do cluster:
│       ├── Verificar se tem baseline
│       ├── Se sim, coletar dados atuais
│       ├── Comparar com baseline (detecção anomalias)
│       └── Salvar novo snapshot
└── Aguardar próxima janela temporal
```

### 6. Exemplo de Execução Temporal (4 clusters):
```
00:00 - 00:25s: Cluster1(55553) + Cluster2(55554) em paralelo
00:25 - 00:50s: Cluster3(55553) + Cluster4(55554) em paralelo  
00:50 - 01:15s: Cluster1(55553) + Cluster2(55554) em paralelo
01:15 - 01:40s: Cluster3(55553) + Cluster4(55554) em paralelo
...
```

### 7. Shutdown da Aplicação
```
Signal SIGINT/SIGTERM → 
Stop TimeSlot Scan Loop → 
Stop Port-Forward Manager (todas as portas) → 
Fechar SQLite Database → 
Log shutdown completo
```

---

## 📊 APIs Necessárias

### Adicionar HPA
```http
POST /api/v1/monitoring/hpa
Authorization: Bearer poc-token-123
Content-Type: application/json

{
  "cluster": "akspriv-prod-admin",
  "namespace": "production", 
  "hpa": "api-gateway"
}
```

**Resposta:**
```json
{
  "status": "success",
  "message": "HPA added to monitoring",
  "has_baseline": false,
  "baseline_collection_started": true,
  "estimated_completion": "2025-11-08T10:30:00Z"
}
```

### Listar HPAs Monitorados
```http
GET /api/v1/monitoring/hpas
Authorization: Bearer poc-token-123
```

**Resposta:**
```json
{
  "status": "success",
  "hpas": [
    {
      "cluster": "akspriv-prod-admin",
      "namespace": "production",
      "hpa": "api-gateway", 
      "has_baseline": true,
      "monitoring_active": true,
      "last_scan": "2025-11-05T14:25:00Z",
      "baseline_coverage": 95.2
    }
  ]
}
```

### Status do Sistema com Time Slots
```http
GET /api/v1/monitoring/status
Authorization: Bearer poc-token-123
```

**Resposta:**
```json
{
  "status": "success",
  "port_forwards": [
    {
      "cluster": "akspriv-prod-admin",
      "port": 55553,
      "status": "active",
      "uptime": "2h35m"
    },
    {
      "cluster": "akspriv-staging-admin", 
      "port": 55554,
      "status": "active",
      "uptime": "1h42m"
    }
  ],
  "time_slots": {
    "total_clusters": 4,
    "total_slots": 2,
    "slot_duration": "25s",
    "cycle_duration": "50s",
    "current_slot": {
      "index": 1,
      "port_55553_cluster": "akspriv-prod3-admin",
      "port_55554_cluster": "akspriv-prod4-admin", 
      "start_time": "2025-11-05T14:25:25Z",
      "end_time": "2025-11-05T14:25:50Z",
      "time_remaining": "18s"
    },
    "next_slot": {
      "index": 0,
      "port_55553_cluster": "akspriv-prod1-admin",
      "port_55554_cluster": "akspriv-prod2-admin",
      "start_time": "2025-11-05T14:25:50Z",
      "end_time": "2025-11-05T14:26:15Z"
    },
    "all_slots": [
      {
        "index": 0,
        "clusters": ["akspriv-prod1-admin", "akspriv-prod2-admin"],
        "duration": "25s",
        "is_active": false
      },
      {
        "index": 1, 
        "clusters": ["akspriv-prod3-admin", "akspriv-prod4-admin"],
        "duration": "25s",
        "is_active": true
      }
    ]
  },
  "database": {
    "total_snapshots": 1847,
    "baseline_snapshots": 1728,
    "monitoring_snapshots": 119,
    "size_mb": 2.3
  }
}
```

### Visualização de Time Slots
```http
GET /api/v1/monitoring/timeslots
Authorization: Bearer poc-token-123
```

**Resposta:**
```json
{
  "status": "success",
  "configuration": {
    "total_clusters": 4,
    "slot_duration": "25s", 
    "cycle_duration": "50s",
    "parallel_scans": true
  },
  "current_cycle": {
    "started_at": "2025-11-05T14:25:00Z",
    "ends_at": "2025-11-05T14:25:50Z",
    "progress_percent": 60,
    "current_slot_index": 1
  },
  "slots": [
    {
      "index": 0,
      "start_offset": "0s",
      "duration": "25s", 
      "port_55553": {
        "cluster": "akspriv-prod1-admin",
        "prometheus_url": "http://localhost:55553"
      },
      "port_55554": {
        "cluster": "akspriv-prod2-admin", 
        "prometheus_url": "http://localhost:55554"
      },
      "is_active": false,
      "last_executed": "2025-11-05T14:24:25Z"
    },
    {
      "index": 1,
      "start_offset": "25s",
      "duration": "25s",
      "port_55553": {
        "cluster": "akspriv-prod3-admin",
        "prometheus_url": "http://localhost:55553"  
      },
      "port_55554": {
        "cluster": "akspriv-prod4-admin",
        "prometheus_url": "http://localhost:55554"
      },
      "is_active": true,
      "executing_since": "2025-11-05T14:25:25Z",
      "time_remaining": "18s"
    }
  ]
}
```

---

## ✅ Checklist de Implementação

### Fase 1: Port-Forward Persistente ⏳
- [ ] Implementar `PortForwardManager` refinado
- [ ] Implementar `PortForward` individual com health check
- [ ] Integrar com `ScanEngine.Start()` e `Stop()`
- [ ] Testar port-forwards simultâneos nas duas portas
- [ ] Testar cleanup no shutdown

### Fase 2: Coleta Histórica ⏳
- [ ] Implementar `HistoricalCollector`
- [ ] Queries Prometheus para range de 3 dias
- [ ] Validação de cobertura mínima (70%)
- [ ] Integração com SQLite batch insert
- [ ] API endpoint para adicionar HPA com coleta histórica

### Fase 3: Fila Alternada ⏳
- [ ] Implementar `ScanQueue` com alternância
- [ ] Modificar `ScanEngine` para usar fila
- [ ] Reutilização de port-forwards (sem recriar)
- [ ] Reconexão automática se port-forward morrer

### Fase 4: SQLite Storage ⏳
- [ ] Schema otimizado para time-series
- [ ] Métodos batch para performance
- [ ] Índices para queries rápidas
- [ ] Cleanup automático de dados antigos

### Fase 5: APIs e Integração ⏳
- [ ] Endpoint `POST /api/v1/monitoring/hpa`
- [ ] Endpoint `GET /api/v1/monitoring/hpas`
- [ ] Endpoint `GET /api/v1/monitoring/status`
- [ ] Integração com interface web existente

---

## 🧪 Plano de Testes

### Teste 1: Port-Forwards Persistentes
```bash
# Iniciar servidor
./build/k8s-hpa-manager web -f

# Adicionar 2 clusters
curl -X POST .../hpa -d '{"cluster":"prod","namespace":"api","hpa":"gateway"}'
curl -X POST .../hpa -d '{"cluster":"staging","namespace":"web","hpa":"frontend"}'

# Verificar port-forwards ativos
ps aux | grep "kubectl port-forward" | grep -E "(55553|55554)"
# Esperado: 2 processos

# Aguardar 5 minutos (5 scans)
sleep 300

# Verificar port-forwards AINDA ativos (mesmos PIDs)
ps aux | grep "kubectl port-forward" | grep -E "(55553|55554)"

# Parar servidor
# Verificar port-forwards destruídos
```

### Teste 2: Coleta Histórica
```bash
# Limpar database
rm ~/.k8s-hpa-manager/monitoring.db

# Adicionar HPA novo
curl -X POST .../hpa -d '{"cluster":"prod","namespace":"api","hpa":"gateway"}'
# Esperado: {"has_baseline": false, "baseline_collection_started": true}

# Verificar logs de coleta histórica
tail -f /tmp/k8s-hpa-manager.log | grep "coleta histórica"

# Aguardar conclusão (pode demorar 2-5 minutos)
# Verificar SQLite
sqlite3 ~/.k8s-hpa-manager/monitoring.db "SELECT COUNT(*) FROM hpa_snapshots WHERE is_baseline=1;"
# Esperado: ~800-900 snapshots
```

### Teste 3: Time Slots com Paralelismo
```bash
# Adicionar 4 HPAs em 4 clusters diferentes
curl -X POST .../hpa -d '{"cluster":"prod1","namespace":"api","hpa":"gateway"}'
curl -X POST .../hpa -d '{"cluster":"prod2","namespace":"web","hpa":"frontend"}'  
curl -X POST .../hpa -d '{"cluster":"prod3","namespace":"worker","hpa":"processor"}'
curl -X POST .../hpa -d '{"cluster":"prod4","namespace":"cache","hpa":"redis"}'

# Verificar configuração de slots
curl -X GET .../monitoring/timeslots | jq '.slots'

# Monitorar logs de time slots
tail -f /tmp/k8s-hpa-manager.log | grep -E "(Executando scan da janela|slot_index)"

# Esperado (janelas paralelas de 25s cada):
# Slot 0 (0-25s):   prod1→55553 + prod2→55554 (PARALELO)
# Slot 1 (25-50s):  prod3→55553 + prod4→55554 (PARALELO)  
# Slot 0 (50-75s):  prod1→55553 + prod2→55554 (PARALELO)
# Slot 1 (75-100s): prod3→55553 + prod4→55554 (PARALELO)

# Verificar paralelismo real
ps aux | grep "kubectl port-forward" | wc -l
# Esperado: 4 processos (todos os port-forwards ativos)

# Testar com 6 clusters (3 slots de 20s cada)
for i in {5..6}; do
  curl -X POST .../hpa -d "{\"cluster\":\"prod$i\",\"namespace\":\"app\",\"hpa\":\"service$i\"}"
done

# Verificar reconfiguração automática
curl -X GET .../monitoring/timeslots | jq '.configuration'
# Esperado: slot_duration="20s", cycle_duration="60s", total_slots=3
```

### Teste 4: Reconexão de Port-Forwards
```bash
# Matar um port-forward manualmente
pkill -f "kubectl port-forward.*55553"

# Monitorar logs de reconexão
tail -f /tmp/k8s-hpa-manager.log | grep "reconectar"

# Esperado:
# "Port-forward não disponível, tentando reconectar..."
# "Port-forward persistente ativo"

# Verificar se scan continua normalmente
# Não deve haver interrupção no ciclo de slots
```

---

## 🚨 Riscos e Mitigações

### Risco 1: Port-Forward Instável
**Problema:** kubectl port-forward pode morrer
**Mitigação:** Health check a cada scan + reconexão automática

### Risco 2: Prometheus Lento
**Problema:** Queries de 3 dias podem demorar muito
**Mitigação:** Timeout de 5min + queries em paralelo por métrica

### Risco 3: SQLite Crescimento
**Problema:** Database pode crescer indefinidamente
**Mitigação:** Cleanup automático (dados > 30 dias)

### Risco 4: Port-Forwards Órfãos
**Problema:** Crash da aplicação deixa processos ativos
**Mitigação:** Signal handling + defer cleanup + PID tracking

---

## 📈 Próximos Passos

1. **Implementar Fase 1** (Port-Forward Persistente)
2. **Testar isoladamente** cada componente
3. **Implementar Fase 2** (Coleta Histórica)
4. **Integrar com UI web** existente
5. **Implementar detecção de anomalias** (próximo TODO)

---

**Data de criação:** 05 de novembro de 2025  
**Responsável:** Claude + Paulo Ribeiro  
**Status:** 📋 Aguardando implementação
