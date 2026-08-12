---
title: "Fidelidade Vence Arrumação: Portando um Serviço de Pagamentos"
date: "2026-08-07"
description: "Quando você extrai um serviço e as duas cópias escrevem no mesmo banco, a condição de sucesso deixa de ser 'funciona' e passa a ser 'concorda'. Sobre um comentário que teria nos feito recusar assinantes pagantes, duas esquisitices que portei de propósito, e o único lugar onde divergimos deliberadamente — mais a mudança de auth que parecia otimização e era decisão de segurança."
tags:
  [
    "migracao",
    "arquitetura",
    "go",
    "python",
    "backend",
  ]
---

Um pouco de contexto, porque o resto só faz sentido com ele.

O produto é um app de esportes com um tier pago. Usuários assinam pela App Store da Apple, pelo Google Play, ou por Stripe na web, e uma assinatura bem-sucedida concede a eles uma role — internamente PRO — que libera as features pagas. Tudo isso vivia num único monolito Django: os endpoints de compra que o app mobile chama, os endpoints de webhook que as lojas chamam, o catálogo do que é comprável, e a lógica que decide se uma compra dá direito ao PRO.

Estamos extraindo a metade de assinaturas disso para um novo serviço em Go. E a decisão mais consequente do projeto inteiro foi tomada antes de qualquer código: **os dados não se movem.** O serviço Go conecta no cluster Postgres existente do monolito e escreve as mesmas tabelas. Nenhum banco novo, nenhuma migração, nenhuma camada de dual-write.

Isso foi julgado o risco menor. Mover tabelas de pagamento não pode sofrer rollback em minutos, e quebraria todo relatório de back-office que faz join nelas. O custo de evitar isso é que, por toda a transição, **duas implementações escritas independentemente escrevem as mesmas linhas.** O monolito também não pode ser desligado — Stripe ainda é inteiramente problema dele — então isso não é uma janela breve de cutover. É o estado normal das coisas por meses.

Esse único fato reescreve a definição de pronto. Uma reescrita tem sucesso quando funciona. Um port nesta posição só tem sucesso quando *concorda* — quando a linha que nosso serviço Go escreve para uma compra é a linha que o Python teria escrito. O que significa que todo lugar onde o sistema antigo é inconsistente, feio, ou aparentemente errado se torna uma decisão, e o default tem que ser reproduzir.

Isso soa óbvio escrito assim. Na prática a pressão para arrumar é constante, e ela chega disfarçada de competência.

## Como tornamos a concordância verificável

Antes dos argumentos, o instrumento, porque é ele que tornou os argumentos decidíveis em vez de retóricos.

Construímos harnesses que dirigem **o próprio Python do monolito** — não uma transcrição da lógica dele para Go, não uma descrição dela num teste. O harness de entitlement recomputa, para toda compra no banco, se ela dá direito, nos dois lados, e compara. O harness de subscribe roda o controller de compra real do monolito contra Django e Postgres, stubando apenas a API da loja e o provedor de identidade, e compara o que cada lado de fato escreveu no banco em vez do que cada lado reportou.

Essa distinção importa mais do que parece. Comparar duas implementações que eu escrevi prova apenas que fui consistente. Comparar o relatório de cada lado aceita a palavra de uma implementação. Ler as linhas resultantes do banco é a única comparação que não pode ser convencida a concordar.

Com isso no lugar, "devemos arrumar isso?" se torna uma pergunta com resposta mensurável.

## O comentário que teria recusado clientes pagantes

No nosso próprio código Go, um comentário no pacote de entitlement afirmava que o endpoint de compra do monolito aceitava apenas assinaturas `ACTIVE`, enquanto seu caminho de notificação aceitava o conjunto completo de status que concedem acesso. Ele descrevia essa assimetria como "real e intencional de preservar".

Era falso havia meses. O endpoint de compra do monolito valida contra o conjunto completo, e fazia isso desde uma mudança no começo do ano.

Considere para quem aquele comentário existia. Ele está no pacote que você leria ao construir o resolver — existe precisamente para te poupar uma viagem ao Python. Qualquer um que confiasse nele teria construído um endpoint que recusa todo assinante em grace period ou billing retry.

Esses não são casos de borda. Um assinante em billing retry é alguém cuja cobrança de renovação falhou e cujo acesso é concedido de qualquer forma, por decisão explícita de produto, enquanto a loja tenta o cartão de novo. Nosso endpoint teria compilado, passado em revisão, concordado com seus próprios testes, e virado clientes pagantes na porta.

