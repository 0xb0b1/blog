---
title: "Medindo Produção e Acreditando na Coisa Errada"
date: "2026-08-10"
description: "Portar um serviço significa que toda decisão de escopo se apoia num fato sobre produção. Uma query de log deu match em 0 de 292.932.577 registros e se leu como resposta definitiva — o formato que ela buscava era 100% do tráfego vivo. Uma amostra de 500 linhas reportou 96% e o corpus completo reportou 80%. Sobre a diferença entre 'nenhum existe' e 'minha query não consegue vê-los', e o controle positivo que separa os dois."
tags:
  [
    "observabilidade",
    "medicao",
    "debugging",
    "aws",
    "testes",
  ]
---

Estamos portando a metade de assinaturas de um monolito Django para um serviço em Go. O que significa, constantemente, que dimensionar uma tarefa depende de um fato sobre produção: este formato de recibo ainda está em uso, este campo de log existe, quantas dessas linhas armazenadas conseguimos de fato replicar, alguém ainda chama este endpoint.

Não há como responder essas do repositório. O código contém todo branch que já foi escrito, incluindo os que nenhum tráfego percorreu em três anos. Então o projeto fez da medição um passo de primeira classe — o critério de aceitação de uma fase inicial era literalmente *o quadro de falha é medido, e reproduzível*, e esse hábito se pagou repetidamente.

Ele também produziu as conclusões mais confiantemente erradas do projeto inteiro. Cada uma veio de uma medição que era bem formada, executou com sucesso, retornou um resultado limpo, e respondeu a uma pergunta diferente da que eu tinha feito.

## Um zero limpo sobre 292 milhões de registros

Eu precisava saber se um formato antigo de recibo ainda estava em uso antes de decidir portar o código que o trata. Recibos StoreKit 1 versus as transações JWS assinadas mais novas: se o SK1 estivesse morto, uma tarefa inteira desaparecia, junto com um endpoint deprecado da Apple e um secret para gerenciar.

A especificação dizia que a evidência já existia — o monolito loga um campo `purchase_token_format` em toda resolução de token, então uma query de log resolveria.

A query deu match em **0 de 292.932.577 registros.**

Esse é um zero limpo sobre um denominador muito grande, e se lê como uma resposta. Quase 300 milhões de registros, nenhum SK1, pronto. A verdade era que o SK1 respondia por **100% do tráfego de produção observado**: 14 resoluções em 30 dias, cada uma delas SK1, zero JWS.

O campo é passado ao logger como um argumento extra, e o próprio código-fonte do monolito diz, num comentário, que seu formatter descarta esses. Então o campo nunca estava numa linha de log — não raro, ausente por construção, por toda a história do serviço. Minha query estava bem formada, rodou contra o log group correto, escaneou 292 milhões de registros, e não poderia ter dado match em nada, não importa o que produção fizesse.

A resposta veio do *texto* da mensagem, que é logado incondicionalmente. Toda linha "purchase token resolved" é seguida por uma linha sobre o recibo não ter informação de latest-receipts — que existe apenas no branch de SK1, porque o branch de JWS retorna imediatamente depois de decodificar. A presença de uma string num branch é um sinal pior que um campo estruturado em toda linha, de todas as formas exceto uma: ele estava de fato lá.

## A mesma forma, duas vezes mais

**Um log group obsoleto.** Antes daquela query rodei uma contra o nome óbvio de log group, presente na conta de produção. Ela retornou `scanned: 0`, o que se lê como "nenhum tráfego".

Seu último evento tinha seis meses, e os nomes dos streams carregavam a convenção de *staging*. Era um group abandonado sentado no formato da conta errada. O group vivo veio de ler a configuração de log do serviço em execução em vez de adivinhar um nome — e uma vez consultado, escaneou 722.828.950 registros.

`scanned: 0` é a saída mais honesta desta história e eu ainda quase a li errado. Ela não diz "nenhum evento correspondente". Diz que a query não examinou nada, o que é uma declaração sobre a query.

**Um 404 de um serviço que não é o serviço.** Numa fase anterior eu precisava comparar nossa resposta de catálogo com a do monolito, e sondei o hostname da API de staging. Ele respondeu — 404 na rota. Produção respondeu 401 no mesmo path. Concluí que a rota existia só em produção e estacionei um critério de aceitação como improvável de provar em staging, escrevendo três opções de como proceder sem ele.

