# Análise: Visualização de Values de Releases Superseded no Helm

**Data**: 16 de janeiro de 2026  
**Contexto**: Análise da possibilidade de visualizar values de releases com status "superseded" na aba Helm

---

## 📋 Resumo Executivo

**Resposta**: ✅ **SIM, é possível** visualizar os values de releases com status "superseded" no Helm!

O comando `helm get values` suporta a flag `--revision` que permite obter os values de qualquer revisão específica do histórico, incluindo aquelas com status "superseded".

---

## 🔍 Análise Atual

### 1. Implementação Existente

#### Backend (Go)

**Arquivo**: [internal/pkg/helm/cli_client.go](internal/pkg/helm/cli_client.go)

**Função atual** (`getValues`):
```go
func (c *CLIClient) getValues(ctx context.Context, cluster ClusterTarget, namespace, release string, includeAll bool) (string, error) {
    args := []string{"get", "values", release, "--output", "yaml"}
    if includeAll {
        args = append(args, "--all")
    }
    if namespace != "" {
        args = append(args, "--namespace", namespace)
    }
    
    stdout, stderr, err := c.runCommand(ctx, cluster, args...)
    if err != nil {
        return "", fmt.Errorf("helm get values failed: %w (stderr: %s)", err, strings.TrimSpace(stderr))
    }
    
    return string(stdout), nil
}
```

**Limitação**: A função atual **NÃO aceita** um parâmetro de revisão, então sempre retorna os values da revisão atual (deployed).

#### Frontend (TypeScript/React)

**Arquivo**: [internal/web/frontend/src/components/HelmReleaseDetails.tsx](internal/web/frontend/src/components/HelmReleaseDetails.tsx)

**Aba de Histórico** (linha ~700):
```tsx
const HistoryTab = ({
  revisions,
  loading,
  currentRevision,
}: {
  revisions: any[];
  loading: boolean;
  currentRevision: number;
}) => {
  // ...
  return (
    <div className="space-y-2">
      {revisions.map((revision) => (
        <Card key={revision.revision}>
          {/* Exibe: revision, status, description, updatedAt */}
          {/* Botão Rollback para revisões não-atuais */}
        </Card>
      ))}
    </div>
  );
};
```

**Limitação**: A aba de histórico exibe:
- ✅ Número da revisão
- ✅ Status (deployed, superseded, failed, etc.)
- ✅ Descrição
- ✅ Data de atualização
- ❌ **NÃO exibe** um botão ou modal para visualizar os values daquela revisão

---

## 🎯 Solução Proposta

### Opção 1: Botão "Ver Values" no Histórico (Recomendada)

#### Backend: Adicionar suporte a `--revision`

**Modificação em** `internal/pkg/helm/cli_client.go`:

```go
// Adicionar campo opcional em GetReleaseOptions
type GetReleaseOptions struct {
    Cluster    ClusterTarget
    Namespace  string
    Release    string
    IncludeAll bool
    Revision   int  // NOVO: 0 = current, >0 = specific revision
}

// Atualizar getValues para aceitar revision
func (c *CLIClient) getValues(ctx context.Context, cluster ClusterTarget, namespace, release string, includeAll bool, revision int) (string, error) {
    args := []string{"get", "values", release, "--output", "yaml"}
    if includeAll {
        args = append(args, "--all")
    }
    if namespace != "" {
        args = append(args, "--namespace", namespace)
    }
    if revision > 0 {
        args = append(args, "--revision", strconv.Itoa(revision))
    }
    
    stdout, stderr, err := c.runCommand(ctx, cluster, args...)
    if err != nil {
        return "", fmt.Errorf("helm get values failed: %w (stderr: %s)", err, strings.TrimSpace(stderr))
    }
    
    return string(stdout), nil
}

// Atualizar chamadas em GetRelease() para passar revision
valuesRaw, err := c.getValues(ctx, opts.Cluster, opts.Namespace, opts.Release, true, opts.Revision)
valuesRendered, err := c.getValues(ctx, opts.Cluster, opts.Namespace, opts.Release, false, opts.Revision)
```