O comentário não era descuidado. Era verdadeiro quando escrito, ficava ao lado de código que nunca mudou, e descrevia comportamento em *outro repositório* que mudou. Não existe mecanismo pelo qual ele poderia ter sido atualizado — nenhum teste, nenhum compilador, nenhum revisor que veja os dois lados. **Um comentário sobre outro sistema é um snapshot sem timestamp e sem dono.**

Duas coisas mudaram como resultado. Um teste agora fixa todos os cinco status naquela fronteira, e a ponte de verificação falha se aquela afirmação reaparecer no arquivo. O comentário obsoleto virou uma falha de teste em vez de uma armadilha.

## As duas esquisitices que portei de propósito

Então a versão real da mesma pressão, onde o código é genuinamente ruim e copiá-lo parece errado.

O monolito escreve **linhas de compra diferentes para a mesma compra**, dependendo de qual caminho a criou. No endpoint de compra, o offer id é escrito como null — mesmo quando a resposta da transação da loja carrega um. E uma flag de "oferta introdutória" é derivada da *truthiness* do campo de tipo de oferta, então qualquer oferta a marca, não apenas uma introdutória.

Nenhuma das duas é defensável nos próprios termos. A primeira descarta informação que está ali na resposta. A segunda marca um campo nomeado por uma coisa com base na presença de outra. As duas são quase certamente acidentes que ninguém teve motivo de olhar.

E nosso serviço agora tem os dois caminhos num único processo — o endpoint de compra e o processador de notificações, compartilhando um pacote. O movimento arrumado é óbvio: derivar a linha uma vez, corretamente, usar nos dois lugares. Menos código, nenhuma duplicação, nenhum null estranho. Teria parecido melhor em revisão e eu teria me sentido melhor escrevendo.

Aqui está por que é errado. **Fazer nossos dois caminhos concordarem entre si faz os dois discordarem do monolito.** Os dois serviços escrevem essas tabelas hoje, concorrentemente. Arrumar não remove uma inconsistência — realoca ela, para fora do nosso código onde é anotada e testada, para dentro da fronteira entre dois sistemas vivos, onde ela aparece meses depois como uma diferença de dados que alguém encontra num relatório.

Então as duas esquisitices são portadas, cada uma com um teste nomeando o comportamento e um comentário explicando por que ele existe. Este código proíbe política duplicada como regra; essa é a exceção, e é a exceção porque a duplicação está *na coisa sendo modelada*.

A regra que eu escreveria: **enquanto dois sistemas escrevem, fidelidade vence consistência interna.** Arrumar durante uma migração também destrói atribuição — se um relatório muda no mês que vem, foi a migração ou a melhoria? A janela para arrumar abre no dia em que o writer antigo é desligado, e não antes.

## Fidelidade pequena, uma vez aceita a regra

Um conjunto de decisões menores decorre da regra em vez de precisar de argumento individual:

**O nome de um membro de enum não é seu código de fio.** O nome do membro de um erro e a string que ele de fato envia aos clientes diferem — o membro diz service-unavailable, o fio diz unavailable. Porte o código de fio. O app está fazendo match na string.

**Uma máscara de e-mail, asterisco por asterisco.** O monolito mascara endereços com uma contagem fixa de oito asteriscos que não acompanha o tamanho do nome, então um nome de um caractere gera o mesmo caractere visível duas vezes. Copiado exatamente. Uma máscara "melhor" cuja largura acompanhasse o tamanho real vazaria esse tamanho — e qualquer cliente comparando valores mascarados entre os dois serviços os veria divergir.

**Nomes de evento e um SHA-1.** A telemetria que uma compra deixa é uma chave de join entre dois serviços. Um evento renomeado ou um hash "melhor" é um join que silenciosamente retorna nada durante o incidente que ele deveria explicar. O linter aponta o SHA-1; ele é anotado com a razão em vez de mudado.

**Um mapeamento de erro mantido como tabela explícita.** Cinco de seis recusas internas chegam ao app como uma falha genérica de validação — incluindo a que significa *o usuário pagou e não recebeu acesso*. É o que o monolito faz. Manter o mapeamento como tabela é o que torna a assimetria visível em vez de implícita, então ela se lê como decisão deliberada de mascaramento em vez de lacuna.

## Divergindo de propósito, e pagando por escrito

Nada disso argumenta contra mudar coisas. Argumenta sobre *quando*, e sobre rotulagem.

Uma divergência comportamental neste projeto é intencional. Quando o app mobile faz retry de uma compra contra nosso serviço que o monolito já processou, nós adotamos a linha existente; o monolito, com sua flag de adoção desligada, recusa. Isso foi levantado como pergunta, decidido por uma pessoa, e registrado — e o relatório de paridade nomeia aquele branch como um onde uma **discordância é o resultado esperado**, para que 300 casos concordando nunca possam ser lidos como "idêntico em todos os casos".

