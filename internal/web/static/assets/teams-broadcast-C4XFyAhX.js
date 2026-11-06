const a=`## Para que serve

Envia **a mesma mensagem para vários chats do Microsoft Teams** de uma vez (por exemplo, um comunicado de mudança para os grupos das equipes), com editor **Markdown**, **pré-visualização**, **modelos salvos** e a opção de **apagar** depois as mensagens enviadas.

## Quando usar

- Avisar **várias equipes** de uma janela de manutenção ou de um incidente.
- Reenviar uma mensagem que já existe no Teams (por link), com a imagem dela.
- Corrigir um envio errado **apagando** as mensagens daquele lote.

## Como usar (passo a passo)

1. **Escaneie os chats** com **Escanear chats**: a ferramenta abre o **Chrome com a sessão do Teams** e lê todos os chats (leva cerca de 30 segundos). O Teams é acessado com um perfil de navegador **separado** do ServiceNow.
2. **Escolha os destinos.** A lista traz os chats; use **Buscar chat por nome** e marque os que vão receber a mensagem.
3. **Escreva a mensagem** no editor. Alterne entre **Markdown** e **Texto simples** (no texto simples, nada é interpretado como formatação). A **Visualização** mostra como vai ficar.
4. Para **reaproveitar** uma mensagem, **cole o link de uma mensagem do Teams** (\`teams.microsoft.com/l/message/...\`): a ferramenta carrega o conteúdo, **inclusive as imagens**, e você edita.
5. **Modelos:** salve o texto como um arquivo (**Salvar como novo arquivo**), abra um salvo (**Abrir arquivo salvo**) ou comece do zero (**Novo arquivo**).
6. **Enviar.** O progresso aparece em tempo real, chat por chat.
7. **Apagar mensagens:** depois do envio, dá para **apagar as mensagens enviadas** diretamente no Teams (mensagem por engano, formatação errada). Só dá para apagar as que o envio registrou com o identificador.

## Permissões

Exige o **login do Teams** no servidor (a sessão do Chrome). **Escanear chats, buscar uma mensagem por link, enviar, apagar mensagens, salvar e excluir modelos** exigem perfil SRE; **listar chats e ler modelos** não. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Ação externa e irreversível para quem recebeu.** A mensagem **sai com a conta do Teams do servidor**, não com a sua, e chega a pessoas reais. Ficam no Change History o **envio** (\`teams_broadcast_send\`) e a **exclusão** (\`teams_broadcast_delete\`), só com **quantos chats** e quantos deram certo. **O texto da mensagem e os identificadores dos chats não são gravados**, porque são dados pessoais de conversas.

## Cuidados e erros comuns

- **Confira a lista de destinos antes de enviar.** Um chat marcado por engano recebe a mensagem. Apagar depois **não desfaz** o que já foi lido.
- **Releia a mensagem na Visualização.** Uma formatação errada, um nome de pessoa ou um link trocado é fácil de passar batido.
- **A mensagem sai em nome da conta do servidor.** Deixe claro no texto quem está avisando.
- **O texto da mensagem não fica no histórico**: se precisar de registro do que foi enviado, **salve um modelo** ou guarde por outro meio.
- **"Escanear chats" abre um navegador no servidor.** Se a sessão do Teams expirou, ele pede **login** (inclusive MFA): o processo pode parar até você concluir.
- **Imagens são embutidas na própria mensagem** (não há upload de mídia): imagens muito grandes deixam a mensagem pesada.
- **Apagar só alcança o que foi enviado por esta ferramenta e registrado**; mensagens antigas ou de outra origem não aparecem.
- **Os modelos ficam em arquivos no servidor** (\`~/.k8s-hpa-manager/broadcast-templates\`), visíveis a todos os usuários da ferramenta.

## Veja também

[GitHub Releases](ajuda:github-releases), [Certificados](ajuda:certificates).
`;export{a as default};
