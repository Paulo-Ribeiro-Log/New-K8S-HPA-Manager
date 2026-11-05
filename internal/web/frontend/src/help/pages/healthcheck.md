## Para que serve

Faz uma **varredura de saúde** em vários clusters de uma vez e lista o que está errado ou arriscado: Deployments, Services, ConfigMaps e Secrets, eventos, HPAs, PVCs, nodes, uso real contra requests, e (se você ligar) os **problems abertos do Dynatrace** cruzados com os problemas do Kubernetes.

## Quando usar

- Ter um **panorama da frota** no início do dia ou antes de uma janela de mudança.
- Achar o que está **degradado** sem abrir cluster por cluster.
- Cruzar um sintoma do Kubernetes com um **problem do Dynatrace** aberto.
- Montar uma lista de alertas (CSV ou Markdown) para acompanhar com as equipes.

## Como usar (passo a passo)

1. Escolha **o que varrer**: um **Filtro de Ambiente** (prod, homologação ou todos) **ou** a seleção manual de clusters (com busca). Os **namespaces** são opcionais: vazio significa **todos**.
2. Marque os **Tipos de Verificação**:

| Verificação | O que olha |
|---|---|
| **Deployments** | o estado dos Deployments e dos pods deles (pode ser lento: Deployments têm muitos pods) |
| **Services** | **testes de conectividade** com os Services (veja "Impacto") |
| **ConfigMaps e Secrets** | validação rápida das configurações |
| **Eventos** | eventos do Kubernetes de aviso, como `FailedScheduling` |
| **HPAs** | mínimo igual ao máximo, métricas ausentes, escala travada |
| **PVCs** | status e storage class dos volumes |
| **Nodes** | capacidade e utilização dos nodes (pods ativos contra a capacidade) |
| **Histórico de recursos** | uso real (P95 no Prometheus) contra o request configurado |
| **Rollback no Spinnaker** | deployment com rollback recente, como sinal de risco |
| **Problems do Dynatrace** | problems **abertos** no Dynatrace, correlacionados com o Kubernetes |
| **Sinais OneAgent** | métricas elevadas de entidades instrumentadas **sem** problem aberto |

3. Mantenha os **Filtros Inteligentes** ligados (padrão): eles descartam falsos positivos conhecidos, como ConfigMaps vazios (por exemplo `ingress-controller-leader`), Secrets de sistema sem dados e recursos internos do `kube-system`.
4. **Executar.** A tela **Ver Progresso** mostra o andamento, cluster por cluster, e dá para **cancelar**. Em vários clusters, eles rodam em paralelo.
5. Veja o **resultado** nas abas **Resultados** e **Métricas** (painel visual). Cada problema vem com **severidade** (crítico, aviso...), o recurso afetado e um atalho para os **logs detalhados**. Com o Dynatrace ligado, aparece a lista de **correlações K8s ↔ Dynatrace**.
6. **Histórico** reabre varreduras anteriores (filtro por cluster, status e período). **Exportar** gera a lista de alertas em **CSV** ou **Markdown**.

## Whitelist de falsos positivos

Dos resultados dá para **adicionar um alerta à whitelist**: a regra passa a descartar aquele tipo de achado nas próximas varreduras. Adicionar e remover regras exige perfil SRE.

## Permissões

**Executar, cancelar e excluir uma varredura exigem perfil SRE.** Ver resultados, histórico e métricas, não. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**A verificação de Services cria um pod temporário nos namespaces.** Ele usa a imagem `busybox:1.36`, executa os testes de conectividade por `exec` e é apagado ao final. Fora isso, o Health Check **só lê**. Os resultados ficam guardados **por 7 dias** e depois são apagados sozinhos.

## Cuidados e erros comuns

- **Pode haver ruído e também falsos negativos.** Os filtros e a whitelist reduzem o ruído, mas um problema fora dos tipos que você marcou não aparece.
- **Varrer todos os clusters demora e carrega as APIs.** Comece por um ambiente ou por poucos clusters.
- **O pod do teste de Services precisa baixar `busybox`** e ser aceito pelas políticas do namespace (Kyverno, Gatekeeper). Se for bloqueado, os Services aparecem como não verificados.
- **Dynatrace desligado = sem correlação.** A tela avisa "Ative Problems Dynatrace nas opções" e mostra a lista vazia.
- **Cluster inalcançável (VPN fora)** aparece como falha daquele cluster, não como saudável.
- **O histórico expira em 7 dias**: exporte o que quiser guardar.
- Um resultado **verde não prova** que a aplicação funciona: ele só diz que nada do que foi verificado acusou problema.

## Veja também

[Dynatrace](ajuda:dynatrace), [Cobertura Dynatrace](ajuda:dynatrace-coverage), [Pods](ajuda:pods), [Events](ajuda:events).
