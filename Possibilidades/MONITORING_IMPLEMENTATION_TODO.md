# TODO: Implementação Correta do Sistema de Monitoring HPA-Watchdog

**Data:** 05 de novembro de 2025
**Status:** 🔴 Implementação Incorreta - Requer Refatoração Completa

---

## 🎯 Objetivo

Implementar sistema de monitoramento de HPAs em tempo real com:
- Port-forward persistente para Prometheus
- Coleta histórica de 3 dias antes de iniciar monitoramento
- Baseline de dados para comparação e detecção de anomalias
- Gerenciamento correto de portas com cleanup no shutdown

---

## ❌ Problemas da Implementação Atual

### 1. Port-Forward Efêmero (ERRADO)
**Problema:** Port-forward é criado e destruído a cada scan (linhas 373-389 de `engine.go`)
```go
// ATUAL (ERRADO)
if err := e.pfManager.Start(target.Cluster); err != nil { ... }
func() {
    defer func() {
        if err := e.pfManager.Stop(target.Cluster); err != nil { ... }
    }()
    // ... scan ...
}()
```

**Por que está errado:**
- Port-forward é destruído após cada coleta (1 minuto)
- Overhead de criar/destruir conexão kubectl a cada scan
- Impossível manter duas portas simultâneas
- Portas ficam órfãs se aplicação crashar durante scan

### 2. Sem Coleta Histórica de Baseline
**Problema:** Monitoramento inicia IMEDIATAMENTE ao adicionar HPA
- Não há dados históricos de 3 dias para comparação
- Detecção de anomalias é impossível sem baseline
- Sistema não tem contexto de comportamento normal do HPA

### 3. Gerenciamento de Portas Incorreto
**Problema:** Lógica de portas ímpares/pares não funciona como fila alternada
- Port-forward é destruído antes de alternar para próximo cluster
- Impossível ter 2 portas abertas simultaneamente
- Não há alternância real entre portas 55553 e 55554

### 4. Cleanup Inadequado
**Problema:** Port-forwards órfãos quando servidor crashar
- Destruição de porta acontece durante scan (goroutine pode não completar)
- Não há cleanup garantido no shutdown do servidor
- `StopAll()` é chamado mas pode não executar se servidor crashar

---

## ✅ Arquitetura Correta a Implementar

### Fase 1: Port-Forward Persistente

**Objetivo:** Port-forward vive durante toda execução do servidor, não por scan.

#### 1.1. Alterações em `engine.go`

**Remover:** Criação/destruição de port-forward no `runScan()` (linhas 373-389)

**Adicionar:** Port-forward persistente no `Start()` e `Stop()`

