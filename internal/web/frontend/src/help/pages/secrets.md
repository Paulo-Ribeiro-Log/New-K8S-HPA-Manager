Esta aba segue o padrão das [abas de recurso](ajuda:geral-abas-de-recurso) (lista, editor YAML, Diff, Validar e Aplicar). Aqui está o que é específico dela.

## Para que serve

Lista, cria, edita e exclui **Secrets**, vê o certificado de Secrets TLS e controla a sincronização com o **External Secrets** (Azure Key Vault).

## Quando usar

- Corrigir ou criar uma credencial no cluster.
- Ver quando vence o certificado de um Secret TLS e atualizá-lo.
- Testar uma credencial nova num ambiente de homologação sem o External Secrets desfazer a mudança.
- Forçar o External Secrets a ressincronizar com o Key Vault.

## Como usar (passo a passo)

1. Escolha o cluster e o namespace. A busca da lista procura por nome ou label.
2. **Clique em um Secret** para abrir o YAML. O botão **Recarregar YAML** relê do cluster.
3. Os valores em `data` ficam em **base64**. O botão **Encoded / Decoded** alterna a visão: em **Decoded** você edita em texto puro, e ao voltar o conteúdo é re-codificado. Valores que não são base64 válido são ignorados na decodificação.
4. Edite e siga o fluxo [Diff, Validar, Aplicar](ajuda:geral-abas-de-recurso#editar-com-seguranca-diff-validar-e-aplicar).
5. **Criar Secret** abre um assistente com modelos (**Delinea**, **Normal secret** e **Custom**), nome, namespace, annotations e pares chave/valor, com opção de codificar cada valor em base64 e prévia do YAML.

## O que é específico desta aba

| Ação | O que faz |
|---|---|
| **Ver Certificado TLS** | só em Secrets `kubernetes.io/tls`: mostra Subject, Issuer, SANs e validade |
| **Atualizar Certificado** | aparece quando o certificado está **expirando ou expirado**: troca `tls.crt` e `tls.key` |
| **Resync AKV** | só em Secrets com `akv` no nome: força o External Secrets a ressincronizar com o Azure Key Vault (anota o ExternalSecret com `force-sync`) |
| **Pausar sincronização** | apaga o **ExternalSecret** (o Secret fica intacto) para você editar à mão sem o operador reverter em segundos. Só habilita se o Secret é gerenciado por um ExternalSecret **com `deletionPolicy=Retain`** |
| **Retomar sincronização** | recria o ExternalSecret exatamente como estava e o operador volta a sincronizar |
| **Deletar Secret** | apaga o Secret, com diálogo de confirmação |
| **Describe** | o equivalente a `kubectl describe secret` (não mostra os valores) |

## Permissões

As ações que alteram só ficam disponíveis com permissão de escrita no namespace. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes). Os Secrets internos de revisão do Helm (`sh.helm.release.*`) podem ser removidos por qualquer usuário autenticado; os demais exigem perfil SRE.

## Impacto

**Altera o cluster, e pode derrubar aplicações na hora:** pods que leem o Secret como variável de ambiente só veem o valor novo ao reiniciar; os que montam como volume veem depois de um tempo. Ficam no Change History: criar, aplicar, **excluir**, Resync AKV, pausar e retomar a sincronização. **O conteúdo do Secret nunca vai para o histórico**: o aplicar grava só o SHA-256 e o tamanho do YAML, e a exclusão só a identificação.

## Cuidados e erros comuns

- **Pausar a sincronização é temporário por desenho:** enquanto estiver pausada, o Key Vault deixa de mandar. Retome ao terminar, ou o valor editado à mão fica para sempre.
- Sem `deletionPolicy=Retain`, apagar o ExternalSecret apagaria o Secret junto; por isso o botão fica bloqueado.
- **"illegal base64"** ao validar quer dizer que algum valor em `data` não está codificado. Use **Encoded**, ou deixe a ferramenta re-codificar.
- Quebra de linha no fim de uma senha também é um valor diferente: confira ao colar.
- Excluir um Secret em uso faz os pods falharem ao iniciar (`CreateContainerConfigError`). Confira antes quais workloads usam o Secret.

## Veja também

[ConfigMaps](ajuda:configmaps), [Remoção de Aplicação](ajuda:app-removal).
