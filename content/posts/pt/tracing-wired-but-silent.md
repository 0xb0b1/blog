---
title: "Tracing: Instrumentado mas Silencioso"
date: "2026-07-21"
description: "Instrumentamos um novo serviço Go com OpenTelemetry no primeiro dia e o configuramos para não emitir nada, porque nenhum coletor havia sido escolhido. Sobre separar uma decisão de código de uma operacional, por que isso vence tanto \"adicionar depois\" quanto \"escolher um backend agora\", e as letras miúdas que tornam isso honesto."
tags:
  [
    "go",
    "observabilidade",
    "opentelemetry",
    "logging",
    "backend",
    "extracao-de-assinaturas",
  ]
---

A fase 0 do nosso novo serviço tinha uma seção de design com um título que gostei o suficiente para manter: **"Tracing: presente mas silencioso."**

O serviço é totalmente instrumentado com OpenTelemetry. Spans são criados, contexto propaga, toda requisição produz um trace carregando o id de correlação. E por padrão ele não exporta nada disso, porque quando construímos o scaffold ninguém havia decidido para onde traces deveriam ir.

Isso soa como o pior dos dois mundos. Eu acho que é o melhor disponível, e o raciocínio generaliza para além de tracing.

## Duas decisões tratadas como uma

"Este serviço deveria ter tracing?" e "qual backend de tracing nós rodamos?" chegam juntas e têm quase nada em comum.

A primeira é uma decisão de **código**. É sobre se handlers recebem um context, se esse context cruza a fronteira da sua fila, se a camada de banco participa. Toca toda assinatura de função no caminho da requisição. É caro de fazer retroativamente e barato de fazer enquanto você já está escrevendo o código.

A segunda é uma decisão **operacional** — fornecedor, custo, retenção, quem administra, se é auto-hospedado. Não toca código de aplicação nenhum. É caro errar e não há razão para correr.

Juntar as duas produz os dois resultados ruins que já vi repetidamente. Ou você bloqueia o scaffold numa conversa de aquisição, ou postergam as duas e "adicionamos tracing depois" — o que significa costurar context por quarenta pontos de chamada num serviço que agora tem tráfego, e fazer isso sob a pressão do incidente que te fez querer traces em primeiro lugar.

Separá-las significa que a metade caro-de-fazer-depois sobe com o código, e a metade reversível fica aberta.

## O que "silencioso" precisa significar para ser honesto

Um adiamento só é seguro se for genuinamente inerte, e há três formas de isso dar errado.

**A instrumentação precisa ser exercitada, não meramente presente.** Nosso critério é que *toda requisição produz um trace carregando o id de correlação* — afirmado em testes contra um exporter de gravação em memória. Código que constrói spans que ninguém nunca verifica é código que vai estar errado quando você ligar o exporter, e você vai depurá-lo durante a indisponibilidade para a qual você o queria.

**Silencioso precisa significar custo de rede zero.** Um exporter no-op, não um exporter real apontado para um endpoint morto. A segunda versão faz retry, buffer, timeout, e eventualmente aparece nos seus percentis de latência — uma feature "desligada" que custa milissegundos é pior que uma ausente.

**Ligar precisa ser configuração, não uma release.** Se habilitar traces exige uma mudança de código, você não postergou uma decisão, você agendou trabalho. Uma variável de ambiente, sem rebuild.

## O prêmio de consolação é a maior parte do valor

Aqui está a parte que tornou o adiamento confortável em vez de meramente defensável: o id de correlação flui pelos **logs** desde o primeiro dia.

A pergunta que de fato é feita durante um incidente é "o que aconteceu com a requisição deste usuário?" Com um id de correlação em toda linha de log estruturado, e esse id devolvido a quem chamou, isso é uma consulta — e funciona sem backend de tracing nenhum. O equivalente a um grep sobre logs estruturados responde a maior parte do que um trace responde, menos a cascata de tempos.

O que reformula a decisão de tracing honestamente. Traces te dão *tempo de span* e topologia entre serviços. Logs correlacionados te dão *o que aconteceu, em ordem*. O segundo é 80% da depuração de incidentes e não precisa de fornecedor. Então a coisa que postergamos foi a metade menor, e a coisa que entregamos imediatamente foi a metade que responde à pergunta comum.

Se você está pesando essa troca no seu próprio scaffold, eu colocaria assim: logging estruturado correlacionado não é o primo pobre do tracing. É o estrutural. Tracing é o que você adiciona quando precisa saber *por que estava lento* em vez de *o que fez*.

## O design precisa dizer por quê

A seção de design tem dois parágrafos, e diz que tracing está instrumentado, que a exportação está desligada, que ligar é uma variável, e que a decisão do coletor está aberta sem data anexada.

Essa última parte importa mais do que parece. Uma feature desligada e não documentada é indistinguível de uma quebrada. Seis meses depois, o próximo engenheiro encontra código de tracing que não produz nada e tem duas leituras possíveis: alguém postergou isso deliberadamente, ou alguém deixou pela metade. Sem a nota de design ele vai presumir a segunda, e ou arrancar tudo ou "corrigir" apontando para qualquer backend que ele goste.

Escrever "presente mas silencioso" como um título em vez de um comentário é o que converte uma ausência numa decisão. Também significa que o adiamento é visível na revisão — quem revisa pode contestar, o que não pode fazer com uma coisa que simplesmente não foi mencionada.

## O que eu levaria disso

**Separe decisões de código das operacionais e entregue a metade de código cedo.** Costurar context é caro de fazer depois; escolher um fornecedor não é.

**Teste a instrumentação com um exporter de gravação.** Construção de spans não inspecionada é um bug latente que aparece exatamente quando você não quer.

**Silencioso significa um exporter no-op e uma flag de configuração**, não um exporter real apontado para o nada e uma futura mudança de código.

**Documente o adiamento como uma decisão, com sua razão.** Caso contrário a próxima pessoa lê uma feature desligada como inacabada — e não está sendo irracional.

**Coloque o id de correlação nos seus logs primeiro.** Responde a pergunta que as pessoas de fato fazem, e não precisa de ninguém escolhendo um produto.

---

*Parte de [A Extração de Assinaturas](/pt/posts/the-subscriptions-extraction-a-reading-order), dezessete posts sobre extrair a metade de assinaturas de um monolito Django para um serviço em Go.*
