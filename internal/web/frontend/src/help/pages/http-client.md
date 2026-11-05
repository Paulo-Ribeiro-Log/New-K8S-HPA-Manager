## Para que serve

Monta e envia uma **requisição HTTP** (ou **cola um comando cURL**) e mostra a resposta, no estilo do Postman. Pode sair **do próprio servidor da aplicação** ou **de um pod do cluster**.

## Quando usar

- Testar uma API ou um endpoint sem abrir o Postman nem o `curl` à mão.
- Verificar se um **serviço interno do cluster** (`*.svc.cluster.local`) responde, partindo de um workload específico.
- Reproduzir uma chamada que alguém mandou como `curl`.
- Repetir uma requisição já feita (lista de execuções).

## Como usar (passo a passo)

1. Escolha **de onde a requisição sai**:
   - **Pelo servidor:** a requisição parte da máquina do servidor da aplicação e alcança o que a **rede e a VPN do servidor** alcançam.
   - **De um pod do cluster:** executa `curl` em um *ephemeral container* anexado a um pod do **Deployment** que você escolher (e, se quiser, o pod e o container específicos). Alcança serviços internos e **reflete NetworkPolicy e Istio** daquele workload.
2. Escolha o **método** (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD` ou `OPTIONS`) e digite a **URL**.
3. Se tiver um `curl` pronto, use **Colar cURL**: a ferramenta preenche método, URL, headers e corpo.
4. Nas abas da requisição, edite os **Headers** (chave e valor) e o **Body** (há um botão **Formatar JSON**; o corpo só vale em métodos que aceitam corpo).
5. Opções: **Seguir redirects** (desligado por padrão), **Ignorar certificado TLS (não recomendado)** e **Timeout** (padrão 30 s, máximo 120 s).
6. **Enviar.** A resposta mostra **status**, **tempo**, **tamanho**, **origem** (de onde saiu), os **headers** e o **corpo**, com **busca** no texto (Ctrl+F; Enter e Shift+Enter navegam entre os trechos).

## O que a resposta mostra

- Um status de erro HTTP (`404`, `500`) **não é erro da ferramenta**: aparece como resposta normal. Só quando não houve resposta (DNS, conexão recusada, timeout, TLS) é que a ferramenta mostra um erro.
- O corpo é limitado a **5 MB**: o que passar disso é cortado e marcado como **truncado**. Corpo binário é sinalizado.

## Lista de execuções

Cada envio entra na sua lista de **Execuções**, com método, URL, status e tempo. Dali você pode **abrir no formulário** (sem enviar), **re-executar** ou **remover**. A lista guarda as **últimas 200** execuções e é **só sua**.

## Permissões

**Enviar** exige perfil SRE. Colar cURL, listar pods e ver as suas execuções não exigem. No modo pod é preciso poder criar *ephemeral containers*. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Pode alterar sistemas externos**: `POST`, `PUT`, `PATCH` e `DELETE` fazem o que a API faz. Cada envio fica no Change History (`http_client_request`) com **método, host e caminho, modo e status**. **Query string, headers e corpo nunca vão para o histórico.**

## Cuidados e erros comuns

- **Headers e corpo ficam guardados na sua lista de execuções**, em texto puro, em um banco local do servidor (arquivo com permissão restrita ao dono). Isso inclui **tokens** (`Authorization`) e senhas que você tenha colado. **Remova da lista** as execuções com segredo quando terminar.
- **Pelo servidor, o alcance é o da rede do servidor**, não o da sua máquina nem o do cluster. A ferramenta **não bloqueia nenhum destino**: use com critério, principalmente contra endereços internos.
- **Não use `DELETE` ou `POST` de verdade em produção** só para testar. Prefira um ambiente de teste.
- **Ignorar o certificado TLS** desliga a verificação do servidor: só para diagnóstico.
- **No modo pod, a imagem do `curl` (`curlimages/curl:8.10.1`)** precisa ser baixada pelo node e o cluster precisa aceitar *ephemeral containers*.
- **Redirects desligados** fazem a resposta ser o próprio `3xx`; ligue **Seguir redirects** para ver o destino final.

## Veja também

[Teste de Latência](ajuda:latency-test), [Pods](ajuda:pods).
