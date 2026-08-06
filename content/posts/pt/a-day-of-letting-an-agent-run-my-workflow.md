---
title: "Um Dia Deixando um Agente Conduzir Meu Fluxo de Trabalho"
date: "2026-08-05"
description: "Especificações barradas por uma auditoria mecânica, progresso espelhado no board do time, uma planilha de horas preenchida a partir do que realmente aconteceu. Um relato honesto de um dia construindo e depurando esse arranjo — incluindo os três work items que criei sem querer num board de produção, e a única falha que apareceu em absolutamente todos os componentes."
tags:
  [
    "agentes-ia",
    "workflow",
    "experiencia-do-desenvolvedor",
    "automacao",
    "retrospectiva",
  ]
---

Há um tempo minhas features não triviais passam por um fluxo orientado a especificação: escrever a spec, derivar as tarefas, aprovar uma vez, então executar tarefa por tarefa com uma auditoria mecânica no final. Um agente conduz; eu aprovo em um único checkpoint e reviso no fim.

Passei um dia trabalhando na maquinaria em si, em vez de através dela — ligando-a ao nosso board do Azure DevOps, fazendo-a preencher minha planilha de horas, e corrigindo o que quebrou no caminho. Isto é o que aprendi, incluindo as partes que não me favorecem.

## O arranjo, brevemente

Três peças, cada uma com um trabalho.

**Um engine de spec.** Features vivem em `.spec/features/<nome>/`: uma spec com histórias de usuário e critérios de aceite, uma lista de tarefas, um diário. Todo critério de aceite se torna um teste anotado com o id do critério. A auditoria é um comando que sai com 0 ou não — o veredito é um código de saída de processo, não uma frase que um modelo escreveu.

**Um manifesto de execução.** Cada execução escreve um pequeno JSON: projeto, feature, estágio, o ticket vinculado, o branch. Essa é a costura onde todo o resto se pendura.

**Um dashboard local.** Um servidor em Go que lê os manifestos e os arquivos de spec, para eu ver onde uma execução está sem ler um transcript.

## Uma falha, cinco vezes

Aqui está o que eu diria se você lesse apenas um parágrafo. Todo bug significativo que encontrei naquele dia era o mesmo bug com roupas diferentes: **o trabalho aconteceu e o registro dele não.**

- Onze tarefas executadas, sete commitadas, **zero** marcadas como concluídas no `tasks.md`. O dashboard leu o `tasks.md` e reportou 0 de 9. Ele estava certo.
- Uma execução fez sessenta chamadas de ferramenta explorando antes de registrar seu manifesto. Por vinte e cinco minutos o dashboard não mostrou nada e eu presumi que o dashboard estava quebrado.
- Uma execução respondeu uma pergunta no terminal e deixou a pergunta renderizada na tela por seis minutos, porque limpá-la era um passo separado.
- Uma execução passou na auditoria e chegou ao estágio final enquanto seu manifesto ainda dizia `execute`, então a interface mostrava um estágio velho ao lado de contagens de tarefas vivas e corretas.
- Cartões do board foram criados para uma execução concluída e todos eles nasceram em `To do`, porque a coisa que fecha um cartão roda por tarefa durante a execução, e a execução já havia terminado.

Em cada caso a instrução para fazer o registro existia e era clara. O que não existia era qualquer mecanismo garantindo que acontecesse. A correção, cinco vezes seguidas, foi parar de descrever o passo e embuti-lo no comando que já estava sendo invocado naquele momento — o commit, a escrita do manifesto, a transição de estágio.

Existe um corolário que só apreciei no fim do dia. Como essas operações vivem em scripts helper lidos do disco em cada chamada, uma sessão que havia carregado as instruções *antigas* ainda obteve o comportamento *novo*. No fim do dia uma execução que começou antes da correção chegou ao estágio final e criou corretamente sua Story no board e seis cartões de tarefa, completos com descrições. As instruções dela estavam velhas; o mecanismo não. Você não consegue retroalimentar prosa numa sessão em andamento, mas consegue retroalimentar um script.

## O dashboard estava falando a verdade

Duas vezes fui procurar um bug no dashboard e encontrei o bug no que estava sendo dado a ele.

"As tarefas parecem velhas" — as tarefas *estavam* velhas, no `tasks.md`, porque nada as havia marcado. "A execução parece velha" — o manifesto não era escrito havia seis minutos enquanto os campos derivados de arquivo seguiam atualizando, então metade da tela estava viva e metade congelada num momento do passado.

