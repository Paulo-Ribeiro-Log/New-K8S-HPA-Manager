## Para que serve

Acompanha o comportamento de **HPAs** ao longo do tempo: uso de CPU e memória, número de réplicas, métricas de pico e tendência, e alertas de anomalia. Os dados vêm do **Prometheus** do cluster.

## Quando usar

- Ver se um HPA está escalando como esperado (réplicas contra carga).
- Comparar o uso real de CPU e memória com os requests e limits.
- Investigar um pico ou um período de instabilidade.
- Decidir se vale ajustar o HPA e abrir o editor direto dali.

## Como usar (passo a passo)

1. Na aba [HPAs](ajuda:hpas), use o botão de **monitoramento** do HPA. Ele entra na lista desta aba.
2. Abra **Monitoramento**. A barra lateral mostra os **HPAs monitorados**, agrupados por cluster; clique no nome do cluster para recolher ou expandir. Use o botão do topo para esconder a barra: com ela fechada, um seletor lista os HPAs.
3. **Clique em um HPA** para ver a **Análise de Métricas**:
   - escolha o **período** (de 5 minutos a 24 horas) e o **auto-refresh** (desligado, ou a cada 1, 5, 10 ou 15 minutos);
   - veja os gráficos de **Uso de CPU**, **Uso de Memória** e **Réplicas** ao longo do tempo, com os requests e limits do Deployment indicados ao lado;
   - cada gráfico pode ser **expandido** e tem zoom;
   - o botão de **Alertas** mostra quantos alertas existem no período, quantos são críticos, ou "Tudo OK".
4. **Editar configurações deste HPA** leva direto ao editor na aba HPAs.
5. A lixeira de cada HPA **remove do monitoramento** (não apaga o HPA do cluster).

## O indicador "Ativo / Parado"

O cabeçalho da barra lateral mostra se o **motor de monitoramento** está ativo. Clique com o botão direito no indicador para **Reiniciar Engine** ou **Copiar Info para Clipboard**.

## Permissões

É uma aba de **consulta**. Ela precisa que o cluster tenha um Prometheus que o servidor consiga acessar.

## Impacto

**Somente leitura.** Não altera o cluster. A lista de HPAs monitorados fica **no seu navegador**, não no servidor: outro usuário, ou outro navegador, não vê a sua lista.

## Cuidados e erros comuns

- **Gráficos vazios** quase sempre querem dizer que o Prometheus não respondeu (rede, VPN) ou que o HPA é novo e ainda não tem histórico.
- **A lista é local:** limpar os dados do navegador esvazia os HPAs monitorados.
- O número de réplicas e o uso vêm de **amostras do Prometheus**; um pico muito curto pode não aparecer no período escolhido.
- Para investigar a causa de um pico, cruze com [Events](ajuda:events) e [Pods](ajuda:pods).

## Veja também

[HPAs](ajuda:hpas), [Staging](ajuda:staging), [Events](ajuda:events), [Pods](ajuda:pods).