```go
// Start inicia scan engine COM port-forwards persistentes
func (e *ScanEngine) Start() error {
    e.mu.Lock()
    if e.running {
        e.mu.Unlock()
        return nil
    }
    e.running = true
    e.paused = false
    e.mu.Unlock()

    log.Info().
        Str("mode", e.config.Mode.String()).
        Dur("interval", e.config.Interval).
        Dur("duration", e.config.Duration).
        Msg("Iniciando scan engine")

    // NOVO: Inicia port-forwards PERSISTENTES para todos os clusters
    // Agrupa clusters por porta (ímpar/par)
    clustersByPort := e.groupClustersByPort()

    // Inicia port-forwards nas 2 portas SIMULTANEAMENTE
    for port, clusters := range clustersByPort {
        for _, cluster := range clusters {
            if err := e.pfManager.Start(cluster); err != nil {
                log.Error().
                    Err(err).
                    Str("cluster", cluster).
                    Int("port", port).
                    Msg("Falha ao iniciar port-forward persistente")
                // Continua com outros clusters
            } else {
                log.Info().
                    Str("cluster", cluster).
                    Int("port", port).
                    Msg("Port-forward persistente iniciado")
            }
        }
    }

    // Se modo stress test, captura baseline antes de iniciar
    if e.config.Mode == scanner.ScanModeStressTest {
        if err := e.captureBaseline(); err != nil {
            log.Error().
                Err(err).
                Msg("Falha ao capturar baseline, continuando sem baseline")
        }
    }

    // Inicia loop de scan (SEM criar/destruir port-forwards)
    e.wg.Add(1)
    go e.scanLoop()

    return nil
}

// Stop para scan engine E port-forwards persistentes
func (e *ScanEngine) Stop() error {
    e.mu.Lock()
    if !e.running {
        e.mu.Unlock()
        return nil
    }
    e.running = false
    e.mu.Unlock()

    log.Info().Msg("Parando scan engine")

    // Cancela contexto (para scanLoop)
    e.cancel()

    // Aguarda scanLoop terminar
    e.wg.Wait()

    // NOVO: Para TODOS os port-forwards persistentes
    log.Info().Msg("Parando todos os port-forwards persistentes...")
    if err := e.pfManager.StopAll(); err != nil {
        log.Error().Err(err).Msg("Erro ao parar port-forwards")
    } else {
        log.Info().Msg("Todos os port-forwards parados com sucesso")
    }

    // Se modo stress test, finaliza e salva resultado
    if e.config.Mode == scanner.ScanModeStressTest {
        if err := e.finalizeStressTest(); err != nil {
            log.Error().
                Err(err).
                Msg("Erro ao finalizar stress test")
        }
    }

    // Cleanup e fecha persistência
    if e.persistence != nil {
        if err := e.persistence.Cleanup(); err != nil {
            log.Warn().Err(err).Msg("Erro ao limpar dados antigos")
        }
        if err := e.persistence.Close(); err != nil {
            log.Warn().Err(err).Msg("Erro ao fechar banco de dados")
        }
        log.Info().Msg("Persistência SQLite fechada")
    }

    log.Info().Msg("Scan engine parado completamente")
    return nil
}

// groupClustersByPort agrupa clusters por porta (ímpar/par)
func (e *ScanEngine) groupClustersByPort() map[int][]string {
    result := map[int][]string{
        55553: {}, // Porta ímpar
        55554: {}, // Porta par
    }

    for i, target := range e.config.Targets {
        port := 55554 // Par (padrão)
        if i%2 == 1 {
            port = 55553 // Ímpar
        }
        result[port] = append(result[port], target.Cluster)
    }

    return result
}
```

**Modificar:** `runScan()` para REUTILIZAR port-forwards existentes

```go
// runScan executa um scan completo (SEM criar/destruir port-forwards)
func (e *ScanEngine) runScan() {
    log.Info().Msg("Executando scan...")

    scanStart := time.Now()

    // Para cada target configurado
    for _, target := range e.config.Targets {
        log.Info().
            Str("cluster", target.Cluster).
            Strs("namespaces", target.Namespaces).
            Msg("Escaneando cluster")

        // NOVO: REUTILIZA port-forward existente (não cria/destroi)
        promEndpoint := e.pfManager.GetURL(target.Cluster)
        if promEndpoint == "" {
            log.Warn().
                Str("cluster", target.Cluster).
                Msg("Port-forward não disponível, pulando cluster")
            continue
        }

        // Cria contexto com timeout para o scan
        ctx, cancel := context.WithTimeout(e.ctx, 2*time.Minute)

        // Cria ClusterInfo
        context := target.Cluster
        if !strings.HasSuffix(target.Cluster, "-admin") {
            context = target.Cluster + "-admin"
        }

        clusterInfo := &models.ClusterInfo{
            Name:    target.Cluster,
            Context: context,
        }

        // Cria collector para este cluster
        collector, err := monitor.NewCollector(clusterInfo, promEndpoint, &monitor.CollectorConfig{
            ScanInterval:      e.config.Interval,
            ExcludeNamespaces: []string{},
            EnablePrometheus:  true,
        })
        if err != nil {
            log.Error().
                Err(err).
                Str("cluster", target.Cluster).
                Msg("Falha ao criar collector")
            cancel()
            continue
        }

        // Executa scan do cluster
        result, err := collector.Scan(ctx)
        cancel()

        if err != nil {
            log.Error().
                Err(err).
                Str("cluster", target.Cluster).
                Msg("Falha ao executar scan")
            continue
        }

        // ... resto do código de processamento de snapshots ...

        log.Info().
            Str("cluster", target.Cluster).
            Int("snapshots", result.SnapshotsCount).
            Int("anomalies", len(result.Anomalies)).
            Int("errors", len(result.Errors)).
            Msg("Cluster escaneado com sucesso")
    }

    scanDuration := time.Since(scanStart)
    log.Info().
        Dur("duration", scanDuration).
        Msg("Scan completo")
}
```

#### 1.2. Alterações em `portforward.go`

**Adicionar:** Verificação de saúde do port-forward antes de cada scan

