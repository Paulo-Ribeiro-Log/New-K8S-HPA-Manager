## Para que serve

Lista os **eventos do Kubernetes** (`kubectl get events`) do cluster: o que o scheduler, o kubelet e os controllers estão dizendo sobre os recursos (pod que não agendou, imagem que não baixou, probe falhando).

## Quando usar

- Entender **por que** um pod não sobe ou reinicia.
- Ver o que mudou no cluster nos últimos minutos.
- Separar o que é aviso (`Warning`) do que é rotina (`Normal`).

## Como usar (passo a passo)

1. Escolha o cluster. Por padrão a lista mostra **todos os namespaces**; escolha um para restringir.
2. Use o filtro **Tipo** (Todos, Normal ou Warning). Para achar problema, comece por **Warning**.
3. Digite na busca: ela procura no nome do evento, namespace, reason, mensagem e no nome e tipo do objeto envolvido.
4. **Clique em uma linha** para abrir os detalhes: cluster, namespace, objeto envolvido (kind, nome, UID), primeira e última ocorrência, origem (componente e host) e a **mensagem completa**.

## O que as colunas mostram

| Coluna | Significado |
|---|---|
| **Tipo** | ícone de alerta para `Warning`, ícone de informação para `Normal` |
| **Objeto** | o recurso sobre o qual o evento fala (Pod, Deployment, Node...) |
| **Reason** | o motivo resumido, por exemplo `BackOff` ou `FailedScheduling` |
| **Mensagem** | o texto; passe o mouse para ver inteiro |
| **Count** | quantas vezes o evento se repetiu |
| **First / Last Seen** | primeira e última ocorrência |
| **Source** | componente que gerou o evento (e o host, no tooltip) |

A lista **atualiza sozinha a cada 30 segundos** e o botão **Atualizar** força a leitura na hora. Namespaces de sistema ficam ocultos até você usar **Mostrar Sistema**.

## Impacto

**Somente leitura.** Não altera nada no cluster.

## Cuidados e erros comuns

- O Kubernetes **guarda eventos por pouco tempo** (em geral cerca de uma hora). Evento antigo não aparece, e isso não quer dizer que nunca aconteceu.
- Um `Warning` com `Count` alto é um problema que continua acontecendo; um com `Count` 1 e `Last Seen` antigo costuma já ter passado.
- A busca é refeita no servidor e no navegador: se não achar nada, confira se o namespace de sistema não está oculto.

## Veja também

[Pods](ajuda:pods), [Containers](ajuda:containers), [Deployments](ajuda:deployments).
