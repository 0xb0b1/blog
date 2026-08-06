---
title: "Instruções São Ignoradas. Comandos Não."
date: "2026-08-05"
description: "Três vezes em um único dia, um passo claramente documentado no meu fluxo de trabalho foi ignorado. A correção nunca foi escrever melhor — foi embutir o passo no comando que já rodava naquele momento. Sobre por que procedimento escrito se degrada, por que 'igual a antes' é um cheiro de documentação, e o que significa tornar um passo impossível de esquecer."
tags:
  [
    "agentes-ia",
    "workflow",
    "automacao",
    "ferramentas",
    "experiencia-do-desenvolvedor",
  ]
---

Eu tenho um fluxo de trabalho que leva uma feature de uma especificação escrita até um conjunto de commits, com um portão de auditoria mecânico no final. A maior parte dele é um documento: um conjunto longo e cuidadosamente escrito de instruções que um agente segue, estágio por estágio. Funciona bem o suficiente para eu usar diariamente.

Então passei um dia observando de perto, e encontrei a mesma falha três vezes seguidas. Em cada uma delas, um passo estava claramente documentado. Em cada uma delas, ele foi ignorado. E em cada uma delas, a correção acabou sendo a mesma coisa — não escrever melhor, mas apagar a instrução e colocar a operação dentro de um comando que já estava rodando.

## A falha que deixou tudo óbvio

A regra do fluxo para executar tarefas diz, em parte: *faça os testes do critério passarem, `onp-spec task <feature> T-xxx done`, então commite.* Duas operações, ambas obrigatórias, em uma ordem específica.

Uma execução passou por onze tarefas. Sete delas tinham commits no branch de spec, corretamente formatados, cada um fechando uma tarefa real. Zero delas estavam marcadas como concluídas no `tasks.md`.

O dashboard, que lê o `tasks.md`, dizia **0 de 9 concluídas**. Ele estava falando a verdade. O trabalho havia acontecido; o registro dele, não.

O que torna isso digno de nota é que nada falhou. `git commit` teve sucesso independentemente de a chamada ao engine ter acontecido. Não houve erro, nem aviso, nem código de saída diferente de zero em lugar algum. A instrução para fazer as duas coisas estava ali no documento, e a única coisa garantindo o cumprimento era a diligência de quem lê, ao longo de uma execução de mais de uma hora.

## A mesma forma, duas vezes mais

**Registro da execução.** A primeira ação do fluxo deveria ser escrever um manifesto da execução, que é o que o dashboard lê para saber que existe uma execução. Uma delas fez cerca de sessenta chamadas de ferramenta explorando repositórios e infraestrutura antes de registrar qualquer coisa. Por vinte e cinco minutos o dashboard não mostrou nada, e eu presumi que estava quebrado. Não estava — genuinamente não havia execução alguma em disco para exibir.

**Limpar uma pergunta no meio da execução.** Quando o fluxo para para perguntar algo, ele escreve a pergunta no manifesto para a interface renderizar como um cartão de decisão, e a limpa depois de respondida. Uma execução parou numa pergunta, foi respondida no terminal, seguiu trabalhando — e deixou a pergunta na tela. O cartão ficou lá por seis minutos anunciando uma decisão que já havia sido tomada, porque limpá-lo era o passo três de um procedimento de três passos e a execução tinha seguido adiante depois do passo dois.

Três passos diferentes, três partes diferentes do documento, uma única forma: **uma operação que precisava ser lembrada separadamente da coisa que ela acompanhava.**

## Por que o documento perde

A leitura óbvia é que quem lê foi descuidado. Não acho que essa seja a leitura útil, porque eu reconheço essa falha em times de humanos.

Um procedimento escrito compete por atenção com o trabalho. Quando o trabalho é absorvente — você está na décima primeira tarefa de um refactor, os testes estão vermelhos, você tem quatro arquivos na cabeça — o passo burocrático é exatamente o que cai. É por isso que checklists de deploy são automatizados, por isso que `git commit -n` existe e é motivo de arrependimento, e por isso que toda revisão de incidente que termina em "deveríamos documentar melhor" produz o mesmo incidente dezoito meses depois.

O passo não falhou porque estava mal descrito. Falhou porque estava descrito *separadamente.*

## Embuta no que já roda

A correção em cada caso foi encontrar o comando que já estava sendo invocado naquele exato momento, e tornar a operação esquecida parte dele.

Para a conclusão de tarefas, esse comando é o commit. Então os dois se tornaram um:

```bash
task-commit.sh done <feature> T-019
```

