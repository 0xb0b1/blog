---
title: "Os Testes que Guardam o Processo Envelhecem Mais Rápido"
date: "2026-08-11"
description: "Gateamos cada fase desta extração numa verificação mecânica: cada critério de aceitação precisa de um teste que passa, e o veredito é um exit code. Ela pegou defeitos reais. Também produziu três lições inteiramente sobre ela mesma — porque um teste cujo sujeito é a forma do seu código é invalidado por refatoração por design, e toda falha falsa é um convite para enfraquecê-lo."
tags:
  [
    "testes",
    "processo",
    "verificacao",
    "refatoracao",
    "workflow",
    "extracao-de-assinaturas",
  ]
---

Estamos [extraindo a metade de assinaturas e pagamentos de um monolito Django para um serviço em Go](/pt/posts/quantify-the-failure-before-you-redesign-it), em fases — quatorze até agora. Cada fase recebe uma especificação com user stories e critérios de aceitação numerados, e a regra que faz isso ser mais que papelada é esta: **todo critério de aceitação precisa de um teste que passa, e o veredito é um exit code de processo em vez da opinião de alguém.** Uma fase ou audita limpa ou não está pronta. Não há adjetivo disponível.

Isso se pagou. Pegou critérios que tinham testes que nunca de fato passaram, arquivos que foram escritos e nunca mapeados a nenhuma tarefa, e — repetidamente — a descoberta de que o estado real de uma fase era pior que o resumo do autor.

Também produziu três lições que são inteiramente sobre *ela mesma*, e são essas que quero anotar. Os testes que guardam um processo são código. Têm falhas de design como qualquer outro código, e envelhecem mais rápido que o código que guardam, porque o sujeito deles é a forma do repositório em vez do comportamento dele.

## Asserções que proíbem formas

A maioria dos critérios é provada por testes comportamentais comuns. Um punhado não pode. "O entry point passa pela montagem compartilhada do router" não é observável de fora do processo. Nem "o cliente de loja é construído exatamente quando suas credenciais estão configuradas". Esses são provados afirmando sobre texto de código-fonte, e eu ainda acho isso correto — uma fase anterior entregou quatro endpoints que responderam 404 em produção precisamente porque nada verificava como o entry point era montado.

Duas dessas asserções deram errado, do mesmo jeito, em fases consecutivas.

**A primeira afirmava que o código contém `cfg.X.Enabled() && writePool != nil`.** Uma refatoração posterior içou os clientes de loja para fora do bloco de fila, dividindo aquela expressão em dois lugares. Nada quebrou. As duas metades da condição sobreviveram, um nível separadas — o cliente ainda é construído exatamente quando as credenciais estão habilitadas, o processador ainda é gateado no pool de escrita.

**A segunda afirmava que o código não contém `DB: pool.Pool` em nenhum lugar do entry point.** Verdadeiro quando os processadores eram a única coisa segurando um pool. Então uma fase adicionou componentes somente-leitura que corretamente pegaram o pool *reader* — fazendo exatamente a coisa certa — e a asserção genérica falhou uma mudança que deveria ter recebido bem.

As duas foram estreitadas em vez de enfraquecidas: escopadas para o bloco sobre o qual de fato eram, força inalterada. Mas o padrão é o ponto. **Uma asserção sobre um arquivo inteiro proíbe formas; uma asserção sobre uma construção afirma um significado.** A primeira tem que estar correta sobre código que ainda não existe, o que não é algo que um teste consegue ser. "Esta expressão não aparece em lugar nenhum deste arquivo" é uma afirmação sobre toda linha futura, e o futuro fica adicionando linhas que estão ótimas.

Levou três ocorrências para ver isso, e a terceira me convenceu de que não era sorte. Afirme sobre o bloco, não sobre o arquivo.

A distinção generaliza para além deste projeto. Um teste comportamental é estável sob refatoração por design. Uma asserção estrutural é *invalidada* por refatoração por design. Isso não é um defeito da técnica — é o custo corrente dela, e se você a adota está assinando para reescopar essas asserções periodicamente. O que você não pode fazer é enfraquecer uma para fazer uma refatoração passar, porque naquele momento você não consegue distinguir entre "esta asserção é ampla demais" e "esta refatoração quebrou a propriedade". O teste para saber em qual você está: enuncie o significado que a asserção protegia. Se você não consegue, ainda não sabe se a refatoração era segura.

## Uma prova que fica obsoleta se você corrige um typo

A segunda lição é menor e custou mais.

A verificação registra, por critério, que um teste específico passou contra um estado específico do código. Mude qualquer entrada e a prova registrada não descreve mais o que está em disco, então o engine a marca como obsoleta. Isso é correto, e é todo o valor — uma prova que sobrevive a edições arbitrárias não é uma prova.

A consequência prática: **verifique por último, gateie imediatamente, nada no meio.** E "nada" acabou incluindo uma mudança de uma linha numa fixture de teste.

Eu aprendi duas vezes. Uma quando uma fase reverificou todas as fases anteriores exatamente como o processo prescreve, e então deletou um arquivo de scaffold e editou uma lista de tarefas, obsoletando as oito provas que tinha acabado de renovar. Outra quando uma fixture foi arrumada depois da verificação, transformando um gate limpo em doze erros.

