---
title: "Faça o Portão Ser Mecânico: Códigos de Saída em Vez de Adjetivos"
date: "2026-07-29"
description: "Se a definição de pronto é uma frase, ela vai ser negociada. Sobre tornar critérios de aceite executáveis, deixar um código de saída de processo ser o veredito, recusar registrar uma lição sem evidência por trás — e a lacuna que encontrei entre dez critérios que tinham testes e zero que haviam passado."
tags:
  [
    "agentes-ia",
    "testes",
    "ci",
    "workflow",
    "arquitetura",
  ]
---

Todo processo de revisão em que já trabalhei tem a mesma junta fraca: o momento em que alguém declara o trabalho terminado. Normalmente é uma frase. "Os testes passam." "Isso está pronto." "Pra mim está bom."

Frases são negociáveis. Sob pressão de prazo, "os testes passam" silenciosamente se torna "os testes que importam passam", que se torna "aquele que falhou já era instável". Ninguém mente; a frase simplesmente tem folga suficiente para absorver a situação.

Tenho rodado um fluxo de trabalho em que essa junta é um código de saída de processo, e a diferença é maior do que eu esperava.

## Critérios que são testes, não prosa

O fluxo começa com uma spec contendo histórias de usuário e critérios de aceite, cada um com um id — `US-011`, `AC-022`. Comum o suficiente. A parte que faz o trabalho é o passo seguinte: todo critério de aceite precisa ter um teste anotado com seu id.

```go
// @spec:AC-023
func TestConfigValidateNamesTheOffendingVariable(t *testing.T) {
```

Essa anotação é o truque inteiro. Significa que a pergunta "o `AC-023` está satisfeito?" tem uma resposta mecânica — encontre o teste que carrega esse id, veja se ele passou. Não existe passo interpretativo em que alguém decide se o critério está *basicamente* coberto por um teste vizinho.

Também torna a pergunta inversa respondível, o que acaba importando mais. "Quais critérios não têm teste?" é uma consulta, não um julgamento. Qualquer coisa sem anotação está visivelmente não comprovada, em vez de tacitamente presumida.

Duas regras mantêm isso honesto, e são o tipo de coisa que precisa ser declarada como inviolável ou se erode:

- Um teste ignorado não é prova. É um critério não comprovado que por acaso está documentado.
- Você nunca enfraquece, ignora ou apaga um teste para passar pelo portão. Se o portão está falhando, o código é o que está errado.

Essa segunda parece óbvia escrita assim. É exatamente o que é violado às 18h.

## O veredito é um código de saída

A auditoria roda como um comando:

```bash
onp-spec audit --ci
```

Saída 0 ou a feature não está pronta. Não "a auditoria reporta que a feature está em boa forma" — um número que o shell devolve. O fluxo é construído de forma que nenhum participante, humano ou não, tenha vocabulário para discutir com ele. Não existe campo em lugar algum que guarde um veredito em prosa, porque um campo assim eventualmente seria preenchido com algo otimista.

Quando o portão falha, o fluxo tem três tentativas de corrigir a causa, cada uma registrada. Ainda falhando depois de três e ele *estaciona*: a execução para, os achados são apresentados em ordem, e ele espera por um humano. Esse limite importa tanto quanto o portão. Sem ele, "corrija e tente de novo" se torna um laço sem limite que eventualmente encontra um jeito de fazer a verificação passar sem fazer o software estar correto — o que é um resultado estritamente pior do que parar.

## A lacuna entre escrito e verificado

Aqui está o achado que me convenceu de que essa maquinaria valia a pena.

Rodei o comando de status numa feature que havia passado por todo o fluxo e estava em `implementing`:

```
feature                     criteria  with-test  proven
phase-0-service-scaffold          10         10       0
```

Dez critérios de aceite. Dez deles tinham testes anotados. **Zero havia passado por uma verificação.**

