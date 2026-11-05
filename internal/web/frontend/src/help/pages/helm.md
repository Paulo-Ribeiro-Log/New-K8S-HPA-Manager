## Para que serve

Lista os **releases Helm** do cluster e permite ver os values e o manifest de cada um, o histórico de revisões, e fazer **install, upgrade, rollback e uninstall**.

## Quando usar

- Ver qual versão de chart e de aplicação cada release está rodando.
- Mudar os values de um release.
- Voltar a uma revisão anterior depois de um deploy ruim.
- Instalar um chart novo ou remover um release.

## Como usar (passo a passo)

1. Escolha o cluster. A lista mostra os releases; filtre por namespace, use **Sistema** para mostrar os namespaces de sistema e busque por nome, namespace, chart, versão da aplicação ou status. O botão de atualizar relê do cluster.
2. **Clique em um release.** O cabeçalho mostra nome, status, chart, versão da aplicação, **revisão**, namespace e links de origem do chart. **Export Values** baixa os values em um arquivo `<release>-values.yaml`.
3. O menu **⋮** tem **Install**, **Upgrade**, **Rollback** (desabilitado quando só existe uma revisão) e **Uninstall**.
4. As abas do release são **Valores**, **Histórico** e **Manifest**.

## As abas

| Aba | O que faz |
|---|---|
| **Valores** | edita os values em YAML (desfazer, refazer, tela cheia). **Validar** confere só a **sintaxe do YAML**; **Aplicar** abre a confirmação com o diff. Dá para alternar para a visão dos values renderizados, somente leitura |
| **Histórico** | as revisões, com a atual marcada. Cada uma tem **Rollback** direto para ela e a lixeira, que apaga o **Secret** que o Helm guarda daquela revisão |
| **Manifest** | o manifest renderizado do release (o que o chart gerou), **somente leitura**, com **copiar**, **baixar** e tela cheia; mostra também as Release Notes |

## Ações

| Ação | O que faz |
|---|---|
| **Install** | cria um release novo: nome, namespace (criado se não existir), chart (`repo/chart` ou caminho local), versão do chart opcional, **Force** e **Dry run** |
| **Upgrade** | atualiza o release com novos values e, se quiser, outro chart ou outra versão (**Force** e **Dry run** também) |
| **Rollback** | volta a uma revisão anterior, com **Force** (recria recursos se preciso). Mostra o log da operação ao vivo |
| **Uninstall** | remove o release e todos os recursos dele, depois de uma confirmação |

Todas mostram o **log da operação em tempo real** e, ao terminar com sucesso, a lista é atualizada.

## Como o chart é encontrado

No **Apply** da aba Valores, o campo **Chart Reference** é opcional: **vazio** mantém o chart atual e muda só os values; **preenchido** troca de chart (formato `repositorio/chart`). Quando é preciso achar o chart do release, o servidor procura primeiro um arquivo `.tgz` em `~/.k8s-hpa-manager/storaged-helm/` e depois nos **repositórios Helm já configurados** na máquina do servidor. Se não achar, o erro vem do `helm upgrade` e aparece no log.

## Permissões

Quem executa é o **servidor**, com o `helm` e o acesso ao cluster que ele tem. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Altera o cluster e pode derrubar aplicações.** Install, upgrade, rollback e uninstall ficam no Change History (sem dry-run), com release, namespace, chart, versão e revisão. Os **values nunca vão para o histórico**: só o tamanho e o SHA-256, porque podem conter segredo. **Apagar o Secret de uma revisão** também fica registrado, como a exclusão de qualquer Secret (`delete_secret`).

## Cuidados e erros comuns

- **O upgrade usa só os values que você envia mais os padrões do chart.** Ele não reaproveita os values antigos (não usa `--reuse-values`): o que você tirar do YAML **volta ao padrão do chart**. Por isso a aba Valores envia o YAML inteiro; não apague trechos sem querer.
- **O Manifest não é editável, de propósito.** Ele é a **saída** do chart, não uma entrada: para mudar o release, edite os **Valores** (ou o chart). Para mexer em um recurso avulso, use o [Explorer](ajuda:explorer). Se alguém tentar enviar um manifest como values pela API, o servidor **recusa** (`VALUES_LOOK_LIKE_MANIFEST`), porque o Helm faria o release voltar aos padrões do chart.
- **Validar** só confere se o YAML está bem formado; não confere se o chart aceita os values. Use o **Dry run** do Upgrade para isso.
- **Rollback com Force** recria recursos e pode causar indisponibilidade.
- **Uninstall remove tudo do release**, inclusive o que ele criou; não pede para digitar o nome. Confira o namespace antes.
- **Apagar o Secret de uma revisão** tira essa revisão do histórico do Helm: depois disso não dá mais para voltar a ela. É uma limpeza, não um rollback.
- Um release com status `failed` ou `pending-*` precisa de atenção antes de novo upgrade: veja o histórico.

## Veja também

[Secrets](ajuda:secrets), [Deployments](ajuda:deployments), [Pods](ajuda:pods).
