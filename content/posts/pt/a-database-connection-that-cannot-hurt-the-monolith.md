---
title: "Uma Conexão de Banco Que Não Pode Machucar o Monolito"
date: "2026-07-20"
description: "O novo serviço lê o banco do serviço antigo durante a fase de shadow, o que o torna um risco a menos que a conexão seja restrita por construção. Sobre critérios de aceite formulados como capacidades que um serviço precisa não ter, por que readiness e liveness não devem concordar sobre o banco, e configuração que falha alto no startup."
tags:
  [
    "go",
    "bancos-de-dados",
    "postgres",
    "confiabilidade",
    "backend",
    "extracao-de-assinaturas",
  ]
---

A primeira fase de extrair um serviço não produziu features. Produziu um processo que inicia, responde a um health check, loga num formato que a plataforma entende, e conecta ao banco do monolito sem ser capaz de machucá-lo.

Essa última cláusula é a fase inteira. Durante o período de shadow o novo serviço lê os dados de produção do sistema antigo. Se ele se comportar mal — uma conexão vazada, um laço de queries descontrolado, um deploy ruim — o raio de destruição é o sistema que atende todos os usuários hoje. Um serviço novo que pode degradar o antigo é pior que nenhum serviço novo.

O interessante nos critérios que escrevemos é quantos deles são formulados como coisas que o serviço precisa **não** ser capaz de fazer.

## Limitado por configuração, não por intenção

> **AC-028** — O pool é limitado por configuração.

Não "o serviço é cuidadoso com conexões". Um teto, em configuração, com um número. A distinção importa porque o modo de falha não é um desenvolvedor decidindo abrir mil conexões — é um laço de retry, ou um handler que esquece de liberar, ou tráfego dez vezes maior do que alguém modelou. Cuidado não sobrevive a isso. Um teto rígido sobrevive.

Junto dele, o grant é somente-leitura. Entre os dois, o pior que um bug no novo serviço pode fazer é *falhar*: ele esgota seu próprio pool limitado e começa a retornar erros para suas próprias requisições, enquanto o orçamento de conexões do monolito fica intocado e seus dados não podem ser escritos.

Passei a pensar nisso como o formato útil para um critério numa fronteira de confiança: **descreva a capacidade que o componente precisa não ter.** "Limitado e somente-leitura" é verificável. "Não interfere com o monolito" é uma esperança.

Uma fase posterior precisou escrever, e o notável é como chegou lá — um role *separado* com capacidade de escrita, adicionado como tarefa própria, em vez de ampliar o somente-leitura. Ampliar um grant é uma mudança de uma linha que silenciosamente apaga a garantia. Adicionar um segundo role mantém o caminho de leitura comprovadamente somente-leitura e faz do caminho de escrita algo que você pode apontar.

## Readiness e liveness precisam discordar

> **AC-027** — Readiness reflete o banco; liveness não.

Este é o critério que eu mais gostaria de ver copiado, porque errá-lo é tão comum e a consequência é tão ruim.

Se liveness depende do banco, então uma instabilidade do banco mata seus contêineres. O orquestrador vê um probe de liveness falhando, reinicia o processo, o novo processo também não alcança o banco, e você tem um crash loop empilhado sobre uma indisponibilidade — além de ter descartado toda requisição em andamento e qualquer aquecimento de pool, exatamente no momento em que o sistema está sob estresse.

A divisão é: **liveness** responde *este processo está funcionando?* — deveria falhar apenas quando um restart ajudaria. **Readiness** responde *esta instância deveria receber tráfego agora?* — deveria falhar quando uma dependência está indisponível, para o load balancer parar de mandar trabalho, sem nada ser morto.

Mesma verificação subjacente, dois consumidores diferentes, comportamentos corretos opostos.

## Falhe alto no startup

> **AC-023** — Configuração incorreta falha no startup, alto.

O serviço se recusa a iniciar com configuração ruim, e o erro nomeia a variável ofensora.

As duas metades merecem seu lugar. Falhar no startup em vez de no primeiro uso significa que um deploy mal configurado morre no rollout, onde a plataforma nota e faz rollback, em vez de às 3h da manhã na primeira requisição que toca a coisa mal configurada. E nomear a variável é a diferença entre uma correção de cinco segundos e um bisect num arquivo de configuração, especialmente para quem for acionado e não escreveu o deploy.

A forma geral: valide toda a configuração de uma vez, no boot, e diga exatamente o que falta. Um `Validate()` retornando "database URL is required" vale mais que qualquer quantidade de documentação sobre quais variáveis definir.

## Logs estruturados e um id de correlação que volta

> **AC-024** — Toda linha de log é JSON estruturado com os campos da plataforma.
> **AC-025** — Um id de correlação existe em toda requisição e volta para quem chamou.

A segunda metade do AC-025 é a parte que as pessoas deixam de fora. Um id que você gera internamente permite que *você* rastreie uma requisição. Um id devolvido a quem chamou permite que o ticket de suporte de um usuário se torne uma consulta — alguém cola um identificador, e você acha a requisição exata em vez de adivinhar por timestamp e rota.

Existe uma decisão de nomenclatura no design que vale mencionar: isso deliberadamente não se chama `request_id`. Uma única ação de usuário pode cruzar várias requisições entre serviços, e chamá-lo de id de requisição encoraja o código a gerar um novo por salto — que é precisamente como um id de correlação para de correlacionar. Nomeá-lo pelo que faz em vez de por onde apareceu primeiro torna a regra de propagação óbvia.

E os nomes dos campos vêm da convenção existente da plataforma em vez de serem inventados, porque logs só são consultáveis se todo serviço concorda como os campos se chamam. Herdar convenções de um serviço irmão é sem glamour e se paga na primeira vez que você precisa de uma consulta cobrindo os dois.

## O que eu levaria disso

**Formule critérios de fronteira de confiança como capacidades que o componente precisa não ter.** Limitado, somente-leitura, não pode esgotar, não pode escrever. Isso é testável; "cuidadoso" não é.

**Nunca deixe liveness depender de uma dependência.** Uma instabilidade do banco se torna um crash loop, e você perde trabalho em andamento no pior momento. Readiness é o probe que deveria se importar.

**Adicione um segundo grant em vez de ampliar o primeiro.** Ampliar é uma linha e remove silenciosamente a garantia em torno da qual você construiu a fase.

**Valide toda a configuração no boot e nomeie o que está errado.** A alternativa é descobrir na requisição que por acaso precisa dela.

Nada disso entregou uma feature. É a fase que tornou toda fase posterior segura de tentar, e os critérios valem mais para mim do que o código que produziram — o código são algumas centenas de linhas e seria escrito diferente em outra linguagem, mas "readiness reflete o banco, liveness não" transfere para todo lugar.

---

*Parte de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), dezessete posts sobre extrair a metade de assinaturas de um monolito Django para um serviço em Go.*
