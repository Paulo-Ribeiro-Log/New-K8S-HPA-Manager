const e=`## Para que serve

**Compara dois arquivos \`values.yaml\`** guardados no **Nexus Repository Manager**: por exemplo, a mesma aplicação em duas versões, ou em dois ambientes (hlg e prd). Mostra um diff lado a lado.

## Quando usar

- Ver **o que mudou nos values** entre duas versões de um chart antes de promover para produção.
- Conferir por que um ambiente se comporta diferente do outro (diferença de configuração).
- Montar um resumo das diferenças para anexar a uma mudança.

## Como usar (passo a passo)

1. **Configure o Nexus uma vez**, no menu de Perfil, em **Nexus**: o endereço do Nexus (\`BaseURL\`), o **repositório**, um diretório temporário opcional (\`TempDir\`) e o **padrão de URL** dos arquivos. O padrão é \`{baseUrl}/repository/{repository}/{release}/{version}/{environment}/values/{type}-values.yaml\`. Sem isso, a aba mostra "Nexus não configurado" e o botão **Configurar Nexus**.
2. A aba verifica a conexão ao abrir.
3. Para **cada um dos dois arquivos** (**Arquivo 1** e **Arquivo 2**), escolha **Release**, **Versão** e **Arquivo**. A busca de release aceita digitar **2 ou mais caracteres**; as versões e os arquivos vêm da própria busca, sem outra chamada.
4. **Preview das URLs** mostra os endereços que serão baixados, para conferir antes.
5. **Comparar** baixa os dois arquivos e mostra o **diff**. Se um dos downloads falhar, o erro aponta **qual arquivo** (1 ou 2) e a URL tentada.
6. Dá para montar um **resumo em Markdown** da comparação (release, versão e arquivo de cada lado) para colar em outro lugar.

## Autenticação

O usuário e a senha **não são pedidos aqui**. A ferramenta usa o seu **Perfil SSO** corporativo (e-mail ou matrícula, conforme o perfil), a mesma identidade do ServiceNow, Teams e Spinnaker, lida na hora. Se der erro \`401\`, o problema costuma estar no Perfil SSO (senha desatualizada), não em uma configuração do Nexus.

## Permissões

É uma aba de **consulta**. A configuração do Nexus e o Perfil SSO são **seus** (por usuário).

## Impacto

**Somente leitura no Nexus.** Se o **diretório temporário** estiver configurado, os arquivos baixados são **gravados lá**, no disco do servidor, em subpastas \`release/versão/ambiente\`.

## Cuidados e erros comuns

- **Values costumam ter dados sensíveis** (hosts internos, às vezes credenciais). Se você configurar o diretório temporário, os arquivos ficam em disco, em arquivos de permissão padrão (\`0644\`, legíveis por outros usuários da máquina). **Deixe o \`TempDir\` vazio**, a menos que precise dos arquivos, e apague-os depois.
- **Um caminho que não bate com o padrão de URL** dá "não encontrado": o padrão precisa refletir a estrutura real do seu repositório.
- **O diff é de texto**: uma mudança só de ordem ou de espaçamento aparece como diferença.
- **A comparação não valida o YAML nem o chart**: ela mostra o que mudou, não se está correto.
- **Sem acesso ao Nexus (VPN fora)**, a verificação de conexão falha: a aba avisa.

## Veja também

[Helm](ajuda:helm), [Deployments](ajuda:deployments).
`;export{e as default};
