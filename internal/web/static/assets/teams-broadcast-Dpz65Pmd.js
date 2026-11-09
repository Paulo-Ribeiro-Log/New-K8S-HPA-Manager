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
5. **Modelos:** salve o texto como um arquivo (**Salvar como novo arquivo**), abra um salvo (**Abrir arquivo salvo**) ou comece do zero (**Novo arquivo**). Ao salvar, marque **Salvar também os destinatários selecionados** para gravar a lista no mesmo arquivo; ao abrir, os destinatários voltam selecionados (a seleção atual é substituída). O arquivo guarda a lista num comentário no topo (invisível em Markdown) e, se ele foi salvo com destinatários, o **Ctrl+S** também os regrava.
6. **Editar o que já foi enviado:** depois do envio, ajuste o texto no editor e clique em **Editar N com o texto atual**. O Teams substitui o conteúdo das mensagens enviadas com sucesso (só as da conta do Teams do servidor) e as marca como "editadas". A **primeira** é editada sozinha para validar; se o Teams recusar, as demais **não são tentadas** e o motivo aparece no aviso. Mensagens apagadas não podem ser editadas.
6. **Enviar.** O progresso aparece em tempo real, chat por chat.
7. **Apagar mensagens:** depois do envio, dá para **apagar as mensagens enviadas** diretamente no Teams (mensagem por engano, formatação errada). Só dá para apagar as que o envio registrou com o identificador. Na lista de resultados, cada contato enviado tem uma **caixa de seleção**: desmarque quem deve ficar e **apague (ou edite) só os marcados**.
8. **Envios anteriores:** o botão **Envios** (barra do editor) lista os envios guardados no servidor, com data, autor e um resumo da primeira linha. Abra um envio, marque os contatos e **apague ou edite** (com o texto atual do editor) a qualquer momento, mesmo depois de outros envios ou de reiniciar o servidor. O **X** esquece o envio na lista (as mensagens continuam no Teams).
9. **Cancelar:** enquanto enviar, editar ou apagar estiver em andamento, aparece o botão **Cancelar**. O **lote em execução termina** (as mensagens são enviadas de 10 em 10) e o **restante não é executado**: os contatos não alcançados ficam marcados como **cancelado** (⊘). O que já foi enviado continua no Teams, fica no histórico de envios e pode ser apagado ou editado depois. Se o Teams estiver esperando o seu login, cancelar interrompe essa espera. O cancelamento não interrompe o carregamento inicial do Teams (a abertura do Chrome) nem uma requisição já em curso.

## Permissões

Exige o **login do Teams** no servidor (a sessão do Chrome). **Escanear chats, buscar uma mensagem por link, enviar, apagar mensagens, salvar e excluir modelos** exigem perfil SRE; **listar chats e ler modelos** não. Veja [Permissões](ajuda:geral-abas-de-recurso#permissoes).

## Impacto

**Ação externa e irreversível para quem recebeu.** A mensagem **sai com a conta do Teams do servidor**, não com a sua, e chega a pessoas reais. Ficam no Change History o **envio** (\`teams_broadcast_send\`), a **edição** (\`teams_broadcast_edit\`) e a **exclusão** (\`teams_broadcast_delete\`), só com **quantos chats** e quantos deram certo. **O texto da mensagem e os identificadores dos chats não são gravados**, porque são dados pessoais de conversas.

## Cuidados e erros comuns

- **Confira a lista de destinos antes de enviar.** Um chat marcado por engano recebe a mensagem. Apagar depois **não desfaz** o que já foi lido.
- **Releia a mensagem na Visualização.** Uma formatação errada, um nome de pessoa ou um link trocado é fácil de passar batido.
- **A mensagem sai em nome da conta do servidor.** Deixe claro no texto quem está avisando.
- **O texto da mensagem não fica no histórico**: se precisar de registro do que foi enviado, **salve um modelo** ou guarde por outro meio.
- **"Escanear chats" abre um navegador no servidor.** Se a sessão do Teams expirou, ele pede **login** (inclusive MFA): o processo pode parar até você concluir.
- **Imagens são embutidas na própria mensagem** (não há upload de mídia): imagens muito grandes deixam a mensagem pesada.
- **Apagar só alcança o que foi enviado por esta ferramenta e registrado**; mensagens antigas ou de outra origem não aparecem.
- **O histórico de envios fica no servidor** (\`~/.k8s-hpa-manager/teams_sent.db\`, permissão \`0600\`, visível a todos os usuários da ferramenta): data, autor, o **resumo da primeira linha** do texto, o **nome dos contatos**, o identificador de chat e o **identificador de cada mensagem**. O texto completo **não** é guardado. Envios com mais de 1 ano saem sozinhos; para tirar um antes, use o **X** na lista.
- **Os modelos ficam em arquivos no servidor** (\`~/.k8s-hpa-manager/broadcast-templates\`), visíveis a todos os usuários da ferramenta.

## Veja também

[GitHub Releases](ajuda:github-releases), [Certificados](ajuda:certificates).
`;export{a as default};
