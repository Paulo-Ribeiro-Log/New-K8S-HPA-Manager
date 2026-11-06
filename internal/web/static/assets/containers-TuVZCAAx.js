const a=`## Para que serve

Mostra os **containers** do cluster (principais, init e efêmeros) em uma tabela, e abre **logs** e **detalhes** de qualquer um deles.

## Quando usar

- Achar containers que reiniciam muito ou não estão prontos.
- Ver os logs de um container específico de um pod com vários.
- Descobrir qual imagem e versão cada container está rodando.

## Como usar (passo a passo)

1. Escolha o cluster e o namespace (ou todos). Namespaces de sistema ficam ocultos até você usar o botão do olho.
2. A tabela da direita lista **um container por linha**. Cada linha traz o tipo (\`main\`, \`init\` ou \`efêm.\`), o pod, a versão da imagem, se está pronto (**READY**), o **STATE**, os reinícios (**REST.**), o node e a idade.
3. Filtre pelo cabeçalho das colunas (tipo, state, node) e ordene clicando nelas. A busca do topo procura no nome do container, do pod, no namespace, na imagem e no node.
4. **Clique em uma linha** (ou use Enter, com ↑↓ para navegar) para abrir o container.
5. No painel aberto, a aba **Logs** mostra as últimas **1000 linhas** do container escolhido no seletor; há botões para recarregar, baixar o texto e inspecionar JSON de um trecho selecionado. A aba **Details** mostra o pod e o estado de cada container, e **Manifest** abre o YAML do pod (somente leitura).
6. **Voltar pra lista de containers** retorna à tabela.

A tabela **atualiza sozinha a cada 5 segundos** enquanto a aba está visível.

## Impacto

**Somente leitura.** Esta aba não edita, reinicia nem exclui nada; para isso use [Pods](ajuda:pods).

## Cuidados e erros comuns

- **Logs não são em tempo real:** é uma leitura das últimas 1000 linhas. Para acompanhar ao vivo, use os logs da aba Pods (com atualização automática).
- Container \`init\` que terminou com sucesso aparece como concluído; isso é normal, não é erro.
- Container que acabou de reiniciar pode ter logs vazios: a leitura é do container atual, não da execução anterior.

## Veja também

[Pods](ajuda:pods), [Events](ajuda:events).
`;export{a as default};