Nas duas vezes meu instinto foi que a exibição estava errada. Nas duas vezes a exibição era a única coisa no sistema reportando com precisão. Isso é um argumento para construir cedo a visão chata e somente-leitura: é o único componente sem motivo para mentir para você.

O dashboard tinha bugs próprios, e um vale o desvio. A detecção de mudança dele comparava payloads JSON inteiros para decidir se deveria re-renderizar. O payload continha `generatedAt`, um timestamp novo em cada requisição, então a comparação nunca coincidiu uma única vez — a interface se reconstruía a cada 2,5 segundos e me jogava de volta ao topo de qualquer documento que eu estivesse lendo. A guarda estava lá desde o início e nunca havia disparado.

## Onde eu de fato causei dano

Quero ser específico sobre isso em vez de arredondar.

Enquanto testava o novo caminho de "criar itens no board automaticamente", rodei com `AZ_BOARD=dry` — uma flag cujo propósito inteiro é imprimir requisições sem enviá-las. Ela criou três work items reais no board compartilhado do meu time: uma Story e duas Tasks, ambas visíveis para colegas, com notificações ligadas porque a permissão necessária para suprimi-las não é concedida à minha conta.

A causa era banal. Toda outra escrita naquele arquivo passava por um wrapper que respeita a flag de dry-run. A nova função de criação chamava o cliente da API diretamente. A flag era real, os testes passavam, e o caminho de código sob teste era o único caminho que a ignorava.

Apaguei os três itens e corrigi a função para interromper antes de qualquer escrita. Mas a lição não é "adicione a verificação" — é que uma flag de segurança que não é imposta em um único ponto de estrangulamento não é uma flag de segurança, é uma convenção. O caminho de escrita agora tem exatamente um lugar de onde uma requisição pode sair.

Também relatei uma mudança de fonte como no ar quando não estava. O rebuild havia funcionado, o restart havia falhado silenciosamente, e o endpoint de health continuava respondendo — do binário antigo. `/healthz` retornando `ok` prova que algo está escutando. Não prova que é o seu build.

## Quanto a automação realmente vale

Duas integrações saíram do dia, e o valor delas é assimétrico de um jeito que eu não esperava.

**Espelhar o board** vale a pena porque remove uma classe de cobrança. Comentários de progresso, a mudança para `In Progress`, o link do PR, fechar cada cartão de tarefa — nada disso precisa de um humano, e tudo isso é o tipo de coisa que silenciosamente deixa de acontecer numa semana cheia.

**Preencher a planilha de horas** acabou sendo mais interessante do que útil no começo. Derivar "no que trabalhei hoje" a partir do board, dos registros de execução e do git é fácil; a parte difícil é que a resposta ingênua é inutilizável. Minha primeira rodada produziu **58 atividades para uma única terça-feira**. Work items contêineres apareceram porque criar um filho atualiza a data de modificação do pai. Cartões de tarefa individuais apareceram porque um dia de trabalho orientado a spec toca uma dúzia deles. Commits de squash apareceram duas vezes, uma delas cortada com reticências. Chegar a sete entradas utilizáveis foram quatro rodadas de filtragem, cada uma precisando de um motivo em vez de um chute.

A forma geral: automatizar o *fazer* é direto, e automatizar o *relatar* é onde você descobre o que seus dados de fato contêm.

## O que eu manteria

**Portões mecânicos.** O veredito da auditoria ser um código de saída em vez de uma frase é a propriedade mais valiosa desse arranjo. Não existe versão de "os testes passam em geral" disponível para ninguém, inclusive para mim.

**Uma visão somente-leitura alimentada pelos arquivos reais.** Estava certa todas as vezes em que duvidei dela.

**Operações, não instruções.** Se um passo precisa acontecer junto de outro, ele pertence dentro dele. Documentação serve para explicar por que um passo existe — não para garantir que ele aconteça.

**Detecção de desvio nas fronteiras.** Fundir operações previne desvio novo; não faz nada pelo desvio que já existe ou que aparece quando alguém contorna a ferramenta. Um comando `check` que sai diferente de zero quando o estado registrado e o estado real discordam transforma "algo que notamos semanas depois" numa build que falha.

O que eu diria a quem estiver construindo esse tipo de arranjo: seu agente não é pouco confiável de formas interessantes. Ele é pouco confiável exatamente das formas em que um humano cansado é, exatamente nos mesmos passos — os de registro, no fim de um trecho longo de trabalho absorvente. Projete para isso e a maioria das surpresas desaparece.