#### API REST: Novo endpoint

**Modificação em** `internal/web/handlers/helm.go`:

```go
// Adicionar novo endpoint: GET /api/v1/helm/releases/:release/revisions/:revision/values
func (h *HelmHandler) GetRevisionValues(c *gin.Context) {
    cluster := strings.TrimSpace(c.Query("cluster"))
    if cluster == "" {
        h.respondMissingParam(c, "cluster")
        return
    }
    
    release := strings.TrimSpace(c.Param("release"))
    if release == "" {
        h.respondMissingParam(c, "release")
        return
    }
    
    revision, err := strconv.Atoi(c.Param("revision"))
    if err != nil || revision <= 0 {
        h.respondBadRequest(c, "INVALID_REVISION", err)
        return
    }
    
    namespace := strings.TrimSpace(c.Query("namespace"))
    
    // Buscar release com revisão específica
    detail, err := h.service.GetRelease(
        c.Request.Context(),
        cluster,
        helm.GetReleaseOptions{
            Namespace: namespace,
            Release:   release,
            Revision:  revision,
        },
    )
    if err != nil {
        h.respondInternalError(c, "GET_REVISION_VALUES_ERROR", err)
        return
    }
    
    c.JSON(http.StatusOK, gin.H{
        "success": true,
        "data": gin.H{
            "revision":        revision,
            "valuesRaw":       detail.ValuesRaw,
            "valuesRendered":  detail.ValuesRendered,
        },
    })
}

// Registrar rota no router
router.GET("/helm/releases/:release/revisions/:revision/values", handler.GetRevisionValues)
```

#### Frontend: Modal de Visualização

**Adicionar em** `internal/web/frontend/src/components/HelmReleaseDetails.tsx`:

```tsx
// Novo componente: Modal de visualização de values de revisão
const RevisionValuesModal = ({
  open,
  onOpenChange,
  cluster,
  release,
  namespace,
  revision,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: string;
  release: string;
  namespace: string;
  revision: number;
}) => {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [valuesRaw, setValuesRaw] = useState("");
  const [valuesRendered, setValuesRendered] = useState("");
  const [showRendered, setShowRendered] = useState(false);

  useEffect(() => {
    if (open) {
      fetchRevisionValues();
    }
  }, [open, revision]);

  const fetchRevisionValues = async () => {
    setLoading(true);
    setError(null);
    
    try {
      const params = new URLSearchParams({ cluster });
      if (namespace) params.set("namespace", namespace);
      
      const response = await fetch(
        `/api/v1/helm/releases/${release}/revisions/${revision}/values?${params}`,
        { headers: getAuthHeaders() }
      );
      
      const data = await response.json();
      
      if (!response.ok || !data.success) {
        throw new Error(data.error?.message || "Failed to fetch revision values");
      }
      
      setValuesRaw(data.data.valuesRaw);
      setValuesRendered(data.data.valuesRendered);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl max-h-[90vh]">
        <DialogHeader>
          <DialogTitle>
            Values - Revisão {revision}
          </DialogTitle>
          <DialogDescription>
            {release} • {namespace} • {revision === currentRevision ? "Atual" : "Superseded"}
          </DialogDescription>
        </DialogHeader>
        
        {loading && <Loader2 className="h-6 w-6 animate-spin" />}
        
        {error && (
          <div className="text-sm text-destructive">{error}</div>
        )}
        
        {!loading && !error && (
          <div className="space-y-2">
            {/* Toggle Raw/Rendered */}
            <div className="flex gap-2">
              <Button
                size="sm"
                variant={!showRendered ? "default" : "outline"}
                onClick={() => setShowRendered(false)}
              >
                Raw
              </Button>
              <Button
                size="sm"
                variant={showRendered ? "default" : "outline"}
                onClick={() => setShowRendered(true)}
              >
                Renderizado
              </Button>
            </div>
            
            {/* Editor YAML (read-only) */}
            <div className="border rounded-lg overflow-hidden" style={{ height: "600px" }}>
              <MonacoYamlEditor
                value={showRendered ? valuesRendered : valuesRaw}
                readOnly={true}
                height={600}
              />
            </div>
            
            {/* Botão Export */}
            <Button
              size="sm"
              variant="outline"
              onClick={() => {
                const blob = new Blob([valuesRaw], { type: 'text/yaml' });
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = `${release}-revision-${revision}-values.yaml`;
                a.click();
                URL.revokeObjectURL(url);
              }}
            >
              <Download className="h-4 w-4 mr-2" />
              Exportar Values
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
};

// Atualizar HistoryTab para incluir botão "Ver Values"
const HistoryTab = ({ revisions, loading, currentRevision }) => {
  const [selectedRevision, setSelectedRevision] = useState<number | null>(null);
  
  return (
    <div className="space-y-2">
      {revisions.map((revision) => (
        <Card key={revision.revision}>
          <div className="flex items-start justify-between gap-2">
            <div className="flex-1">
              {/* ... informações da revisão ... */}
            </div>
            
            <div className="flex gap-2">
              {/* Botão Ver Values (para qualquer revisão) */}
              <Button
                size="sm"
                variant="outline"
                className="gap-1"
                onClick={() => setSelectedRevision(revision.revision)}
              >
                <Eye className="h-3 w-3" />
                Ver Values
              </Button>
              
              {/* Botão Rollback (só para não-atuais) */}
              {revision.revision !== currentRevision && (
                <Button size="sm" variant="outline" className="gap-1">
                  <RotateCcw className="h-3 w-3" />
                  Rollback
                </Button>
              )}
            </div>
          </div>
        </Card>
      ))}
      
      {/* Modal de visualização */}
      <RevisionValuesModal
        open={selectedRevision !== null}
        onOpenChange={(open) => !open && setSelectedRevision(null)}
        cluster={cluster}
        release={release}
        namespace={namespace}
        revision={selectedRevision || 0}
      />
    </div>
  );
};
```

