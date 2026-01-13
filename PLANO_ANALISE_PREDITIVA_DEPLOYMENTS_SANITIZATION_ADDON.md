# Adição ao Plano - Sanitização de Dados Sensíveis

## 🔒 Sanitização de Dados Sensíveis (CRÍTICO)

**IMPORTANTE**: Todos os dados coletados devem passar pelo sistema **Sanitizer** antes de serem:
- Enviados para IA
- Exibidos na interface
- Exportados em relatórios
- Armazenados em cache/logs

### Dados que DEVEM ser Sanitizados:

1. **Informações de Identificação**:
   - Nomes de clusters (mascarar para "cluster-xxx")
   - Nomes de namespaces sensíveis (produção, staging)
   - IPs internos e ranges de rede
   - Nomes de nodes (mascarar identificadores únicos)

2. **Métricas e Valores**:
   - URLs de serviços externos
   - Connection strings
   - Tokens ou secrets acidentalmente expostos em variáveis de ambiente
   - Credenciais em annotations ou labels

3. **Logs e Eventos**:
   - Stack traces com paths internos
   - Mensagens de erro com informações sensíveis
   - Nomes de usuários ou emails

4. **Relatórios Exportados**:
   - Todos os dados devem ser sanitizados antes da exportação
   - PDF, Markdown e JSON devem conter apenas dados mascarados

### Implementação da Sanitização:

**No Backend (Go)**:

```go
import "your-project/internal/sanitizer"

// Antes de enviar para IA
func (a *Analyzer) AnalyzeDeployment(ctx context.Context, req PredictionRequest) (*PredictionResult, error) {
    // 1. Coletar métricas
    metrics := a.collectMetrics(ctx, req)
    
    // 2. SANITIZAR dados antes de enviar para IA
    sanitizedMetrics := sanitizer.SanitizeMetrics(metrics)
    
    // 3. Enviar para IA
    result := a.aiIntegrator.AnalyzeMetrics(ctx, req, sanitizedMetrics)
    
    // 4. SANITIZAR resposta da IA (pode conter dados refletidos)
    sanitizedResult := sanitizer.SanitizeResult(result)
    
    return sanitizedResult, nil
}

// Antes de exportar relatórios
func (rg *ReportGenerator) GenerateMarkdown() (string, error) {
    // Sanitizar todos os campos antes de gerar relatório
    sanitizedResult := sanitizer.SanitizeForExport(rg.result)
    return rg.templateMarkdown(sanitizedResult)
}
```

**No Frontend (TypeScript)**:

```tsx
import { sanitizeForDisplay } from '@/lib/sanitizer';

// Antes de exibir na UI
const displayMetrics = sanitizeForDisplay(result.raw_metrics);

// Antes de exportar
const exportReport = async (format: string) => {
    const sanitizedData = sanitizeForExport(result);
    // ... proceder com exportação
};
```

### Checklist de Sanitização:

- [ ] Integrar chamadas ao Sanitizer em todos os pontos de coleta
- [ ] Sanitizar antes de enviar para IA
- [ ] Sanitizar resposta da IA
- [ ] Sanitizar antes de exibir na UI
- [ ] Sanitizar antes de exportar relatórios
- [ ] Sanitizar logs e audit trails
- [ ] Testar com dados reais para validar eficácia
- [ ] Documentar padrões de sanitização para o time

### Benefícios da Sanitização:

1. ✅ **Conformidade**: Atende requisitos de segurança e compliance
2. ✅ **Proteção de Dados**: Impede vazamento de informações sensíveis
3. ✅ **Segurança da IA**: Evita exposição de dados em prompts enviados para IA externa
4. ✅ **Auditoria**: Facilita compartilhamento de relatórios sem riscos
5. ✅ **Confiança**: Permite uso seguro da funcionalidade em ambientes críticos
