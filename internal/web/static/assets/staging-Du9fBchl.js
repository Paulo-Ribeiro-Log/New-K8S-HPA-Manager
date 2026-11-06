const a=`## Para que serve

O **Staging** é a fila de alterações pendentes de **HPAs** e **Node Pools**. Você prepara várias mudanças, revisa tudo num lugar só e aplica de uma vez, em vez de mexer no cluster a cada edição.

## Quando usar

- Mudar vários HPAs ou node pools numa mesma janela de manutenção.
- Conferir o "de → para" de tudo antes de aplicar.
- Guardar um conjunto de mudanças para aplicar depois (por sessão).

## Como usar (passo a passo)

1. Nas abas [HPAs](ajuda:hpas) e [Node Pools](ajuda:nodepools), edite o item e use **Salvar (Staging)**. Nada muda no cluster nesse momento.
2. Abra a aba **Staging**. A lista da esquerda mostra cada item, com o selo **HPA** ou **Node Pool**, o selo **Modified** quando mudou e o resumo do que muda (por exemplo \`Min: 2 → 3 | Max: 10 → 12\` ou \`Nodes: 3 → 5\`). A busca procura por nome, namespace e cluster.
3. **Clique em um item** para abrir o mesmo editor da aba de origem e ajustar de novo.
4. O ícone da lixeira tira um item da fila. **Clear All** esvazia o Staging inteiro.
5. Para aplicar, use o botão com o **contador de alterações** no cabeçalho (ele só aparece quando há algo pendente). Abre a confirmação **Confirmar Alterações**, onde cada item pode ser **Aplicado** individualmente, ter o conteúdo **editado** ou ser **removido da lista**, ou onde você usa **Aplicar Todos**. **Cancelar e Limpar** descarta a fila.
6. Os node pools marcados para troca sequencial (\`*1\` e \`*2\`) são aplicados **em sequência**, com cordon e drain, e nunca em paralelo.

## Guardar para depois: sessões

O Staging fica **só na memória do navegador**: recarregar a página ou fechar a aba perde a fila. Para guardá-la, use **Save Session** no cabeçalho; **Load Session** traz de volta uma sessão salva. A sessão pode ser salva a partir das mudanças pendentes ou de um snapshot do cluster.

## Permissões

Aplicar só funciona com permissão de escrita no cluster. Antes de aplicar, a ferramenta confere a conexão com a **VPN**. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Aplicar altera o cluster.** Salvar no Staging, não: ele só guarda a intenção. Cada HPA e node pool aplicado fica no Change History, com o antes e o depois.

## Cuidados e erros comuns

- **Staging não é histórico:** ele mostra o que **ainda não foi aplicado**. Depois de aplicar, o item sai da fila e o registro passa a ficar no Change History.
- **Valores podem ter ficado velhos.** O Staging guarda o que você viu ao editar; se alguém mudou o cluster no meio tempo, a confirmação compara com o estado atual, então leia o "de → para".
- **Mudar requests e limits de um HPA reinicia pods** ao aplicar; confira os rollouts marcados antes de **Aplicar Todos**.
- **Mudanças em node pools custam dinheiro** e podem remover VMs: veja os avisos de [Node Pools](ajuda:nodepools).
- Itens de clusters diferentes ficam na mesma fila: confira o cluster de cada um antes de aplicar.

## Veja também

[HPAs](ajuda:hpas), [Node Pools](ajuda:nodepools), [FinOps](ajuda:finops).
`;export{a as default};
