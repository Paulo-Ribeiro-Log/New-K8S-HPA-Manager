# Plano de Melhoria — Health Checking de Deployments

## Visão Geral
Este documento consolida a análise do fluxo atual de health checking de Deployments e lista as melhorias sugeridas para aumentar precisão, robustez e valor operacional. Ele também funciona como lista de progresso para execução incremental das ações.

## Escopo Atual Coberto
- Conferência de réplicas desejadas versus prontas.
- Detecção de CrashLoopBackOff, ImagePullBackOff e erros de imagem.
- Avaliação da presença e falhas de liveness/readiness probes.
- Inspeção das condições `Progressing` e `Available` do Deployment.
- Geração de sugestões práticas e publicação de eventos SSE com progresso.

## Lacunas Identificadas
1. **Selector incompleto**: apenas a primeira label do selector é utilizada; matchExpressions não são considerados.
2. **Replicas desejadas nulas**: acessos diretos ao ponteiro podem gerar panic e não diferenciam deployments propositalmente escalados para zero.
3. **Métricas de recursos inoperantes**: campos de CPU/Memória nunca são preenchidos, ocultando saturação de pods.
4. **Listagens duplicadas**: a API de Deployments é chamada duas vezes por namespace (contagem e validação).
5. **Timeout não aplicado**: o parâmetro de timeout não limita chamadas individuais; cancelamentos demoram a surtir efeito.

## Benefícios Esperados
- **Selector completo**: elimina falsos positivos/negativos ao alinhar os pods analisados com o Deployment real.
- **Replicas nulas tratadas**: evita crashes e reduz ruído em ambientes com escalonamento zero-intencional.
- **Métricas de recursos**: permite decisões proativas frente a gargalos de CPU/Memória mesmo quando status básicos estão verdes.
- **Listagens reutilizadas**: diminui latência e pressão na API Kubernetes, especialmente em clusters grandes.
- **Timeout efetivo**: garante respostas rápidas ao usuário ao cancelar e reduz bloqueios em operações longas.

## Avaliação de Dificuldade
| Item | Dificuldade | Observações chave |
|------|-------------|-------------------|
| Selector completo | Média | Requer `metav1.LabelSelectorAsSelector` e tratamento de matchExpressions.
| Replicas nulas | Baixa | Checar ponteiro, definir default e ajustar relatórios.
| Métricas de recursos | Alta | Necessita integração com Metrics API/Prometheus e fallback para clusters sem métricas.
| Reuso de listagens | Média | Refatorar `CheckAll` preservando cálculo de progresso.
| Timeout efetivo | Média | Propagar `context.WithTimeout` por chamada e tratar `DeadlineExceeded` corretamente.

## Plano de Ação (Checklist)
- [x] Atualizar seletor de pods para usar `LabelSelectorAsSelector`, cobrindo labels e matchExpressions; ajustar fallback e testes.
- [x] Tratar `deployment.Spec.Replicas == nil`, diferenciando zero intencional e evitando panic; revisar mensagens e sugestões.
- [x] Integrar coleta de métricas de CPU/Memória (Metrics API ou Prometheus), preenchendo `DeploymentHealth` com tolerância a ausência de dados.
- [x] Refatorar `CheckAll` para reutilizar a listagem inicial de Deployments, garantindo consistência do progresso e reduzindo chamadas.
- [x] Aplicar `context.WithTimeout` nas operações por Deployment/Pod, repassando erros de deadline e validando integração com o cancelamento.

## Próximos Passos Gerais
1. Priorizar itens de menor esforço (replicas nulas, selector completo) para ganhos rápidos.
2. Definir abordagem e dependências para métricas de recursos (permissões, APIs disponíveis em cada cluster).
3. Planejar janelas de testes em um cluster de homologação antes de liberar em produção.