```go
// EnsureRunning verifica e reinicia port-forward se necessário
func (pf *PortForward) EnsureRunning() error {
    if pf.IsRunning() {
        return nil // Já está rodando
    }

    log.Warn().
        Str("cluster", pf.cluster).
        Msg("Port-forward inativo, reiniciando...")

    return pf.Start()
}
```

**Modificar:** `PortForwardManager.Start()` para não parar port-forward anterior

```go
// Start inicia port-forward para um cluster (PERSISTENTE)
func (m *PortForwardManager) Start(cluster string) error {
    // Se já existe E está rodando, retorna
    if pf, exists := m.forwards[cluster]; exists {
        if pf.IsRunning() {
            log.Info().Str("cluster", cluster).Msg("Port-forward já ativo")
            return nil
        }
        // Se existe mas não está rodando, remove e recria
        log.Warn().Str("cluster", cluster).Msg("Port-forward existe mas não está ativo, recriando...")
        pf.Stop()
        delete(m.forwards, cluster)
    }

    // Atribui índice ao cluster se ainda não tem
    if _, exists := m.clusterIndex[cluster]; !exists {
        m.clusterIndex[cluster] = len(m.clusterIndex)
    }
    index := m.clusterIndex[cluster]

    // Determina porta baseado no índice (ímpar/par)
    var port int
    if index%2 == 0 {
        port = m.portEven // 55554
    } else {
        port = m.portOdd // 55553
    }

    // REMOVIDO: Lógica de waitForRelease (não precisamos mais parar port-forward anterior)

    // Descobre o nome do serviço Prometheus
    serviceName := m.discoverPrometheusService(cluster)

    log.Info().
        Str("cluster", cluster).
        Int("index", index).
        Int("port", port).
        Str("service", serviceName).
        Msg("Iniciando port-forward persistente")

    // Context precisa do sufixo -admin
    context := cluster
    if !strings.HasSuffix(cluster, "-admin") {
        context = cluster + "-admin"
    }

    pf := New(Config{
        Cluster:   context,
        Service:   serviceName,
        LocalPort: port,
    })

    if err := pf.Start(); err != nil {
        return fmt.Errorf("falha ao iniciar port-forward: %w", err)
    }

    m.forwards[cluster] = pf

    log.Info().
        Str("cluster", cluster).
        Int("port", port).
        Msg("Port-forward persistente ativo")

    return nil
}
```

---

### Fase 2: Coleta Histórica de Baseline (3 dias)

**Objetivo:** Coletar dados históricos de 3 dias do HPA ANTES de iniciar monitoramento real.

#### 2.1. Nova Função em `monitor/collector.go`