Nada estava quebrado. O passo de scaffolding havia feito exatamente seu trabalho — ele escreve um esqueleto de teste que falha para cada critério antecipadamente, de forma que a definição de pronto existe antes da implementação. Os testes eram reais, corretamente anotados, e vermelhos.

Mas leia esses três números como uma frase e você vê no que eles teriam se transformado numa reunião de status. "Todos os dez critérios têm testes" é verdade, soa como conclusão, e é compatível com nenhum deles passando. As colunas `with-test` e `proven` serem separadas é o valor inteiro da ferramenta. Um único número de "cobertura" teria escondido isso.

## Recusando lições sem evidência

O fluxo tem um estágio de aprendizado: depois de uma execução, ele pode registrar uma lição para execuções futuras absorverem. Essa é a parte que eu esperava ser inútil, e é a parte com a decisão de design mais afiada.

O engine se recusa a registrar uma lição a menos que exista um sinal real por trás — um achado de auditoria de verdade ou uma falha de verificação de uma execução registrada. Peça a ele para lembrar algo em que você meramente acredita e ele falha com `LESSON_WITHOUT_EVIDENCE`.

Na primeira vez que isso me pegou, a execução havia ido perfeitamente e eu queria registrar algo mesmo assim. A resposta do engine, aproximadamente: *nenhum sinal recorreu em duas ou mais features distintas; nada digno de lição por agora.* E está certo. Uma execução limpa não é uma oportunidade perdida de anotar sabedoria. Um arquivo de lições que acumula conselhos de aparência plausível depois de cada execução se torna ruído em um mês, e então ninguém lê a única entrada que importava.

Existe uma regra de promoção anexada: um sinal precisa recorrer em features separadas antes de se tornar uma regra candidata. Uma ocorrência é um incidente. Duas é um padrão. Só padrões são promovidos à constituição — o arquivo de princípios verificado mecanicamente em toda execução subsequente.

## Onde isso não alcança

Dois limites honestos.

**Um portão mecânico só cobre o que os critérios dizem.** Se a spec omite um caso, nenhum código de saída vai notar. O portão te protege de declarar pronto um trabalho não verificado; não te protege de uma spec incompleta. A resposta do fluxo é fazer de premissas e perguntas abertas artefatos de primeira classe — códigos `ASM-xxx` e `Q-xxx` que a auditoria conta e reporta — para que ao menos as omissões sejam visíveis em vez de invisíveis.

**Estado registrado pode divergir do estado real.** Essa é a falha que eu de fato encontrei, e é a imagem espelhada de tudo acima. Uma execução commitou sete tarefas e não marcou nenhuma como concluída no arquivo de tarefas. A auditoria não tinha do que reclamar, porque a auditoria verifica critérios contra testes, não commits contra registros. O dashboard leu o arquivo de tarefas e reportou nenhum progresso, com toda a verdade.

A correção foi outra verificação mecânica, deliberadamente estreita: um comando que compara o que está registrado como concluído contra o que está de fato commitado, nas duas direções, e sai diferente de zero em caso de discordância. Ele roda antes do fluxo sair do estágio de execução.

Essa é a forma geral da lição. Um portão mecânico é tão bom quanto sua cobertura das formas pelas quais as coisas podem estar erradas, e cada portão que você adiciona revela uma nova costura ao lado. Mas cada uma dessas costuras é corrigida uma vez e permanece corrigida, o que não é verdade para "deveríamos ter mais cuidado ao atualizar o arquivo de tarefas".

## O que eu levaria disso

Se você tem um passo de revisão que depende de alguém afirmar que está pronto, a mudança de maior alavancagem é transformar a afirmação em uma pergunta executável — mesmo de forma tosca. Um código de saída ganha de um checklist, um checklist ganha de uma norma, e uma norma ganha de nada.

E mantenha os dois números separados. "Tem teste" e "o teste passou" são fatos diferentes, e no momento em que você os colapsa em uma única figura, você construiu exatamente a coisa que deixa trabalho não verificado parecer terminado.
