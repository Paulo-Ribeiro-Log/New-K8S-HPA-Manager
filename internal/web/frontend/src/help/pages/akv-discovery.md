## Para que serve

Mostra o **conteúdo real de um Azure Key Vault** através de um **ExternalSecret temporário**, com um critério de busca (regexp) que **você escolhe**, sem mexer no ExternalSecret oficial do namespace. Serve para conferir o que o Vault tem, mesmo quando o `find` do ExternalSecret da aplicação não traz o item esperado.

## Quando usar

- Descobrir **por que um item do Key Vault não aparece** no Secret da aplicação (nome diferente, critério restritivo demais).
- Conferir o valor que está no Vault **sem alterar** o ExternalSecret original.
- Testar em homologação, quando o Key Vault não sincroniza (veja "Salvar como Secret").

## Como usar (passo a passo)

1. Escolha o **cluster** e o **namespace**.
2. Se já houver um **ExternalSecret** no namespace, escolha-o como **referência**: a ferramenta aproveita a origem e os critérios dele como sugestão. Se não achar nenhum (ou não conseguir listar), **preencha os campos à mão**.
3. Informe a **origem** dos dados: o **Tipo da origem** (`SecretStore` ou `ClusterSecretStore`) e o **Nome da origem**.
4. Defina o **Critério de busca (padrão de nome)**: um **regexp** sobre os nomes dos itens do Vault. O combo sugere os critérios já usados por ExternalSecrets do namespace e a opção **`*`** (Tudo, que vira `.*`, sem restrição). Aceita qualquer regexp que você digitar.
5. Preencha o **Motivo** (obrigatório, **registrado para auditoria**).
6. **Iniciar.** A ferramenta cria um **ExternalSecret de descoberta** com nome gerado e espera o External Secrets sincronizar (até 30 segundos).
7. Na janela de resultado (um editor), veja os **itens e valores**. Eles aparecem em **base64** por padrão; um botão alterna para o conteúdo **decodificado**, e **Ctrl+Shift+E** e **Ctrl+Shift+D** codificam e decodificam o trecho selecionado. **Copie** o que precisar.
8. **Encerrar e Remover** apaga o ExternalSecret de descoberta; o Secret resultante é recolhido **sozinho** pelo Kubernetes (coleta de lixo), porque pertence ao ExternalSecret. **Fechar a janela também tenta apagar** a sessão, em melhor esforço.

## Salvar como Secret

Para testes em **homologação**, às vezes o Key Vault não reflete o valor que você quer. A janela de resultado deixa **editar os valores à mão** e **Salvar como Secret**: aplica um **Secret real** no cluster (cria se não existir, **sobrescreve** se existir), com o nome que você indicar. Usa o mesmo caminho da aba [Secrets](ajuda:secrets): um *server-side apply* com `--force-conflicts`, que **toma a propriedade** dos campos editados mesmo quando o Secret é gerenciado por um External Secrets real. **Nunca escreve no Key Vault**, só no Secret do Kubernetes, e **o valor se perde no próximo sync bem-sucedido**.

## Permissões

Listar os ExternalSecrets e ver o status não exigem o grupo SRE. **Iniciar, ver os dados e encerrar exigem perfil SRE**, e o RBAC do cluster precisa permitir criar ExternalSecrets e ler Secrets no namespace. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Cria recursos temporários no cluster e expõe segredos reais.** Enquanto a sessão existe, o **Secret de descoberta contém os valores do Vault** e qualquer pessoa com permissão de ler Secrets no namespace pode lê-lo. Ficam no Change History: o **início** (`akv_discovery_start`, com a origem, o **regexp e o motivo**) e o **encerramento** (`akv_discovery_stop`). **Os valores nunca vão para o histórico.**

## Cuidados e erros comuns

- **O mesmo Key Vault costuma ser compartilhado por várias aplicações.** Um critério sem restrição (`*` ou `.*`) traz **segredos de outras aplicações** para dentro do namespace, mesmo que por pouco tempo. **Prefira um regexp específico.**
- **Sempre encerre a sessão.** Se a chamada de limpeza falhar (aba fechada, navegador travado), um **reaper** remove o ExternalSecret de descoberta depois de **30 minutos**, mas até lá os valores continuam no cluster.
- **A sessão fica só na memória do servidor.** Se o servidor reiniciar no meio, o reaper não sabe da sessão: confira o namespace e remova o ExternalSecret de descoberta à mão.
- **"Salvar como Secret" sobrescreve um Secret real.** Confira o nome e o namespace; em produção, não use.
- **A descoberta não altera o ExternalSecret oficial**, mas o External Secrets precisa estar instalado e a origem precisa existir e estar acessível.
- **Sem o Motivo, a descoberta não inicia.** Descreva o motivo de verdade: ele é a trilha de auditoria.

## Veja também

[Secrets](ajuda:secrets), [Dependencies](ajuda:dependencies).