```go
// CollectHistoricalData coleta dados históricos de um HPA via Prometheus
func (c *Collector) CollectHistoricalData(ctx context.Context, namespace, hpaName string, duration time.Duration) ([]*models.HPASnapshot, error) {
    log.Info().
        Str("namespace", namespace).
        Str("hpa", hpaName).
        Dur("duration", duration).
        Msg("Coletando dados históricos do Prometheus")

    // Query Prometheus para dados históricos
    // Range query: últimos 3 dias com step de 5 minutos
    step := 5 * time.Minute
    endTime := time.Now()
    startTime := endTime.Add(-duration)

    // Queries Prometheus para métricas históricas
    queries := map[string]string{
        "cpu_current": fmt.Sprintf(
            `avg(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*"}[5m])) * 100`,
            namespace, hpaName,
        ),
        "memory_current": fmt.Sprintf(
            `avg(container_memory_working_set_bytes{namespace="%s",pod=~"%s-.*"}) / 1024 / 1024`,
            namespace, hpaName,
        ),
        "replicas": fmt.Sprintf(
            `kube_deployment_status_replicas{namespace="%s",deployment="%s"}`,
            namespace, hpaName,
        ),
    }

    snapshots := make([]*models.HPASnapshot, 0)

    // Para cada timestamp no range (step de 5 minutos)
    for ts := startTime; ts.Before(endTime); ts = ts.Add(step) {
        snapshot := &models.HPASnapshot{
            Cluster:   c.clusterInfo.Name,
            Namespace: namespace,
            Name:      hpaName,
            Timestamp: ts,
        }

        // Executar queries para este timestamp
        for metric, query := range queries {
            value, err := c.prometheusClient.QueryAtTime(ctx, query, ts)
            if err != nil {
                log.Debug().
                    Err(err).
                    Str("metric", metric).
                    Time("timestamp", ts).
                    Msg("Falha ao coletar métrica histórica")
                continue
            }

            // Preencher snapshot com valores
            switch metric {
            case "cpu_current":
                snapshot.CPUCurrent = value
            case "memory_current":
                snapshot.MemoryCurrent = value
            case "replicas":
                snapshot.CurrentReplicas = int32(value)
            }
        }

        snapshots = append(snapshots, snapshot)
    }

    log.Info().
        Str("namespace", namespace).
        Str("hpa", hpaName).
        Int("snapshots_collected", len(snapshots)).
        Msg("Dados históricos coletados com sucesso")

    return snapshots, nil
}
```

#### 2.2. Nova Fase de Inicialização em `handlers/monitoring.go`

```go
// AddHPA adiciona um HPA específico para monitoramento
// POST /api/v1/monitoring/hpa
// Body: { "cluster": "...", "namespace": "...", "hpa": "..." }
func (h *MonitoringHandler) AddHPA(c *gin.Context) {
    var req struct {
        Cluster   string `json:"cluster" binding:"required"`
        Namespace string `json:"namespace" binding:"required"`
        HPA       string `json:"hpa" binding:"required"`
    }

    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{
            "status":  "error",
            "message": "Invalid request body",
            "error":   err.Error(),
        })
        return
    }

    // Remove sufixo -admin do cluster
    clusterName := strings.TrimSuffix(req.Cluster, "-admin")

    log.Info().
        Str("cluster_received", req.Cluster).
        Str("cluster_normalized", clusterName).
        Str("namespace", req.Namespace).
        Str("hpa", req.HPA).
        Msg("Adicionando HPA ao monitoramento")

    // NOVO: Verificar se HPA já tem baseline (dados históricos)
    hasBaseline, err := h.checkHPABaseline(clusterName, req.Namespace, req.HPA)
    if err != nil {
        log.Warn().Err(err).Msg("Erro ao verificar baseline do HPA")
    }

    if !hasBaseline {
        log.Info().
            Str("cluster", clusterName).
            Str("namespace", req.Namespace).
            Str("hpa", req.HPA).
            Msg("HPA sem baseline, iniciando coleta histórica de 3 dias...")

        // Inicia coleta histórica em background
        go h.collectHistoricalDataForHPA(clusterName, req.Namespace, req.HPA)
    }

    // Criar target com HPA específico
    target := scanner.ScanTarget{
        Cluster:    clusterName,
        Namespaces: []string{req.Namespace},
        HPAs:       []string{req.HPA},
    }

    h.engine.AddTarget(target)

    c.JSON(200, gin.H{
        "status":       "success",
        "message":      "HPA added to monitoring successfully",
        "has_baseline": hasBaseline,
        "target": gin.H{
            "cluster":   clusterName,
            "namespace": req.Namespace,
            "hpa":       req.HPA,
        },
    })
}

// checkHPABaseline verifica se HPA já tem dados históricos no SQLite
func (h *MonitoringHandler) checkHPABaseline(cluster, namespace, hpa string) (bool, error) {
    cache := h.engine.GetCache()
    if cache == nil {
        return false, fmt.Errorf("cache not available")
    }

    // Verifica se existe dados de pelo menos 2 dias atrás
    tsData := cache.Get(cluster, namespace, hpa)
    if tsData == nil {
        return false, nil
    }

    twoDaysAgo := time.Now().Add(-48 * time.Hour)

    tsData.RLock()
    defer tsData.RUnlock()

    for _, snapshot := range tsData.Snapshots {
        if snapshot.Timestamp.Before(twoDaysAgo) {
            // Tem dados de 2 dias atrás, consideramos como baseline válida
            return true, nil
        }
    }

    return false, nil
}