A segunda divergência deliberada é mais interessante, porque parecia uma otimização.

O monolito não verifica JWTs localmente. Em todo cache miss ele chama o endpoint de validação do provedor de identidade pela rede e cacheia a resposta por cinco segundos. Portado direto, isso é um round trip de rede por requisição por janela de cinco segundos, para uma verificação que poderia ser uma validação local de assinatura contra um conjunto de chaves publicado. Mais rápido, mais barato, sem dependência do provedor estar de pé. Exatamente o tipo de coisa que você corrige de passagem.

Também muda quem tem acesso. Verificação local te diz que a assinatura é válida e que o token não expirou. Ela não pode te dizer que a sessão foi revogada dez minutos atrás, porque revogação é um fato mantido pelo emissor e nada no token muda quando isso acontece. O monolito expõe uma revogação em cinco segundos. Verificação local expõe *nunca* — uma sessão revogada continua válida pelo tempo de vida restante do token, horas num cliente mobile.

Então as duas implementações respondem diferentemente para um usuário real: alguém cuja sessão foi revogada. O monolito diz 401. Verificação local diz 200. **Isso não é uma mudança de performance com uma ressalva — é uma mudança em quanto tempo uma revogação leva para ter efeito, de cinco segundos para horas.** Se isso é aceitável pertence a quem é dono da postura de segurança, não a quem está portando middleware de auth naquela tarde.

Foi registrada como pergunta aberta e colocada como a *primeira* tarefa da fase, para que a resposta chegasse antes de qualquer código de auth existir. A decisão voltou como verificação local **mais uma verificação explícita de revogação**, lendo a mesma lista de revogação que o monolito consulta.

O que me importa é o que aquela resposta mudou antes de uma linha ser escrita. A premissa registrada tinha argumentado *contra* verificação local; ela foi reescrita para declarar a decisão e seu custo — que uma sessão revogada é visível ao monolito em cinco segundos e visível aqui apenas via a verificação de revogação, que é portanto **estrutural em vez de cinto-e-suspensório.** Essa reclassificação é todo o valor. No design rejeitado, o mesmo lookup é uma rede de segurança redundante cuja falha ninguém prioriza. No design escolhido é a única coisa entre uma sessão revogada e um 200.

E um critério de aceitação foi reescrito em vez de reinterpretado. O original dizia que o provedor é chamado uma vez dentro da janela de cache — não é uma afirmação significativa sobre um serviço que não chama o provedor por requisição. Um critério carregado por uma mudança de design é pior que nenhum: ele passa, e testa algo que ninguém quer.

Existe um epílogo que torna "estrutural" concreto. A lista de revogação vive num cache compartilhado com o monolito Django, e o Django prefixa suas chaves de cache. Uma implementação inicial leu a chave sem prefixo. Ela não encontrava nada, toda vez, e reportava que a sessão não estava revogada. **Uma verificação de revogação que lê a chave errada falha aberta e reporta sucesso** — pior que nenhuma verificação, porque a existência dela é o que justificou o design, e ninguém reexamina uma decisão com base num componente que acredita estar funcionando. Foi encontrada lendo a configuração de cache do monolito, não por um teste: um teste escrito contra nosso próprio formato de chave teria usado esse formato nos dois lados e passado.

## O que eu levaria disso

**Se as duas cópias escrevem, a régua é concordância, não correção.** Decida isso no primeiro dia, porque determina se toda decisão de julgamento subsequente vai para arrumação ou para fidelidade.

**Dirija a implementação antiga, não a transcreva.** Um harness que roda o Python real e compara linhas de banco não pode ser contestado. Duas coisas que eu escrevi concordando prova apenas que sou consistente.

**Leia o código-fonte, não o comentário — especialmente comentário sobre outro repositório.** Ele não tem dono nem validade, e provavelmente já foi verdade.

**Transforme uma afirmação corrigida num teste.** Um comentário obsoleto que agora é uma asserção que falha é o único tipo de documentação que não apodrece em silêncio.

**Fazer seus caminhos concordarem entre si pode fazer os dois discordarem do sistema que você está portando.** Isso realoca a inconsistência para onde ninguém testa.

**Arrumar durante uma migração destrói atribuição.** Melhore depois que o writer antigo estiver desligado, quando uma mudança num relatório tem exatamente uma causa possível.

**Uma mudança de performance que altera quando um fato se torna falso é uma mudança de segurança.** Nomeie a janela em unidades e deixe o dono dela decidir.

**Registre divergências deliberadas onde o harness consiga ver.** Então concordância significa algo, e discordância é esperada em vez de ambígua.
