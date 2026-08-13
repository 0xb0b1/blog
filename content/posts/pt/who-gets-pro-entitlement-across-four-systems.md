---
title: "Quem Recebe PRO: Entitlement Através de Quatro Sistemas"
date: "2026-08-08"
description: "Conceder um tier pago parece definir um booleano. É um predicado sobre estados de assinatura com os quais ninguém concorda, aplicado a quatro sistemas sem transação entre eles. Sobre ordenar escritas pelo que cada falha parcial deixa para trás, recusar compensação, uma decisão de produto que concedeu acesso a precisamente ninguém, e um enum a que se fazem duas perguntas que parecem uma."
tags:
  [
    "sistemas-distribuidos",
    "modelagem-de-dominio",
    "confiabilidade",
    "identidade",
    "backend",
    "extracao-de-assinaturas",
  ]
---

O sistema é um app de esportes com um tier pago. Alguém assina pela App Store ou Google Play, e isso deveria liberar as features pagas. Estamos [extraindo essa maquinaria de um monolito Django para um serviço em Go](/pt/posts/quantify-the-failure-before-you-redesign-it), contra o mesmo Postgres compartilhado — então por toda a transição, as duas implementações estão vivas.

Antes dos problemas interessantes, o vocabulário, porque "liberar as features pagas" esconde duas perguntas separadas.

Uma **compra** é uma linha: qual loja, qual produto, qual usuário, em que estado a loja disse por último que ela estava, até quando está paga. Compras chegam por dois caminhos. O app chama um endpoint de subscribe quando alguém compra — esse é o caminho primário de escrita, a origem. E a loja envia **notificações** depois: renovada, cancelada, reembolsada, entrou em billing retry, expirou. Essas são o eco assíncrono, chegando por anos após a venda, independentemente de alguém abrir o app.

**Entitlement** é a resposta derivada: dadas as compras que este usuário tem, ele recebe PRO agora? Isso é um predicado, e enfaticamente não é uma coluna. O que nos leva à primeira coisa que me surpreendeu.

## Entitlement vive em quatro sistemas

Eu tinha assumido que aplicar entitlement significava escrever uma coluna de role na linha do usuário. Ler o `update_user` do monolito mostrou que ele toca quatro sistemas:

- **O provedor de identidade** (FusionAuth) — as roles do registration. É isso que de fato gateia as features pagas. Pule e nada mudou para o usuário, não importa o que mais você escreveu.
- **Redis** — os lookups de auth cacheados. Pule e o monolito continua servindo a role antiga do cache.
- **Postgres** — a role do usuário e um ponteiro para sua compra atual. Pule e o banco discorda da identidade.
- **O serviço de notificações**, via gRPC — a audiência de push. Pule e um usuário rebaixado continua recebendo pushes destinados a assinantes.

Não existe transação entre esses quatro. Não pode existir — um é uma API HTTP, um é um cache, um é um serviço gRPC.

Isso importou imediatamente, porque eu tinha perguntado onde o efeito de entitlement deveria ser aplicado e oferecido *estender os grants de banco do novo serviço* como uma das opções. Essa opção foi escolhida. Era também a pergunta errada, e o erro era meu: minha opção assumia que aplicar significava escrever Postgres.

Estender os grants do Postgres teria permitido ao nosso serviço escrever o **menos consequente** dos quatro. A linha diria PRO, o provedor de identidade ainda gatearia pela role antiga, o cache continuaria servindo ela. Um usuário que pagou continuaria bloqueado enquanto o banco alegava o contrário — o pior tipo de bug, porque todo dashboard diz que funcionou.

Então a pergunta foi refeita com aquela lista anexada, e resolvida de outro jeito: o novo serviço se torna a autoridade de entitlement de fato, todas as quatro escritas. O mecanismo que vale guardar não é a decisão, é que **um registro de decisão carrega a premissa sobre a qual sua resposta foi dada.** A minha estava escrita, que é a única razão pela qual pôde ser encontrada errada e refeita. Uma resposta sem premissa registrada é indistinguível de uma resposta à pergunta certa.

## Ordenando pelo que uma falha parcial deixa para trás

Sem transação disponível, a pergunta de design não é "como tornamos isso atômico". É "o que cada falha parcial deixa para trás, e com quais restos conseguimos viver".

A ordem é Postgres, provedor de identidade, Redis, gRPC. Cada posição é argumentada em vez de convencional:

**Postgres primeiro**, porque é o único dos quatro que pode sofrer rollback. Se algo na sequência vai falhar, você quer a escrita reversível já feita.

**O provedor de identidade segundo**, porque até ele ser escrito, *nada mudou de fato* — ele guarda a role que gateia o acesso. Uma falha depois do Postgres e antes dele deixa uma linha de banco alegando um entitlement que o provedor não concede: visível, errado, e inerte. Esse é o estado intermediário menos danoso disponível.

