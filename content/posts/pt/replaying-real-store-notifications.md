---
title: "As Melhores Fixtures de Teste Já Estavam em Produção"
date: "2026-07-25"
description: "O monolito vinha armazenando toda notificação de loja crua por anos — 720.183 delas. Isso transformou uma reescrita de um exercício de fixtures num exercício de replay. Sobre reconhecer um corpus acidental, semear um banco efêmero para o harness nunca tocar dados vivos, e por que guardar o payload cru é a coisa mais barata que você já vai fazer."
tags:
  [
    "testes",
    "migracao",
    "arquitetura",
    "backend",
    "go",
  ]
---

Estávamos prestes a portar o tratamento de notificações — o código que recebe um webhook da App Store ou Google Play, descobre o que mudou, e atualiza uma compra. Lógica herdada, anos de casos especiais acumulados, nenhuma especificação além da implementação.

A abordagem usual é fixtures: escrever um payload JSON por tipo de notificação, afirmar que o código novo faz a coisa certa. É trabalho honesto e testa seu entendimento do formato, que não é a mesma coisa que testar contra o formato.

Então alguém notou que o sistema antigo vinha armazenando todo payload cru de notificação que já recebeu, junto do resultado do processamento. Não decodificado, não normalizado — o corpo cru.

```
720.183 notificações
404.474 Google Play
315.709 App Store
```

Isso não é mais uma tabela. É um corpus de teste, acumulado de graça ao longo de anos, contendo exatamente os payloads que lojas reais de fato enviaram — incluindo os malformados, os campos deprecados, e os formatos que só apareceram durante alguma indisponibilidade em 2023.

## Um corpus acidental é melhor que um projetado

Fixtures escritas à mão codificam o que você *acredita* que o formato seja. Essa crença vem da documentação, que descreve o formato que o fornecedor *pretende* enviar.

O corpus contém o que eles *enviaram*. Isso difere nas formas que importam: campos documentados como obrigatórios que às vezes estão ausentes, valores de enum que não estão na documentação, um formato de payload que mudou sem mudar de versão. Cada um deles é um caso que quem escreve fixture não pensa em escrever, porque você só consegue escrever uma fixture para um caso que você sabe que existe.

Existe um argumento distribucional também. Fixtures te dão aproximadamente um exemplo por tipo, o que achata a frequência de tudo. O corpus é enviesado — centenas de milhares de renovações, um punhado de revogações — e o viés é informação real. Ele te diz quais caminhos importam para performance e quais são raros o suficiente para um bug subtil ficar ali por um ano.

## O harness não pode tocar nada real

Duas decisões no design existem puramente para tornar o corpus seguro de usar.

```
D-2 — O harness de replay nunca toca o ambiente stable
D-3 — O serviço escreve no stable, o harness não
```

O replay semeia um **banco efêmero** e roda contra ele. Não uma conexão somente-leitura ao compartilhado — um banco separado, criado para a execução, descartado depois.

A distinção importa porque um replay não é uma leitura. Ele *precisa* escrever: processar uma notificação produz uma linha de compra, e comparar resultados significa deixar as duas implementações produzirem suas linhas. Então "somente-leitura" não está disponível como mecanismo de segurança aqui, e a alternativa seria escrever no banco real dentro de uma transação que você promete reverter. Isso está a um `defer` de distância do desastre.

O critério relacionado:

> **AC-049** — A comparação não pode mutar as tabelas compartilhadas.

*Não pode*, não *não muta*. O harness não tem credencial que alcance o banco compartilhado. Estrutural, em vez de uma regra que alguém segue — o que importa porque essa é uma ferramenta que você vai rodar repetidamente caçando discordâncias, muitas vezes tarde, muitas vezes distraído.

O `D-3` é o complemento, e vale declarar separadamente: o *serviço* de fato escreve no ambiente real. Não é que escritas sejam proibidas nesta fase, é que o harness especificamente não tem nada que ver com elas. Dois componentes, dois níveis diferentes de autoridade, escritos para que ninguém os unifique por conveniência.

## Diagnosticável, porque você vai reexecutar muito

> **AC-047** — Toda discordância é diagnosticável sem reexecutar.

O mesmo critério da comparação de entitlement, e ele merece seu lugar duas vezes por uma razão específica: um corpus de 720.183 payloads leva tempo real para processar. Um harness que reporta "1.204 discordâncias" e nada mais te força num ciclo de adicionar-logging, reexecutar, esperar, olhar — onde cada iteração custa minutos e você vai precisar de várias.

Então a saída carrega o payload, os dois resultados, e o branch que cada implementação tomou. O que tem um segundo benefício: um relatório nessa granularidade é agrupável. 1.204 discordâncias acabam sendo quatro causas distintas, e você corrige quatro coisas em vez de triar 1.204 linhas.

## Guarde o payload cru

A lição geral não é sobre harnesses de replay. É que o monolito fez uma coisa barata anos atrás — guardou o corpo cru ao lado do resultado parseado — e essa decisão pagou pela estratégia inteira de verificação do seu próprio substituto.

Guardar o payload cru é quase gratuito. É uma coluna de texto, comprime bem, e é escrita-uma-vez. O que compra:

- **Replay.** Você pode reexecutar código novo contra história real. Esse é o que usamos.
- **Depuração retroativa.** Quando uma compra está num estado que ninguém explica, você pode ler o que a loja de fato disse em vez de inferir do que seu parser guardou.
- **Recuperação de bug de parser.** Se você descobre que seu decodificador descartou um campo por seis meses, você pode reprocessar. Sem o corpo cru, esses dados se foram.

Eu agora trataria isso como padrão para qualquer webhook externo: **guarde o corpo cru, guarde por muito tempo, e não normalize na entrada.** O custo é armazenamento, que é barato. O benefício é opcionalidade que você não consegue fabricar depois, porque os payloads que você não guardou se foram permanentemente e nenhuma quantidade de engenharia os traz de volta.

## O que eu levaria disso

**Procure um corpus antes de escrever fixtures.** Logs, uma tabela de auditoria, uma coluna de payload cru, um arquivo no S3 de requisições. Se você já tem entradas reais, fixtures são um complemento em vez da estratégia.

**Entradas reais contêm os casos que você não sabe escrever.** Esse é o valor inteiro, e não é obtenível sendo minucioso.

**Dê ao harness seu próprio banco efêmero.** Um replay precisa escrever, então somente-leitura não está disponível; separação está. Torne-o *incapaz* de alcançar produção em vez de *improvável* de alcançar.

**Guarde payloads crus na entrada.** É a opcionalidade mais barata em software, e a versão de você fazendo a reescrita em três anos vai achar isso mais valioso que qualquer coisa que você projetou de propósito.