---

### Opção 2: Dropdown de Revisões na Aba Values

Adicionar um dropdown na aba "Values" para selecionar qual revisão visualizar:

```tsx
<Select 
  value={selectedRevision.toString()} 
  onValueChange={(rev) => loadRevisionValues(parseInt(rev))}
>
  <SelectTrigger>
    <SelectValue />
  </SelectTrigger>
  <SelectContent>
    {revisions.map(rev => (
      <SelectItem key={rev.revision} value={rev.revision.toString()}>
        Revisão {rev.revision} {rev.revision === currentRevision ? "(Atual)" : `(${rev.status})`}
      </SelectItem>
    ))}
  </SelectContent>
</Select>
```

---

## 📊 Comparação de Opções

| Aspecto | Opção 1: Botão no Histórico | Opção 2: Dropdown na Aba Values |
|---------|----------------------------|----------------------------------|
| **UX** | ⭐⭐⭐⭐⭐ Mais intuitivo | ⭐⭐⭐ Menos visível |
| **Isolamento** | ✅ Modal separado | ❌ Modifica aba principal |
| **Implementação** | 🟡 Média complexidade | 🟢 Simples |
| **Comparação** | ✅ Permite comparar com diff | ✅ Permite comparar com diff |
| **Export** | ✅ Fácil adicionar | ✅ Fácil adicionar |
| **Espaço UI** | ✅ Não sobrecarrega | 🟡 Adiciona controle extra |

**Recomendação**: **Opção 1** - Botão "Ver Values" no histórico com modal dedicado.

---

## 🚀 Plano de Implementação

### Fase 1: Backend (1-2h)
1. ✅ Adicionar campo `Revision int` em `GetReleaseOptions`
2. ✅ Atualizar função `getValues()` para suportar `--revision`
3. ✅ Modificar `GetRelease()` para passar revision opcional
4. ✅ Criar endpoint REST `GET /releases/:release/revisions/:revision/values`
5. ✅ Testar comando: `helm get values <release> --revision 2 -n <namespace>`