**Redis estritamente depois do provedor**, e esse é o que é fácil inverter. O instinto é invalidar o cache primeiro para que nada obsoleto possa ser servido. Faça isso e você abre uma janela: o cache está vazio, uma requisição chega, a role antiga é lida do provedor — que ainda não foi escrito — e cacheada de novo. Você invalidou um cache direto de volta para o valor que estava tentando remover. Invalidação tem que seguir a escrita que ela invalida, e nenhuma esperteza encurta o intervalo.

**gRPC por último**, porque uma audiência de push obsoleta é um incômodo e uma role errada é um erro de entitlement. Se a sequência para em algum lugar, pare onde a falha é mais branda.

Essa última frase é todo o método: ordene uma sequência não-atômica de forma que todo prefixo dela seja um estado que você consegue defender a um usuário.

## Não compensar, de propósito

O movimento óbvio em seguida é compensação — se o passo três falha, desfaça os passos um e dois. Deliberadamente não fazemos.

Uma falha deixa as escritas anteriores de pé, reporta quais caíram, e não dá acknowledge na mensagem da fila. Todo passo é idempotente, então a reentrega re-executa a sequência inteira e termina o trabalho.

A razão é estreita e eu a defenderia: **desfazer um provedor de identidade é como um bug se torna dois.** Um caminho de compensação é código que só executa quando algo já deu errado, que é exatamente quando ele tem menos chance de ter sido exercitado. Seu modo de falha é remover acesso de alguém que deveria ter, com base numa escrita parcial que ele pode estar lendo errado. Replay idempotente tem um caminho de código — o mesmo que executou na primeira vez, o que executa constantemente e portanto é de fato testado.

O custo é declarado em vez de escondido: entre a falha e a reentrega, um usuário pode ficar num estado parcialmente aplicado. A mitigação é que a lista de escritas que *caíram* viaja com o erro, porque nada é compensado e essa lista é o único registro do que mudou. Um estado parcial cujo registro fica numa linha de log sem correlação é um estado parcial que ninguém consegue corrigir.

O primeiro design retornava imediatamente na falha e confiava na reentrega. Foi emendado para um retry no lugar, limitado, com backoff, e a reentrega como backstop — e o raciocínio corta os dois lados. A reentrega re-executa a sequência *inteira* de quatro sistemas, reescrevendo passos que já tinham sucedido — seguro apenas porque cada passo é idempotente. Essa mesma propriedade é exatamente o que torna um retry no lugar seguro. Se você tolera um, tolera o outro, e o retry no lugar é mais barato por três escritas. O limite importa tanto quanto o retry: retry ilimitado dentro de um handler de fila mantém a mensagem invisível até o visibility timeout expirar, ponto em que ela é reentregue de qualquer forma — o dobro do trabalho para o mesmo resultado, sem nada conseguir falhar rápido.

Aplicar fica atrás de um switch com default desligado, então tudo isso pôde subir, ser deployado e observado antes de mudar a conta de alguém. A primeira vez que o serviço tocou o entitlement de um usuário real foi uma decisão que alguém tomou, não um artefato de deploy.

Um risco que não consigo projetar para fora: uma vez que aplicamos, o monolito ainda pode também. Nada em nenhum dos dois códigos detecta dois writers. A separação vive inteiramente num passo de runbook — desligar o caminho antigo no repoint — e se esse passo é esquecido, os dois serviços escrevem o mesmo registration a partir de notificações diferentes e o último vence, silenciosamente. Isso está escrito no runbook como um passo em vez de deixado como expectativa, que é o máximo que consigo fazer daqui.

## A decisão que concedeu acesso a ninguém

Agora o predicado em si, que é onde estava o bug genuinamente interessante.

Uma assinatura em **billing retry** é uma cujo período pago terminou e cuja cobrança de renovação falhou. A loja continua tentando o cartão; o usuário ainda tem o app aberto. Se ele mantém acesso é uma decisão de produto, e a nossa tinha sido tomada: billing retry e grace period os dois concedem acesso.

O predicado que implementa isso é, em essência:

```go
if !grantingStates.Contains(p.Status) {
    return false
}
return p.EffectiveExpiration.After(now)
```

As duas metades são defensáveis isoladamente. O estado precisa ser um que concede. A data de validade paga não pode estar no passado.

Juntas elas concedem acesso a ninguém em billing retry — porque uma assinatura está naquele estado *precisamente porque* o período terminou e a cobrança falhou, então a data de validade está atrás de você por definição. De 7.684 compras medidas naquele estado, **zero** tinham validade futura. Não poucas. Zero, e necessariamente zero.

A decisão estava viva, documentada, referenciada em reuniões, e nunca tinha concedido nada a ninguém.

Onde o bug morava é a parte que vale sentar com. O monolito tinha um commit implementando a mudança; um pull request o deixou para trás. Nosso port era fiel ao código que de fato subiu — a metade sem o curto-circuito. Duas implementações, as duas internamente consistentes, as duas concordando entre si, as duas deixando de fazer o que a decisão dizia.