Ele marca a tarefa como concluída no `tasks.md` e commita, como uma única operação. Dois detalhes importam mais do que a fusão em si.

**O assunto do commit é derivado do próprio título da tarefa**, lido do `tasks.md`. Não passado como argumento. Um commit portanto não *pode* descrever algo diferente da tarefa que ele fecha, porque não existe forma de dizer o contrário.

**Se o commit falha, o status volta.** A razão de existir desse helper é impedir um marcador `[done]` sem commit por trás, então um commit falho que deixasse o marcador setado recriaria o bug original em espelho. Testei instalando um hook de pre-commit que sai com 1: o status foi restaurado para `[pending]`, e a operação relatou a falha em vez de ter sucesso pela metade.

O mesmo movimento funcionou para os outros dois. Escritas no manifesto passam por um único helper, e tudo que antes era um passo de acompanhamento separado — limpar uma pergunta pendente, publicar progresso no tracker, transicionar um cartão — agora pega carona na escrita do manifesto que já estava acontecendo. Não existe mais uma versão de "avançar o estágio" que não faça o resto também.

## Torne o desvio detectável, não apenas improvável

Fundir as operações impede desvio novo. Não faz nada pelo desvio que já existe, e não ajuda quando alguém contorna o helper.

Então a mesma ferramenta ganhou um segundo trabalho:

```bash
task-commit.sh check <feature>     # sai com 1 se tasks.md e o branch discordam
```

Ele compara o que está marcado como concluído contra o que está de fato commitado, nas duas direções, e sai diferente de zero em qualquer discordância. O fluxo agora roda isso antes de sair do estágio de execução. Uma divergência deixa de ser algo que você nota semanas depois num dashboard e passa a ser uma verificação que falha.

Existe um terceiro subcomando, `reconcile`, que repara a direção comum: para cada tarefa com commit e sem marcador `[done]`, marca. Quero destacar uma coisa que eu errei ao escrevê-lo, porque o erro é instrutivo.

O `reconcile --apply` originalmente saía com 0 depois de fazer seu trabalho. Mas ele só consegue corrigir uma direção — ele adiciona um `[done]` de bom grado quando um commit prova isso, e se recusa a inventar um commit para uma tarefa marcada como concluída sem nada por trás. Então num repositório com esse segundo tipo de desvio, ele relataria sucesso enquanto a discordância permanecia. Um código de saída verde com desvio não resolvido torna toda a verificação inútil como portão. Agora ele rastreia desvio não corrigível separadamente e sai com 1, explicando.

## "Igual a antes" é um cheiro de documentação

Existe uma falha relacionada que vale nomear, porque é o mecanismo pelo qual um bom procedimento silenciosamente se torna nenhum procedimento.

Quando fui ver por que o passo do manifesto estava sendo ignorado, o documento dizia isto:

> Mesmo schema/mecânica de antes (runId `<UTCstamp>-<project>`, project, projectName, feature, sessionId, tasksDir, mode, kind, ticket, description, stage, note, createdAt/updatedAt).

Nomes de campos, e nada mais. Sem template, sem exemplo, sem comando — apesar do título da seção prometer uma técnica específica de escrita atômica. Quatro outros passos no mesmo documento diziam "inalterado em relação a antes".

A versão mais completa a que essas linhas se referiam não existia em lugar algum. Nem no repositório, nem no histórico do git, nem em backup. Alguém — eu — havia condensado o documento em algum momento e deixado ponteiros para uma coisa que não existia mais.

Isso é pior do que um documento incompleto, porque *parece* completo. Cada "como antes" é uma afirmação de que o detalhe está disponível em algum lugar. Quando não está, quem lê improvisa, e improvisação é onde os passos desaparecem. Se você está enxugando um runbook, a frase "inalterado em relação a antes" é um bom lugar para procurar o próximo incidente.

## O que generaliza

Qualquer coisa que precise acontecer junto com outra coisa não deveria ser uma instrução separada. Encontre o comando que roda naquele momento e coloque dentro.

Se genuinamente não puder ser fundido, torne a divergência mecanicamente detectável e verifique numa fronteira. Prosa é um bom jeito de explicar *por que* um passo existe. É um mecanismo ruim para garantir que o passo aconteça.

A distinção à qual eu sempre volto: os scripts helper são confiáveis porque eles *são* a operação. "Lembre-se de também publicar um comentário no portão de auditoria" continua sendo apenas prosa, e prosa é ignorada sob carga — por agentes, e por mim no fim de uma tarde longa.
