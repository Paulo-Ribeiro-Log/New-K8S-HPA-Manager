## Para que serve

Usa um **modelo de IA** para **analisar** um Pod, Deployment, HPA ou Node com problema e devolver causa provável e próximos passos. Esta aba guarda o **histórico das análises**, as **estatísticas de uso** e a **configuração dos provedores de IA**.

## Quando usar

- Entender **por que** um pod está em `CrashLoopBackOff` ou um HPA não escala, sem ler tudo na mão.
- Consultar uma análise feita antes, ou exportá-la como relatório.
- Configurar qual provedor de IA a ferramenta usa.

## Como usar (passo a passo)

1. **Rodar uma análise:** na aba [Pods](ajuda:pods) (e nas de Deployments, HPAs e Nodes), selecione o recurso e clique em **Analisar com AI**. A análise não nasce aqui: esta aba mostra o resultado e o histórico.
2. Em **Diagnósticos**, veja o cartão de **Estatísticas de Uso** (análises feitas, por tipo de recurso e por provedor) e o **Status do provedor** (se o provedor escolhido responde, com botão para atualizar e atalho para as configurações).
3. O **histórico** lista as análises, com filtros por **severidade**, **provedor**, **cluster**, **namespace**, **tipo de recurso** e **período** (última semana, último mês). **Clique em uma análise** para abrir o detalhe completo, **exportar** o relatório (PDF, Markdown ou CSV) ou **apagá-la**.
4. Quando ainda não há histórico, a aba mostra um guia de início rápido.
5. Em **Configurações**, escolha o **provedor preferido** e preencha as credenciais.

## Provedores

| Provedor | Como configurar |
|---|---|
| **Ollama** (local, padrão e recomendado) | roda na máquina do servidor, sem enviar dados para fora; escolha o modelo |
| **Gemini** | **API Key** (Google AI Studio) ou **Vertex AI**, por login OAuth2 (inclusive SSO corporativo por WIF, no formato `poolID/providerID`) ou por **service account**; projeto e região |
| **OpenAI** | API Key; o campo de endpoint aceita qualquer API compatível, por exemplo o **GitHub Models**, com um PAT do GitHub no lugar da chave |
| **Claude** | API Key e modelo |
| **GitHub Copilot** | chave, endpoint e deployment do **Azure OpenAI** |

O **e-mail de IA** identifica as suas configurações (e vira o `login_hint` no login do Google). **Validar Formato** só confere se a chave tem o formato certo, **sem chamar a API**. Se o provedor principal recusar por permissão (erro 403, por exemplo no Vertex), a ferramenta pode **cair automaticamente** para o provedor secundário, com as credenciais do servidor.

## Permissões

Ver o histórico e as configurações não exige o grupo SRE. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Não altera nada no cluster.** Mas, ao usar um provedor **externo** (Gemini, OpenAI, Claude, Copilot), **dados do seu ambiente saem do servidor** e a análise **consome cota ou gera custo** no provedor. Com **Ollama**, nada sai.

## Cuidados e erros comuns

- **O que vai para a IA externa.** A ferramenta **mascara dados sensíveis** antes de enviar: IPs, JWT, Bearer tokens, UUIDs, textos longos em base64, senhas e chaves de API. **E-mails não são mascarados.** O mascaramento é por padrão de texto: não é garantia, então **não use um provedor externo com dados que não possam sair da empresa**, e prefira o Ollama.
- **Uma análise é uma sugestão.** O modelo pode errar ou inventar; confirme no recurso antes de agir.
- **As credenciais ficam no banco local do servidor** (`~/.k8s-hpa-manager/ai_diagnostics.db`), o **mesmo** que guarda os tokens do Dynatrace e o refresh token do Google. **Elas não são criptografadas** e o arquivo é criado com a permissão padrão do sistema (em instalações comuns, legível por outros usuários da máquina). Confira as permissões e prefira credenciais de escopo mínimo.
- **O histórico de análises expira sozinho**, então não é um arquivo permanente: exporte o relatório se precisar guardar.
- **Ollama precisa estar instalado e com o modelo baixado** no servidor; sem isso, a análise falha.
- **O status só carrega ao abrir a aba** (sem atualização automática, para não gastar cota): use o botão de atualizar.

## Veja também

[Pods](ajuda:pods), [Deployments](ajuda:deployments), [HPAs](ajuda:hpas), [Nodes](ajuda:nodes).