Nenhuma foi um erro no código. As duas foram o instinto ordinário de corrigir a coisa pequena que você notou enquanto estava ali. A regra é trivial de enunciar e eu ainda precisei dela duas vezes, que é uma definição justa de regra que vale escrever: a sequência é verificar, então gatear, e *qualquer* edição a reseta para o começo.

Existe uma lição relacionada abaixo dela, aprendida antes e confirmada repetidamente: **reverifique as fases anteriores antes do gate, não depois que ele fica vermelho.** Trabalho numa fase posterior toca arquivos compartilhados — o pacote de config, o pacote de banco, os dois processadores — e toda fase anterior que afirma sobre esses fica obsoleta de uma vez, por razões não relacionadas à correção da fase atual. A primeira execução do gate de uma fase produziu oito provas obsoletas e dois arquivos órfãos; nenhum deles significava que algo estava errado. Duas fases consecutivas depois passaram na primeira tentativa seguindo aquela regra e a regra de verificar-por-último literalmente, que é sobre a demonstração mais limpa que uma lição de processo consegue.

## Um aviso que dispara em fixtures e não deve ser enfraquecido

A terceira é uma questão de design sobre uma verificação que é correta e irritante.

Um princípio permanente é que secrets nunca aparecem em código, imposto por um padrão que casa com formas como `token = "…"`. Ele já disparou, três fases seguidas, em fixtures de teste: um purchase token, outro purchase token, um shared secret. Todos os três obviamente falsos. Todos os três casaram.

A correção tentadora é uma exceção — pular arquivos de teste, ou permitir uma anotação. Argumentei contra e ainda argumentaria.

Um padrão que não consegue distinguir uma fixture de uma credencial real está certo em não tentar. A alternativa é um padrão com buracos, e os buracos são precisamente onde um secret real acaba: num arquivo de teste, durante depuração, commitado por acidente. Então toda vez, a fixture foi para uma variável e o princípio ficou intocado.

O custo é atrito real, três fases seguidas. O que faz valer a pena é o modo de falha alternativo. Uma execução do gate passou com **onze avisos** — seis desse padrão em fixtures, cinco de uma tabela Markdown numa especificação sendo interpretada como linhas de dados. Onze avisos é o número no qual alguém para de ler avisos. As duas fontes eram ruído que eu tinha introduzido, e as duas foram removidas em vez de toleradas: a tabela virou prosa, as fixtures viraram variáveis.

**Um aviso que as pessoas aprendem a ignorar é pior que nenhum aviso.** Uma verificação que grita lobo seis vezes ensinou todos a ignorá-la na sétima, que vai ser a real. Então: limpe para zero toda vez, e nunca baixando a régua.

## A pressão que as três compartilham

Olhe onde cada uma dessas três termina e há um padrão só.

Toda falha aqui chegou como uma falha *falsa* — uma refatoração que estava bem, uma correção de fixture que era inofensiva, um aviso sobre um token obviamente falso. Em cada caso a resolução mais barata disponível era deixar a verificação mais quieta: ampliar a regex, pular o arquivo, ignorar a classe de aviso, re-rodar o gate depois da edição sem reverificar. E em cada caso isso teria removido a capacidade da verificação de pegar a coisa real.

A outra coisa que elas compartilham é o momento. Cada uma dessas chegou quando o gate estava a um passo de verde, no fim de uma fase, quando o trabalho está feito e o obstáculo restante é um detalhe técnico. Esse é o pior momento possível para estar tomando uma decisão de julgamento sobre rigor, e é precisamente quando a maquinaria te entrega uma.

Que é o argumento para o veredito ser um exit code em primeiro lugar. Não porque um número seja mais sábio que uma pessoa, mas porque às 23h com um critério pendente, um número é muito mais difícil de negociar que uma frase.

## O que eu levaria disso

**Escope uma asserção de código-fonte para a construção sobre a qual ela é.** Um padrão sobre o arquivo inteiro proíbe formas em vez de afirmar significados, e tem que estar certo sobre código que ninguém escreveu ainda.

**Estreite uma asserção que falha falsamente; nunca a enfraqueça.** Mesma força, escopo menor — e se você não consegue enunciar o significado que ela protegia, não sabe se a refatoração era segura.

**Verifique por último, gateie imediatamente, e conte um typo corrigido como uma edição.** Qualquer mudança reseta a sequência, que é a propriedade que faz uma prova valer a pena.

**Reverifique trabalho anterior antes do gate, não depois que ele fica vermelho.** Arquivos compartilhados obsoletam provas antigas por razões não relacionadas à mudança atual.

**Não coloque exceções numa verificação de secrets.** A exceção é exatamente onde uma credencial real acaba. Mova a fixture em vez disso.

**Leve avisos a zero sem baixar a régua.** Onze avisos é o ponto no qual o décimo segundo é invisível.

**Espere que asserções estruturais falhem em mudanças boas, e orce o reescopamento delas.** Esse é o custo da técnica, não um defeito nela.

**Assuma que cada uma dessas vai aparecer quando você estiver a um passo de pronto.** É para esse caso que o veredito mecânico existe.

---

*Parte de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), dezessete posts sobre extrair a metade de assinaturas de um monolito Django para um serviço em Go.*
