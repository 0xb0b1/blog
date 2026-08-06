---
title: "Derivando Minha Planilha de Horas do Que Eu Realmente Fiz"
date: "2026-08-05"
description: "Três fontes — o board, os registros de execução do meu próprio fluxo, e o git — combinadas em uma linha por dia trabalhado. Obter os dados foi fácil; a versão ingênua produziu 58 atividades para uma única terça-feira. Sobre por que cada filtro precisou justificar sua existência, e as quatro formas em que trabalho duplicado chega vestido de outra coisa."
tags:
  [
    "automacao",
    "bash",
    "google-sheets",
    "produtividade",
    "ferramentas",
  ]
---

Eu preencho uma planilha de horas. Uma linha por dia, com uma coluna chamada *Descrição das atividades* — no que trabalhei. Preencho no fim da semana de memória, o que significa que ela é reconstruída em vez de registrada, e a reconstrução fica mais rala quanto mais para trás você vai.

Tudo o que é necessário para escrever essa coluna com precisão já existe em algum lugar: cartões que movi no board, execuções que meu fluxo registrou, commits que eu autorei. Então automatizei. A parte interessante não foi o encanamento — foi descobrir quanto do sinal cru é ruído.

## Três fontes

**O board.** Work items cuja última alteração naquele dia foi feita por mim:

```sql
SELECT [System.Id] FROM WorkItems
WHERE [System.ChangedBy] = 'eu@exemplo.com'
  AND [System.ChangedDate] >= '2026-08-04' AND [System.ChangedDate] < '2026-08-05'
```

`ChangedBy` em vez de `AssignedTo` de propósito: um cartão atribuído a mim que outra pessoa moveu não é meu dia de trabalho, e um cartão em que eu comentei é.

**Os registros de execução do meu fluxo.** Cada execução orientada a spec escreve um pequeno manifesto JSON — projeto, feature, estágio, e um link para a Story no board se houver. Eles cobrem trabalho que nunca ganhou um cartão.

**Git.** Assuntos de commits que eu autorei naquele dia, nos repositórios em que trabalho.

Depois combinar, deduplicar, e anexar à célula do dia — apenas o que ainda não está mencionado, para que qualquer coisa que eu digitei à mão sobreviva e reexecutar seja inofensivo.

## 58 atividades para uma única terça-feira

Essa foi a primeira execução real. A célula teria mais de 2.000 caracteres. Quatro causas separadas, cada uma exigindo sua própria correção.

**Work items contêineres.** Criar um filho atualiza a data de modificação do pai e define seu `ChangedBy` para quem criou o filho. Eu havia criado cartões de Story e Task naquela manhã, então um Epic e quatro Features que eu nunca havia aberto apareceram como trabalho que eu pessoalmente fiz. Excluídos por tipo: `NOT IN ('Feature','Epic','Iniciativa')`.

**Granularidade em nível de tarefa.** Um dia de trabalho orientado a spec toca uma dúzia de cartões de tarefa sob uma única Story. Listar todos descreve a mecânica do meu dia, não sua substância. `Task` também é excluído, atrás de uma flag para os dias em que você quer o detalhe.

**Commits já cobertos pelos registros de execução.** Minha convenção de commit é `T-019 <feature>: <título>`, então esses commits descrevem exatamente o trabalho que a fonte de execuções já reporta em nível de Story. Removê-los deixa o git cobrindo aquilo para o que ele é de fato útil — o trabalho pontual que nenhuma execução de spec envolveu.

**Refs que não são trabalho.** `git log --all` inclui `refs/stash`, então `WIP on main: 0685722` apareceu como atividade. `--branches --remotes` no lugar, mais `--no-merges`, porque `Merge pull request #48 from …` é processo, não trabalho.

Depois dos quatro: **sete atividades, 393 caracteres.** Mesmo dia, mesmas fontes.

## A duplicata que passou

Squash merges produzem o mesmo trabalho duas vezes, e minha deduplicação por substring não conseguia ver:

```
correct the idempotency key to (store, store_pur… (#8245)
correct the idempotency key to (store, store_purchase_id)
```