// collectHistoricalDataForHPA coleta dados históricos de 3 dias em background
func (h *MonitoringHandler) collectHistoricalDataForHPA(cluster, namespace, hpa string) {
    log.Info().
        Str("cluster", cluster).
        Str("namespace", namespace).
        Str("hpa", hpa).
        Msg("Iniciando coleta histórica de 3 dias em background")

    // Aguardar port-forward estar disponível
    maxRetries := 30
    promEndpoint := ""

    // TODO: Implementar acesso ao pfManager do engine
    // Por enquanto, assume que port-forward está ativo após 10s
    time.Sleep(10 * time.Second)

    // Criar collector temporário para coleta histórica
    // ... (implementação usando CollectHistoricalData)

    log.Info().
        Str("cluster", cluster).
        Str("namespace", namespace).
        Str("hpa", hpa).
        Msg("Coleta histórica de 3 dias concluída")
}
```

---

### Fase 3: Gerenciamento Correto de Portas (Fila Alternada)

**Objetivo:** Duas portas abertas simultaneamente, leitura alternada entre clusters.

#### 3.1. Alterações em `scanner/scan.go` (NOVO)

```go
// PortQueue gerencia fila de clusters por porta
type PortQueue struct {
    port55553 []string  // Clusters na porta ímpar
    port55554 []string  // Clusters na porta par
    current   int       // Índice atual na leitura alternada
    mu        sync.RWMutex
}

// NewPortQueue cria nova fila de portas
func NewPortQueue() *PortQueue {
    return &PortQueue{
        port55553: make([]string, 0),
        port55554: make([]string, 0),
        current:   0,
    }
}

// AddCluster adiciona cluster à fila apropriada (baseado em índice)
func (pq *PortQueue) AddCluster(cluster string, index int) {
    pq.mu.Lock()
    defer pq.mu.Unlock()

    if index%2 == 0 {
        pq.port55554 = append(pq.port55554, cluster)
    } else {
        pq.port55553 = append(pq.port55553, cluster)
    }
}

// GetNextCluster retorna próximo cluster a ser lido (alternando portas)
func (pq *PortQueue) GetNextCluster() (string, int) {
    pq.mu.Lock()
    defer pq.mu.Unlock()

    // Alterna entre porta ímpar e par
    if pq.current%2 == 0 {
        // Ler porta 55553 (ímpar)
        if len(pq.port55553) > 0 {
            clusterIndex := pq.current / 2
            if clusterIndex >= len(pq.port55553) {
                pq.current = 0
                clusterIndex = 0
            }
            cluster := pq.port55553[clusterIndex]
            pq.current++
            return cluster, 55553
        }
    } else {
        // Ler porta 55554 (par)
        if len(pq.port55554) > 0 {
            clusterIndex := pq.current / 2
            if clusterIndex >= len(pq.port55554) {
                pq.current = 1
                clusterIndex = 0
            }
            cluster := pq.port55554[clusterIndex]
            pq.current++
            return cluster, 55554
        }
    }

    // Se chegou aqui, reseta e tenta novamente
    pq.current = 0
    return pq.GetNextCluster()
}
```

#### 3.2. Modificar `engine.go` para usar PortQueue

```go
// ScanEngine orquestra coleta, análise e detecção
type ScanEngine struct {
    config *scanner.ScanConfig

    // Componentes
    pfManager   *portforward.PortForwardManager
    portQueue   *scanner.PortQueue  // NOVO: Fila de portas alternadas
    cache       *storage.TimeSeriesCache
    persistence *storage.Persistence
    detector    *analyzer.Detector

    // ... resto dos campos ...
}