### Fase 2: Frontend (2-3h)
1. ✅ Criar componente `RevisionValuesModal`
2. ✅ Adicionar hook `useFetchRevisionValues()`
3. ✅ Integrar botão "Ver Values" na `HistoryTab`
4. ✅ Implementar toggle Raw/Rendered
5. ✅ Adicionar botão Export YAML
6. ✅ Testes manuais com releases superseded

### Fase 3: Melhorias Opcionais (1h)
- 🔄 Cache de values de revisões no frontend
- 🔄 Botão "Comparar com Atual" (diff side-by-side)
- 🔄 Badge visual para status (deployed/superseded/failed)
- 🔄 Tooltip com metadados da revisão

---

## ✅ Testes Necessários

### 1. Criar cenário de teste
```bash
# 1. Instalar release
helm install myapp bitnami/nginx -n default --set replicaCount=1

# 2. Fazer upgrade (cria revisão 2 superseded)
helm upgrade myapp bitnami/nginx -n default --set replicaCount=2

# 3. Fazer outro upgrade (cria revisão 3 superseded)
helm upgrade myapp bitnami/nginx -n default --set replicaCount=3

# 4. Verificar histórico
helm history myapp -n default
```

### 2. Validar comando Helm
```bash
# Deve funcionar para qualquer revisão
helm get values myapp -n default --revision 1 --all
helm get values myapp -n default --revision 2 --all
helm get values myapp -n default --revision 3 --all  # atual (deployed)
```

### 3. Testar API
```bash
curl -X GET "http://localhost:8080/api/v1/helm/releases/myapp/revisions/1/values?cluster=local&namespace=default" \
  -H "Authorization: Bearer poc-token-123"
```

### 4. Testar UI
- [ ] Clicar em release na lista
- [ ] Ir para aba "Histórico"
- [ ] Ver múltiplas revisões (deployed + superseded)
- [ ] Clicar "Ver Values" em revisão superseded
- [ ] Validar exibição de values raw e rendered
- [ ] Testar export YAML
- [ ] Comparar values entre revisões diferentes

---

## 📝 Considerações Técnicas

### Permissões Helm
O comando `helm get values --revision` requer que:
- O release ainda exista (não foi uninstalled)
- O usuário tenha permissões de leitura no namespace
- A secret do Helm ainda esteja presente (`sh.helm.release.v1.<release>.v<revision>`)

### Performance
- Values de revisões antigas são armazenados como Secrets no Kubernetes
- Fetch de values é rápido (~100-300ms)
- Considerar cache no frontend para revisões já visualizadas

### Limitações
- Releases uninstalled: histórico perdido (a menos que `--keep-history`)
- Namespaces deletados: revisões inacessíveis
- Permissões RBAC: podem bloquear acesso a secrets antigos

---

## 🎯 Conclusão

✅ **É totalmente possível** visualizar values de releases superseded no Helm!

A funcionalidade já está disponível nativamente no Helm CLI através da flag `--revision`. A implementação requer:

1. **Backend**: Adicionar suporte a `--revision` na função `getValues()` + novo endpoint REST
2. **Frontend**: Adicionar botão "Ver Values" no histórico + modal de visualização
3. **Testes**: Validar com cenários de múltiplas revisões

**Complexidade**: 🟡 Média (4-6h de desenvolvimento)  
**Impacto**: ⭐⭐⭐⭐⭐ Alto (funcionalidade muito útil para auditoria e troubleshooting)  
**Prioridade**: 🔥 Alta (complementa bem a aba Helm já existente)

---

## 📚 Referências

- [Helm Get Values Documentation](https://helm.sh/docs/helm/helm_get_values/)
- [Helm History Documentation](https://helm.sh/docs/helm/helm_history/)
- Código atual: [internal/pkg/helm/cli_client.go](internal/pkg/helm/cli_client.go#L480-L490)
- Frontend: [internal/web/frontend/src/components/HelmReleaseDetails.tsx](internal/web/frontend/src/components/HelmReleaseDetails.tsx#L700-L765)