Um do commit do branch, um do PR — onde o GitHub havia cortado o assunto com reticências e anexado o número do PR. Nenhuma das strings contém a outra, então nenhuma parecia duplicata.

Remover as duas coisas que o GitHub adiciona torna a cópia cortada um prefixo da completa:

```python
s = re.sub(r'\s*\(#\d+\)\s*$', '', s)
s = re.sub(r'\s*[…]+\s*$', '', s)
```

Depois uma segunda correção, porque acertar a *direção* da deduplicação importa: minha lógica original mantinha a variante que chegava primeiro, o que significa que o assunto truncado do PR poderia vencer o completo. Agora ela substitui uma entrada mais curta quando um superconjunto mais longo chega, então a formulação mais completa sobrevive independentemente da ordem das fontes.

## O bug que creditou trabalho ao dia errado

Registros de execução eram casados por `updatedAt`. Esse é o campo que muda em qualquer escrita — incluindo administrativas.

No fim do dia adicionei um campo a seis manifestos existentes. Duas dessas execuções haviam acontecido quatro dias antes. O `updatedAt` delas passou a ser hoje, então as duas foram creditadas a hoje e desapareceram do dia em que de fato foram feitas.

`createdAt` é o campo honesto. O trabalho de uma execução acontece no dia em que ela começa; uma edição posterior no registro dela não é um dia de trabalho. Óbvio em retrospecto, e só apareceu porque por acaso eu estava preenchendo vários dias de uma vez e pude ver o histórico se deslocar.

## Prefira títulos curtos, voltados para quem revisa

Cada fonte descreve o mesmo trabalho em um comprimento diferente. Um título de Story do board é curto e já escrito para um humano — *"Phase 1 — shadow reads"*. Uma descrição de manifesto de execução é uma frase completa escrita para mim:

> Phase 1 shadow reads — catalog, entitlement and purchasing-user read paths plus the parity harness that proves the Go rewrite against r10-hub

170 caracteres, numa célula que carrega oito atividades. Então a fonte de execuções prefere o título da Story vinculada quando existe, cai para a descrição, e qualquer coisa acima de 90 caracteres é cortada numa fronteira de palavra. Títulos do board já são mais curtos que isso, então o limite só morde a fonte verbosa.

## Não invente atividade em dias vazios

Um detalhe que parece trivial e não é. Quase toda linha da minha planilha começa com *dailys* — a reunião diária — então fiz a ferramenta prefixá-lo automaticamente.

O que significou que um domingo sem trabalho ganhou uma linha dizendo `dailys`. A planilha agora afirmava que houve uma daily num dia em que eu não trabalhei. O prefixo agora só pega carona com atividade real; um dia sem fontes deixa a linha vazia, como deveria.

A versão geral: uma constante que você adiciona incondicionalmente é uma afirmação. Se o resto da linha está vazio, essa afirmação é o único conteúdo, e é falsa.

## O que eu diria a quem for fazer isso

**Automatizar o fazer é fácil; automatizar o relatar é onde você aprende o que seus dados contêm.** Todo filtro acima veio de olhar saída real e perguntar por que algo estava ali. Nenhum deles era previsível a partir do schema.

**Todo filtro precisa de um motivo, não de um limite.** "Pegue os 10 primeiros" teria produzido uma célula plausível e descartado as coisas erradas em silêncio. "Exclua contêineres porque criar um filho atualiza o pai" é uma regra que eu ainda consigo justificar em seis meses.

**Anexe, nunca substitua.** A célula contém coisas que nenhum sistema conhece — `folga remunerada 4/10`, `R10 Planning`. Uma ferramenta que reescreve a célula apagaria isso. Anexar apenas o que falta torna a ferramenta segura para rodar repetidamente e segura para rodar sobre uma linha que eu já editei.

**Dry run por padrão.** Toda escrita só sai com `--apply`. Rodei a coisa dezenas de vezes contra dias reais enquanto ajustava os filtros, e nenhuma dessas execuções tocou a planilha.