// runScan executa um scan completo COM leitura alternada de portas
func (e *ScanEngine) runScan() {
    log.Info().Msg("Executando scan com leitura alternada de portas...")

    scanStart := time.Now()

    // NOVO: Ler clusters alternando entre portas
    clustersToScan := len(e.config.Targets)

    for i := 0; i < clustersToScan; i++ {
        cluster, port := e.portQueue.GetNextCluster()

        log.Info().
            Str("cluster", cluster).
            Int("port", port).
            Msg("Escaneando cluster (leitura alternada)")

        target := e.findTargetByCluster(cluster)
        if target == nil {
            continue
        }

        // Reutiliza port-forward existente
        promEndpoint := e.pfManager.GetURL(cluster)
        if promEndpoint == "" {
            log.Warn().
                Str("cluster", cluster).
                Int("port", port).
                Msg("Port-forward não disponível, pulando cluster")
            continue
        }

        // ... resto do código de scan ...
    }

    scanDuration := time.Since(scanStart)
    log.Info().
        Dur("duration", scanDuration).
        Msg("Scan completo")
}
```

---

### Fase 4: Cleanup Garantido no Shutdown

**Objetivo:** Port-forwards são destruídos APENAS quando servidor web para.

#### 4.1. Alterações em `server.go`

```go
// Run inicia servidor web COM cleanup garantido
func (s *Server) Run(ctx context.Context, foreground bool) error {
    // ... código existente ...

    // NOVO: Registrar handler de shutdown para cleanup de port-forwards
    go func() {
        <-ctx.Done()
        log.Info().Msg("Contexto cancelado, iniciando shutdown...")

        // Para monitoring engine (que para port-forwards)
        if s.monitoringEngine != nil {
            log.Info().Msg("Parando monitoring engine e port-forwards...")
            if err := s.monitoringEngine.Stop(); err != nil {
                log.Error().Err(err).Msg("Erro ao parar monitoring engine")
            } else {
                log.Info().Msg("Monitoring engine e port-forwards parados com sucesso")
            }
        }

        // Para servidor HTTP
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()

        if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
            log.Error().Err(err).Msg("Erro ao parar servidor HTTP")
        } else {
            log.Info().Msg("Servidor HTTP parado com sucesso")
        }
    }()

    // ... resto do código ...
}
```

---

## 📋 Checklist de Implementação

### ✅ Fase 1: Port-Forward Persistente
- [ ] Remover criação/destruição de port-forward em `runScan()`
- [ ] Adicionar criação de port-forwards em `engine.Start()`
- [ ] Adicionar destruição de port-forwards em `engine.Stop()`
- [ ] Implementar `groupClustersByPort()` para separar clusters por porta
- [ ] Adicionar verificação de saúde `EnsureRunning()` em portforward.go
- [ ] Modificar `PortForwardManager.Start()` para não parar port-forward anterior
- [ ] Testar: Port-forwards devem permanecer ativos entre scans

### ✅ Fase 2: Coleta Histórica
- [ ] Implementar `CollectHistoricalData()` em collector.go
- [ ] Adicionar queries Prometheus para range queries (últimos 3 dias)
- [ ] Implementar `checkHPABaseline()` para verificar dados existentes
- [ ] Implementar `collectHistoricalDataForHPA()` para coleta em background
- [ ] Modificar `AddHPA` handler para iniciar coleta histórica se necessário
- [ ] Adicionar campo `has_baseline` na resposta da API
- [ ] Salvar dados históricos no SQLite
- [ ] Testar: HPA só inicia monitoramento real após ter baseline de 3 dias

### ✅ Fase 3: Gerenciamento de Fila de Portas
- [ ] Criar struct `PortQueue` em scanner/scan.go
- [ ] Implementar `AddCluster()` para adicionar à fila correta
- [ ] Implementar `GetNextCluster()` para leitura alternada
- [ ] Adicionar `portQueue` no `ScanEngine`
- [ ] Modificar `runScan()` para usar fila alternada
- [ ] Testar: Leitura deve alternar entre porta 55553 e 55554

### ✅ Fase 4: Cleanup no Shutdown
- [ ] Adicionar handler de shutdown em `server.go`
- [ ] Garantir chamada a `monitoringEngine.Stop()` no shutdown
- [ ] Testar: Port-forwards devem ser destruídos ao parar servidor
- [ ] Testar: Port-forwards NÃO devem ficar órfãos após crash (usar `defer` ou signal handling)

---

## 🧪 Plano de Testes

### Teste 1: Port-Forward Persistente
```bash
# 1. Iniciar servidor
./build/k8s-hpa-manager web -f

# 2. Adicionar 2 HPAs de clusters diferentes
curl -X POST http://localhost:8080/api/v1/monitoring/hpa \
  -H "Authorization: Bearer poc-token-123" \
  -H "Content-Type: application/json" \
  -d '{"cluster":"akspriv-prod-admin","namespace":"prod","hpa":"app1"}'

curl -X POST http://localhost:8080/api/v1/monitoring/hpa \
  -H "Authorization: Bearer poc-token-123" \
  -H "Content-Type: application/json" \
  -d '{"cluster":"akspriv-staging-admin","namespace":"staging","hpa":"app2"}'

# 3. Verificar port-forwards ativos
ps aux | grep "kubectl port-forward"
# Esperado: 2 processos (porta 55553 e 55554)

# 4. Aguardar 2 minutos (2 scans)
sleep 120

# 5. Verificar port-forwards AINDA ativos
ps aux | grep "kubectl port-forward"
# Esperado: 2 processos (mesmos PIDs de antes)

# 6. Parar servidor (Ctrl+C)