Paridade tinha sido provada sobre 243.318 compras com zero discordâncias. Essa afirmação era verdadeira e não poderia possivelmente ter pegado isso. **Um teste diferencial é cego a um defeito que os dois lados compartilham.** Toda medida de qualidade que tínhamos estava verde, e o que encontrou foi ler a decisão e o predicado ao mesmo tempo e notar que um estado no conjunto que concede não conseguia satisfazer a condição abaixo dele. Eu não tenho um método mecânico para essa classe de bug, e prefiro dizer isso a inventar um.

A correção parecia um curto-circuito de uma linha e não era, por causa do que o harness afirma. Sua afirmação é *os dois predicados concordam*. Corrigir o nosso sozinho não repara essa afirmação — quebra ela, num estado onde o harness reporta uma divergência real que é inteiramente nossa. Então a metade em Python foi como seu próprio pull request e a nossa metade como uma tarefa aqui, caindo juntas. A alternativa era uma janela onde os dois serviços discordavam sobre quem tinha direito, enquanto o alarme projetado para pegar exatamente isso reportava corretamente e era ignorado como ruído esperado. Um alarme que você decidiu ignorar por uma semana está desligado.

Um detalhe que eu repetiria: a tarefa que subiu *antes* da correção afirmava o comportamento **pré-correção**, com a dependência entre repositórios escrita no nome do teste e na mensagem de falha. Quando a correção caiu, o teste falhou e disse por que e onde. Uma expectativa que vira ruidosamente numa mudança futura conhecida é um alarme agendado; um `// TODO` é uma nota.

## Um enum, duas perguntas

Depois, um endpoint diferente fez o ponto oposto com o mesmo estado.

A tela de compra pergunta *quem é dono deste recibo* antes de deixar alguém comprar — para que um usuário apresentando um recibo antigo expirado receba "não há compra aqui" em vez de ver um dono obsoleto. Responder isso precisa de uma noção de compra que não conta mais. A implementação óbvia reusa o conjunto que concede entitlement: está ali, é testado, já classifica status.

É o conjunto errado. O certo é **expired e revoked apenas**. Billing retry não está nele.

Porque essas são duas perguntas diferentes:

- *Esta compra dá direito ao PRO ao usuário?* — uma pergunta sobre acesso. Billing retry: sim, por decisão.
- *Existe alguma compra viva aqui?* — uma pergunta sobre existência. Billing retry: sim, obviamente. Alguém está no meio de uma renovação.

Elas concordam na maioria das entradas e discordam nessa uma, e o reuso é tentador precisamente porque se sobrepõem. Fundir as duas teria respondido "nenhuma compra" para um assinante pagante cujo cartão acabou de recusar — o que, na tela de compra, o convida a comprar uma segunda assinatura.

Então: um enum, dois predicados, dois nomes, dois testes. Isso não é duplicação. Os conjuntos diferem porque as perguntas diferem, e qualquer refatoração que os unifique reintroduz o bug.

A forma geral do erro vale nomear. O status de assinatura de uma loja descreve um *ciclo de vida de cobrança*. Não é uma permissão, uma flag de disponibilidade, ou um estágio do ciclo de vida do seu próprio produto — cada um desses é uma função separada sobre ele. E a armadilha é que o nome descreve os valores em vez da pergunta: `entitlingStates` convida ao reuso por qualquer um com uma pergunta diferente e valores parecidos. `statesThatGrantPro` e `statesWithNoLivePurchase` os dois teriam tornado o descasamento óbvio no call site.

## O que eu levaria disso

**Descubra quantos sistemas guardam a coisa que você está a ponto de mudar.** Eu assumi um e eram quatro, e a opção que propus teria escrito o menos importante deles.

**Registre a premissa, não só a decisão.** A minha estava errada; a única razão pela qual isso foi recuperável é que estava escrita ao lado da resposta que produziu.

**Sem transação, ordene escritas pelo que cada falha parcial deixa para trás.** Todo prefixo deveria ser um estado que você consegue explicar a um usuário.

**Invalide um cache depois da escrita que ele invalida.** Invalidar primeiro relê o valor obsoleto de uma fonte não atualizada e o cacheia de novo.

**Prefira replay idempotente a compensação, e retorne a lista do que caiu.** Replay usa o caminho de código que executa todo dia; compensação usa um que só executa quando algo já está errado.

**Verifique se cada valor no seu conjunto de aceitação consegue de fato alcançar o resultado de aceitação.** Um estado que estruturalmente não consegue é um branch morto que se lê como vivo.

**Teste diferencial não encontra um defeito que as duas implementações compartilham.** 243.318 comparações, zero discordâncias, bug intacto.

**Duas implementações de uma regra precisam ser corrigidas numa mudança**, ou uma afirmação de paridade verdadeira se torna um falso alarme que você aprende a ignorar.

**Nomeie um conjunto pela pergunta que ele responde, não pelos valores que contém.** Duas perguntas que concordam na maioria das entradas ainda são duas perguntas, e a entrada em que elas diferiam era um assinante pagante sendo informado de que nunca havia comprado nada.

---

*Parte de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), dezessete posts sobre extrair a metade de assinaturas de um monolito Django para um serviço em Go.*