Estava errado. O host responde, e parece com a API, mas seu documento OpenAPI tem exatamente dois paths sob um título sugerindo uma API combinada. É um stub. O backend real fica atrás de outro load balancer sob outro prefixo de path. Um 404 foi lido como "a rota não existe" quando significava "você está perguntando ao serviço errado".

O critério passou de estacionado a provado no mesmo dia, contra um peer vivo, depois que a requisição foi para o lugar certo. Nada sobre o código havia mudado. E a comparação não foi trivial quando rodou: as duas lojas retornaram conjuntos de assinatura correspondentes, o mesmo plano destacado, o mesmo resumo de teste grátis.

## O que as três têm em comum

Cada vez, um resultado vazio foi produzido por um **defeito na observação, não por um fato sobre o sistema.** E cada vez o resultado vazio foi mais persuasivo do que um não-vazio errado teria sido, porque zero parece a ausência de ruído em vez da presença de um erro.

Essa assimetria é a armadilha. Uma query retornando 40.000 registros quando você esperava 12 te faz checar sua query. Uma query retornando nada te faz acreditar na sua hipótese. A segunda é um disfarce muito melhor para uma medição quebrada, e chega exatamente quando você está tentando fechar uma pergunta de sim/não e está com pressa de terminar.

Vale notar quão *razoável* era cada leitura errada. Um campo que a especificação dizia ser logado. Um log group com o nome do serviço nele. Um hostname que serve a API e responde HTTP. Nenhuma foi descuido; cada uma era um artefato plausível que tinha derivado da coisa que aparentava nomear.

## A amostra que respondeu a outra pergunta

O outro modo de falha não é vacuidade, é amostragem — e produziu o erro mais instrutivo do projeto.

O monolito vinha guardando toda notificação bruta de loja que recebia havia anos, o que transforma um problema de fixtures num problema de replay: o corpus é tráfego real, e comparar dois decoders sobre ele é uma afirmação muito mais forte que comparar qualquer um dos dois contra exemplos escritos à mão. A pergunta que decide se a estratégia funciona de todo é que fração dessas linhas ainda carrega um payload recuperável, e quais tipos de notificação aparecem.

Eu medi três vezes.

- **5 amostras** — "o payload é recuperável". Verdadeiro, e não informativo.
- **500 amostras** — 96,3% recuperável, 4 tipos de notificação não cobertos. **Os dois números errados.**
- **O corpus completo, 404.474 linhas** — 80,4% recuperável, 2 tipos não cobertos. Medido.

Os 96,3% são o que eu mais gostaria de guardar, porque não foi um erro de amostragem. Foi um erro de definição vestindo a roupa de um erro de amostragem.

389.541 linhas têm um registro de log associado. 325.384 têm um *payload recuperável*. O primeiro sobre o total é 96,3%; o segundo é 80,4%. Eu tinha escrito uma query para "tem um registro de log" e reportado como "tem um payload", porque numa checagem de cinco linhas essas coisas tinham sido a mesma — toda linha com log tinha um payload nele. Um único número foi apresentado como a resposta a uma pergunta que ele não respondia, e a diferença era dezesseis pontos e 64.000 linhas.

Nenhum aumento no tamanho da amostra teria corrigido isso. A medição de 500 linhas era *mais precisa sobre a quantidade errada.*

O hábito que tirei disso: **escreva o número como a pergunta que ele responde, no mesmo movimento.** Não `recuperável: 96,3%` mas `linhas com registro de log: 96,3%`. Se o rótulo é a query, um descasamento entre rótulo e intenção é visível. Se o rótulo é a conclusão, não é.

A segunda resposta errada foi um artefato genuíno de amostragem, e importa mais que a porcentagem. A amostra de 500 linhas reportou quatro tipos de notificação ausentes do corpus; o corpus completo mostra dois. A diferença são dois tipos aparecendo 1.578 e 3.269 vezes — cerca de 0,4% e 0,8% do corpus, então suas contagens esperadas em 500 linhas são aproximadamente dois e quatro. Ver zero de qualquer um não é digno de nota.