# 7. Verificar port-forwards foram destruídos
ps aux | grep "kubectl port-forward"
# Esperado: Nenhum processo
```

### Teste 2: Coleta Histórica
```bash
# 1. Limpar SQLite
rm ~/.k8s-hpa-manager/monitoring.db

# 2. Iniciar servidor
./build/k8s-hpa-manager web -f

# 3. Adicionar HPA novo (sem baseline)
curl -X POST http://localhost:8080/api/v1/monitoring/hpa \
  -H "Authorization: Bearer poc-token-123" \
  -H "Content-Type: application/json" \
  -d '{"cluster":"akspriv-prod-admin","namespace":"prod","hpa":"app1"}'

# Esperado na resposta:
# {
#   "status": "success",
#   "has_baseline": false,
#   "message": "HPA added, collecting 3 days of historical data..."
# }

# 4. Verificar logs
# Esperado:
# "Iniciando coleta histórica de 3 dias em background"
# "Coletando dados históricos do Prometheus duration=72h"
# "Dados históricos coletados com sucesso snapshots_collected=XXX"
# "Coleta histórica de 3 dias concluída"

# 5. Consultar SQLite
sqlite3 ~/.k8s-hpa-manager/monitoring.db "SELECT COUNT(*) FROM snapshots WHERE namespace='prod' AND hpa_name='app1';"
# Esperado: ~864 snapshots (3 dias * 24h * 12 snapshots/hora com step de 5min)
```

### Teste 3: Fila Alternada de Portas
```bash
# 1. Adicionar 4 HPAs (2 em cada porta)
# cluster1 -> porta 55554 (par)
# cluster2 -> porta 55553 (ímpar)
# cluster3 -> porta 55554 (par)
# cluster4 -> porta 55553 (ímpar)

# 2. Monitorar logs durante scan
# Esperado (ordem alternada):
# "Escaneando cluster cluster=cluster2 port=55553"  # Primeiro da porta ímpar
# "Escaneando cluster cluster=cluster1 port=55554"  # Primeiro da porta par
# "Escaneando cluster cluster=cluster4 port=55553"  # Segundo da porta ímpar
# "Escaneando cluster cluster=cluster3 port=55554"  # Segundo da porta par
```

---

## 📝 Notas Técnicas

### Port-Forward vs Prometheus Service Discovery

**Por que port-forward e não service discovery direto?**
- Prometheus pode estar em namespace privado (monitoring)
- Port-forward garante acesso via kubectl credentials
- Compatível com clusters AKS remotos via VPN

### Step de 5 Minutos para Dados Históricos

**Por que 5 minutos?**
- 3 dias * 24h * 12 snapshots/hora = ~864 snapshots
- SQLite pode armazenar facilmente (< 1MB por HPA)
- Resolução suficiente para detectar anomalias
- Não sobrecarrega Prometheus com queries muito granulares

### Alternância de Portas

**Por que alternar?**
- Evita sobrecarga de uma única porta
- Distribui load de rede entre 2 portas
- Permite paralelismo futuro (ler 2 clusters simultaneamente)
- Reduz chance de timeout por congestionamento

---

## 🚀 Ordem de Implementação Recomendada

1. **Fase 1 primeiro** (Port-Forward Persistente)
   - É a base para tudo funcionar
   - Sem isso, fases 2 e 3 não funcionam

2. **Fase 2 em paralelo** (Coleta Histórica)
   - Pode ser implementada independentemente
   - Requer Fase 1 funcionando para testar

3. **Fase 3 depois** (Fila Alternada)
   - Melhoria de performance
   - Não bloqueia funcionalidade básica

4. **Fase 4 contínua** (Cleanup)
   - Implementar desde o início
   - Testar a cada fase

---

## ⚠️ Avisos Importantes

1. **Não comitar com bugs conhecidos**
   - Sempre testar cada fase completamente
   - Reverter se algo quebrar

2. **Port-forwards órfãos são problemáticos**
   - Podem ocupar porta indefinidamente
   - Sempre garantir cleanup no shutdown
   - Usar `defer` em funções críticas

3. **SQLite pode crescer**
   - Implementar cleanup de dados antigos (> 30 dias)
   - Monitorar tamanho do banco

4. **Prometheus pode ficar lento**
   - Range queries de 3 dias podem demorar
   - Implementar timeout adequado (5 minutos)
   - Mostrar progresso na UI

---

**Fim do Documento TODO**
