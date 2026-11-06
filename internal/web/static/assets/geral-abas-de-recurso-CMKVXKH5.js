const e=`## Para que serve

ConfigMaps, Ingresses, Services, VPAs, DaemonSets e StatefulSets funcionam do mesmo jeito: você escolhe um recurso, edita o **YAML**, confere a diferença, valida e aplica. Esta página explica o que é comum; cada aba tem a sua página só com o que muda.

## Como a tela é organizada

- Uma **tabela de recursos** no estilo do k9s, com busca e filtro de **namespace**. DaemonSets e StatefulSets destacam os que estão com falha.
- Ao abrir um recurso, aparece o **manifesto YAML** num editor, com **Voltar para lista** para sair.
- O cluster vem do seletor do cabeçalho.

## Editar com segurança: Diff, Validar e Aplicar

1. **Edite** o YAML.
2. **Diff** mostra a diferença entre o seu YAML e o que está no cluster. ConfigMaps, Ingresses, DaemonSets e StatefulSets também abrem o diff em tela cheia.
3. **Validar** faz um *server-side apply* com **dry-run**: o cluster confere o manifesto e roda as políticas de admissão, mas **não grava nada**. Se aparecer "Dry-run falhou" ou "Erro de validação", o motivo vem do próprio cluster.
4. **Aplicar** pede confirmação e faz o *server-side apply* de verdade. Em ConfigMaps e Ingresses, sem alteração a ferramenta avisa "Nenhuma alteração para aplicar".

> **Cuidado:** aplicar muda o recurso no cluster na hora. Se o recurso é reaplicado por Helm, Spinnaker ou outro pipeline, a sua edição pode ser **sobrescrita** no próximo deploy.

## Recursos do editor

| Recurso | ConfigMaps | Ingresses | Services | VPAs | DaemonSets | StatefulSets |
|---|---|---|---|---|---|---|
| Describe (\`kubectl describe\`) | sim | sim | sim | sim | sim | sim |
| Desfazer/Refazer (Ctrl+Z / Ctrl+Y) | sim | sim | não | não | sim | sim |
| Tela cheia | sim | sim | sim | sim | sim | sim |
| Abrir em **Edição Lado a Lado** | sim | sim | não | não | sim | sim |
| **Criar** novo | sim | sim | sim | não | não | não |
| **Excluir** | sim | sim | sim | sim | sim | sim |

**Excluir** pede confirmação. Em ConfigMaps e Ingresses, **criar** exige escolher um **namespace específico** (não "todos").

> **Atenção ao Ctrl+S:** em **ConfigMaps e Ingresses**, **Ctrl+S não aplica nada**: ele só guarda um **checkpoint** no histórico de desfazer do editor, no seu navegador. Para gravar no cluster é preciso **Aplicar**. Nas outras quatro abas não existe esse checkpoint.

## Permissões

- Os botões que alteram (aplicar, criar, excluir, reiniciar, escalar) só ficam disponíveis quando o seu usuário tem permissão de escrita no namespace, segundo o **RBAC do cluster**. A aba **VPAs** não consulta o RBAC antes: se faltar permissão, o erro vem do cluster ao aplicar.
- As rotas de escrita são de perfil SRE no desenho da aplicação; quem barra de fato é o RBAC do cluster.

## Impacto e Change History

**Altera o cluster.** Tudo o que altera o cluster fica registrado no **Change History**:

| Aba | Aplicar | Criar | Excluir | Outras |
|---|---|---|---|---|
| ConfigMaps | sim | sim | sim | |
| Ingresses | sim | sim | sim | |
| Services | sim | sim | sim | |
| VPAs | sim | n/a | sim | |
| DaemonSets | sim | n/a | sim | restart: sim |
| StatefulSets | sim | n/a | sim | restart: sim; scale: sim |

Só o registro de identificação (recurso, cluster, autor, resultado) é gravado, nunca o conteúdo. Validar (dry-run) nunca é registrado, por não alterar nada.

## Cuidados e erros comuns

- **Excluir é definitivo** e não há desfazer no cluster. Antes de excluir um ConfigMap, veja se ele está em uso (aba ConfigMaps).
- Alguns campos são **imutáveis** e o cluster os recusa: o \`clusterIP\` de um **Service** e boa parte do \`spec\` de um **StatefulSet** (como \`volumeClaimTemplates\` e \`serviceName\`). O erro aparece ao validar ou aplicar.
- Recursos com **finalizers** podem ficar em *Terminating* depois de excluídos.

## Veja também

[Deployments](ajuda:deployments), [Remoção de Aplicação](ajuda:app-removal).
`;export{e as default};
