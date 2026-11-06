const e=`## Para que serve

Um **navegador universal de recursos do Kubernetes**, no estilo do k9s: escolha **qualquer tipo de recurso** (nativo ou CRD), veja o **YAML**, o \`describe\` e, se for um Pod, os **logs**, e edite, valide, aplique ou exclua.

## Quando usar

- Olhar um recurso que **não tem aba própria** (CRDs como \`ExternalSecret\`, \`Certificate\`, e recursos como \`NetworkPolicy\`, \`ClusterRole\`).
- Conferir o YAML real de qualquer objeto, de qualquer namespace.
- Fazer uma correção pontual em um recurso raro, sem sair da aplicação.

## Como usar (passo a passo)

1. Escolha o **cluster** e o **namespace** (ou **Todos os namespaces**).
2. No seletor de **tipo de recurso**, digite para buscar (por exemplo, \`hpa\` encontra \`horizontalpodautoscalers\`). A lista separa **Recursos Built-in** (do core do Kubernetes) dos **CRDs**. Só entram tipos que o cluster permite **listar** e **ler**.
3. A lista de **itens** daquele tipo aparece; filtre pelo nome e **clique em um** para abrir o **manifesto YAML**.
4. Ferramentas do item:
   - **kubectl describe**;
   - **Recarregar YAML do cluster**;
   - **Logs**, só para Pods: escolha o container (há um **container padrão**), atualize e **exporte como \`.txt\`**;
   - **Editor ou Diff**: alterna entre editar e ver o que mudou em relação ao cluster.
5. Para alterar: edite o YAML, use **Validar (dry-run)**, confira o **diff** e **Aplicar** (com a tela **Confirmar Apply**). **Excluir** pede **Confirmar Delete**.

## Permissões

Listar, ler YAML, descrever e ver logs não exigem o grupo SRE. **Aplicar e excluir exigem perfil SRE** e permissão real no cluster. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster e pode apagar qualquer coisa.** Funciona para **qualquer tipo de recurso**, inclusive Secrets, Namespaces, ClusterRoles e CRDs. O **Aplicar** primeiro faz um \`kubectl replace\` (**substitui o objeto inteiro**, como o \`kubectl edit\`); se o recurso não existe, cria com \`apply\`; e, se o \`replace\` falhar por outro motivo, tenta um **server-side apply com \`--force-conflicts\`**, que **toma a propriedade** dos campos de outro gerenciador (Helm, operator).

Ficam no Change History: **Aplicar** (\`explorer_apply\`, só quando **não** é dry-run) e **Excluir** (\`explorer_delete\`), com o recurso no formato \`tipo/namespace/nome\`, o cluster, o autor e o resultado, **inclusive as falhas**. O registro do Aplicar leva só o **SHA-256 e o tamanho do YAML**, o grupo da API e se o \`force\` foi usado, **nunca o conteúdo** (que pode ser um Secret). Prefira a aba própria do recurso quando houver uma: ela valida mais coisas para aquele tipo.

## Cuidados e erros comuns

- **O histórico não guarda o conteúdo que você aplicou**, só a identificação e o hash. Para saber *o que* mudou, compare com o estado anterior (use o **Diff** antes de aplicar) ou guarde o YAML no Git.
- **Excluir é definitivo e vale para qualquer tipo.** Excluir um Namespace apaga tudo dentro dele; excluir um CRD apaga todos os objetos daquele tipo. Leia o nome e o namespace na confirmação.
- **O YAML de um Secret mostra os valores** (em base64). Quem tem acesso a esta aba lê Secrets de qualquer namespace que o RBAC permitir.
- **Mudanças em recursos gerenciados por Helm, Argo CD ou Flux** são desfeitas no próximo sync. Ajuste a origem.
- **Substituir o objeto inteiro apaga o que você não colocou no YAML.** Um campo que existia no cluster e que você removeu do editor **some**. Recarregue o YAML do cluster antes de editar.
- **Alguns campos são imutáveis** depois que o objeto existe: o cluster recusa a alteração e a mensagem aparece na tela.
- **Logs só existem para Pods.** Para workloads, abra o Pod.
- **Tipos sem permissão de listar** não aparecem na lista: não é bug.

## Veja também

[Secrets](ajuda:secrets), [Gateways](ajuda:gateways), [Namespaces](ajuda:namespaces).
`;export{e as default};