Mas **os tipos raros são exatamente o motivo da verificação existir.** Um tipo comum de notificação é exercitado por qualquer teste que alguém escreva; são as transições de pausa e os eventos estranhos de ciclo de vida que carregam os branches que ninguém leu com cuidado. Uma amostra que perde precisamente as categorias que você mais precisa cobrir, enquanto reporta uma porcentagem de cobertura que parece boa, é pior que nenhuma amostra — ela coloca confiança no lugar errado. Para perguntas de cobertura, tamanho de amostra é um penhasco, não uma rampa: você vê uma categoria ou não vê.

## O que a medição completa achou que ninguém pediu

Medir tudo revelou duas coisas que nenhuma amostra teria mostrado, e as duas mudaram decisões.

**A cobertura tem dois buracos, e um é recente.** Nada antes de certo mês de 2023 carrega payload — esperado, é quando o logging foi adicionado. O segundo é o interessante: um trecho do fim de 2025 ao começo de 2026 onde um mês recupera 14 linhas de 5.480 e o seguinte recupera **0 de 5.255.** Algo mudou no logging e depois mudou de volta. Ninguém sabia, e uma porcentagem — mesmo uma correta — esconde isso completamente. Uma taxa de recuperação é uma distribuição no tempo, e a média é a coisa menos útil sobre ela.

**O corpus termina.** A notificação mais recente tinha data de três meses antes de eu rodar a query, e as notificações da outra loja param na mesma data. Staging tinha parado de receber tráfego de loja inteiramente, meses antes, e ninguém tinha notado porque nada em staging dependia disso.

Essa última não era a pergunta que fiz, e é a coisa mais importante que a medição produziu. Vários planos restantes envolviam observar comportamento em staging; todos estavam apoiados num ambiente que estava escuro desde maio.

Com os números reais em mão o replay comparou **319.507 notificações com zero discordâncias**, e o relatório nomeia os dois tipos que o corpus não contém em vez de deixá-los implícitos. Duas coisas tornaram isso digno de publicar: o lado Python importa o próprio modelo do monolito em vez de reimplementá-lo, e os tipos não cobertos estão no relatório como dado, com um teste afirmando que aquele campo existe — então uma execução que silenciosamente perdeu um branch não consegue deixar o critério verde.

## O controle

A correção para tudo isso é chata e mecânica. **Antes de acreditar num zero, prove que a query pode produzir um não-zero.**

- Remova todo filtro e confirme que a query retorna *alguma coisa*. Se não retorna, você está medindo seu acesso aos dados, não os dados.
- Busque uma string que você tem certeza que existe — uma linha de startup, um health check, qualquer coisa incondicional. Se isso não bate, o alvo está errado.
- Leia o denominador antes do numerador. `scanned: 0` e `scanned: 292.932.577` com o mesmo `matched: 0` são achados inteiramente diferentes, e só um é sobre produção.
- Para uma sonda HTTP, obtenha uma resposta de controle de uma rota que você sabe que é servida. Um 401 de uma rota montada ao lado de um 404 da que você está perguntando teria dito "serviço errado" imediatamente.

E acima de tudo isso: **não aceite de uma especificação uma afirmação sobre o que é logado.** Confirme que o campo aparece numa linha real antes de projetar uma medição sobre ele. A afirmação na minha foi escrita de boa fé e provavelmente já foi verdade; o comentário do formatter estava no código todo esse tempo.

## O que eu levaria disso

**Um resultado vazio mede sua query tanto quanto mede o mundo.** As duas explicações estão sempre disponíveis e nada no resultado as distingue.

**Um denominador grande torna um zero mais convincente sem torná-lo mais verdadeiro.**

**Rode um controle positivo antes de reportar um achado negativo.**

**Rotule um número com sua query, não com sua conclusão.** Uma amostra maior não corrige um denominador errado; ela faz uma medição precisa da coisa errada.

**Amostre para decidir se medir, nunca para publicar um número.** Cinco linhas estabeleceram corretamente que o payload era recuperável de todo, que era tudo que elas podiam sustentar.

**Olhe a distribuição no tempo, não a média.** O mês que recuperou 0 de 5.255 era invisível dentro de uma porcentagem do corpus inteiro.

**Meça a coisa inteira uma vez, cedo.** É uma query. Custa minutos, resolve a pergunta, e te conta coisas que você não perguntou — como que o ambiente no qual você planejava verificar estava em silêncio havia três meses.
