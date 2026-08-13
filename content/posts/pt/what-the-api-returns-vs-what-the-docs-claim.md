---
title: "O Que a API Retorna vs O Que a Documentação Afirma"
date: "2026-07-27"
description: "Avaliando se um provedor de dados esportivos poderia substituir outro. A matriz de capacidades publicada e a API de trial discordavam, então a análise foi reconstruída sobre sondagens contra o endpoint real. Sobre derivar seu consumo do código em vez da memória, citar evidência por veredito, e rotular um apêndice explicitamente como não comprovado."
tags:
  [
    "arquitetura",
    "api",
    "avaliacao-de-fornecedor",
    "tomada-de-decisao",
    "backend",
    "migracao-de-provedor",
  ]
---

Nós ingerimos dados esportivos — partidas, competições, times, eventos ao vivo — de um provedor comercial. A pergunta na mesa era se um provedor diferente poderia fornecer a mesma coisa, por um custo menor.

A forma usual de responder isso é uma planilha construída a partir da documentação de dois fornecedores, preenchida ao longo de uma semana, apresentada como comparação. Eu já construí essa planilha. Ela está confiantemente errada de um jeito específico: compara o que dois fornecedores *dizem* contra o que você *acha* que usa.

As duas metades disso são pouco confiáveis, então a análise foi estruturada para substituir cada uma por evidência.

## O que consumimos, derivado do código

A primeira história de usuário não era sobre o novo provedor:

> **US-001** — Saber exatamente o que tiramos do Opta.

Não "o que usamos" como pergunta de entrevista. Um inventário derivado do código: os endpoints de feed nomeados na camada de acesso a dados, as constantes de feed WebSocket, os valores de enum no contrato gRPC, os nomes dos jobs do scheduler. Enumerado mecanicamente, para que a lista seja a verdade em vez de uma lembrança.

Isso importa mais do que parece, e do mesmo jeito que o inventário de superfícies do monolito importou. As pessoas conhecem os feeds que elas pessoalmente tocaram. O conjunto completo inclui um feed adicionado para uma competição três anos atrás, e um que é consultado por um job agendado que ninguém olhou desde que foi escrito. Os dois contam quando você está precificando uma migração, e nenhum dos dois vai aparecer numa conversa.

Um efeito colateral útil: o inventário é a coisa que você leva ao novo fornecedor. "Vocês conseguem fornecer estas quatorze capacidades específicas" é uma pergunta muito melhor que "vocês conseguem substituir o Opta", e produz uma resposta muito melhor.

## Vereditos com citações

A segunda história é a comparação, e o critério dela é onde a disciplina vive:

> **US-002** — Saber se a Sportradar pode fornecer cada capacidade.

Cada capacidade recebe um veredito, e cada veredito cita sua fonte. Não um selo verde — uma referência à seção da documentação, para que quem lê possa verificar a alegação sem refazer a pesquisa.

Isso soa como burocracia até os vereditos discordarem da realidade, que é exatamente o que aconteceu.

## A documentação e a API de trial discordavam

Um spike posterior existe porque a primeira análise não bastou:

> **US-006** — Saber o que a API de fato retorna, não o que a documentação afirma.

A matriz de capacidades publicada dizia que certos dados estavam disponíveis num certo tier. Sondar o endpoint de trial retornou outra coisa — campos ausentes, cobertura mais estreita que a descrita, algumas capacidades presentes apenas para um subconjunto de competições.

Não acho que isso seja desonestidade do fornecedor. Documentação descreve o produto em sua configuração mais completa; o que uma conta específica alcança depende de tier, região, licenciamento, e de quais competições o fornecedor tem direitos nesta temporada. O documento não está mentindo, só está respondendo uma pergunta mais geral que a sua.

Então a análise foi reconstruída sobre sondagens: chamar o endpoint real para as capacidades que de fato consumimos, através das competições que de fato atendemos, e versionar as respostas cruas como evidência. A unidade de verdade deixou de ser "a documentação diz" e passou a ser "nós perguntamos, nesta data, e recebemos isto".

A lição que eu generalizaria: **para uma avaliação de fornecedor, documentação é uma hipótese e uma chave de trial é o experimento.** Se o fornecedor não te dá uma chave de trial, isso em si é informação sobre como a relação vai ser.

## Rotule o que você não conseguiu provar

O detalhe ao qual eu sempre volto é uma tarefa intitulada *apêndice do TheSports, explicitamente não comprovado.*

Um segundo provedor apareceu durante o trabalho, e não tínhamos acesso para testá-lo. Duas opções tentadoras: deixar de fora, ou incluir apenas com base em documentação ao lado do provedor empiricamente verificado.

As duas são ruins. Omitir perde informação — alguém vai perguntar, e a análise vai parecer incompleta. Incluir silenciosamente é pior, porque quem lê não consegue distinguir que uma coluna da comparação é evidência e outra é material de marketing, e vai dar o mesmo peso às duas.

Então está incluído e **rotulado como não comprovado**, em um apêndice próprio, separado da análise verificada. O leitor recebe a informação e sua procedência.

Esse é o mesmo instinto de declarar lacunas de cobertura num harness de teste, e aparece em todo lugar quando você percebe: **o nível de confiança de uma alegação é parte da alegação.** Um documento que apresenta achados verificados e não verificados com o mesmo peso visual destruiu informação que seu autor tinha.

## O que eu levaria disso

**Derive seu consumo do código, não da memória.** O conjunto de coisas que você tira de um fornecedor inclui feeds de que ninguém lembra, e esses custam o mesmo para substituir que os que você usa diariamente.

**Leve o inventário ao fornecedor.** Quatorze capacidades nomeadas recebem uma resposta real; "vocês conseguem substituir X" recebe uma resposta comercial.

**Trate documentação como hipótese e sonde a API.** A documentação descreve o produto; sua conta alcança um subconjunto dele. A diferença é o risco inteiro da migração.

**Versione as respostas cruas das sondagens.** Evidência datada, revisável, reexecutável. Também permite fazer ao fornecedor uma pergunta precisa quando a resposta difere da documentação.

**Rotule seções não comprovadas como não comprovadas.** Incluir algo que você não conseguiu verificar é aceitável. Incluir com a mesma confiança de todo o resto é uma forma de enganar quem lê usando afirmações verdadeiras.

---

*Parte de uma linha de dois posts sobre substituir um provedor de dados esportivos — ao lado de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), o projeto que corria em paralelo.*
