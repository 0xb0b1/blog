---
title: "Não Existem Soluções, Apenas Trade-Offs: O Capítulo 1 do DDIA e Quatro Decisões Que Eu Realmente Tomei"
date: 2026-07-06
description: "O primeiro capítulo da segunda edição de Designing Data-Intensive Applications não é sobre técnicas — é sobre trade-offs. Lendo-o à luz de quatro decisões reais de backend em uma plataforma esportiva: monolito vs distribuído, banco compartilhado vs banco por serviço, system of record vs dados derivados, e cloud vs self-hosting."
tags:
  [
    "golang",
    "ddia",
    "arquitetura",
    "design-de-sistemas",
    "microservices",
    "sistemas-distribuidos",
    "postgresql",
    "trade-offs",
    "backend",
  ]
---

O primeiro capítulo da segunda edição de _Designing Data-Intensive Applications_ abre com uma citação de Thomas Sowell que acaba sendo o ponto central do livro inteiro:

> Não existem soluções; existem apenas trade-offs. [...] Mas você tenta conseguir o melhor trade-off possível, e isso é tudo que você pode esperar.

Eu já escrevi sobre [como os conceitos de stream processing do DDIA se aplicam a um sistema de notificações real](/posts/ddia-stream-processing-notification-systems). Aquele capítulo me ensinou técnicas — event streams, backpressure, exactly-once. O Capítulo 1 é diferente. Ele não te ensina uma técnica. Ele te ensina quais perguntas fazer antes de escolher uma, percorrendo quatro escolhas contrastantes que moldam todo sistema de dados: operacional vs analítico, cloud vs self-hosting, distribuído vs single-node, e a tensão entre o negócio e os direitos das pessoas cujos dados você guarda.

Ler isso depois do fato foi desconfortável, no bom sentido. Cada decisão que o capítulo enquadra como um trade-off, eu tinha tomado sob pressão de prazo na R10 Score — às vezes bem, às vezes por acidente. Este post passa quatro dessas decisões de volta pela lente de Kleppmann e Riccomini. Não para exibir boa arquitetura, mas para mostrar como é quando você escolhe o lado "errado" de uma "boa prática" de propósito e sabe exatamente o que abriu mão.

## Decisão 1: Ficamos Monolito Por Mais Tempo do Que os Diagramas Permitem

O DDIA lista oito razões legítimas para distribuir um sistema: distribuição inerente, requisições entre serviços de cloud, tolerância a falhas, escalabilidade, latência, elasticidade, hardware especializado e conformidade legal. Depois ele faz algo que a maioria do conteúdo sobre arquitetura se recusa a fazer — argumenta na direção oposta:

> Mais nós nem sempre são mais rápidos; em alguns casos, um programa single-threaded simples em um computador pode ter um desempenho significativamente melhor do que um cluster com mais de 100 núcleos de CPU. [...] realizar uma tarefa em uma única máquina costuma ser muito mais simples e barato do que montar um sistema distribuído.

E sobre microservices especificamente:

> Microservices são primariamente uma solução técnica para um problema de pessoas: permitir que times diferentes progridam de forma independente sem precisar coordenar uns com os outros.

Essa frase é a que eu gostaria de ter lido três anos antes. A R10 começou como um monolito Python/Django — o `r10-hub` — e continuou assim por muito mais tempo do que os posts sobre microservices diziam que deveria. Só extraímos serviços em Go quando havia uma razão concreta que o DDIA reconheceria: notificações tinham um perfil de recursos genuinamente diferente (I/O-bound, com picos de fan-out durante as partidas) e precisavam escalar de forma independente do monolito que serve requisições. Odds era um time separado com seu próprio ritmo de deploy. Essas são as razões do "problema de pessoas" e do "escalar de forma independente", não "microservices são o jeito moderno".

Eu já escrevi a versão longa desse argumento em [When to Go Distributed](/posts/when-to-go-distributed). A versão curta, e a parte que o Capítulo 1 afiou pra mim, é que a extração só continuou barata porque a fronteira já existia dentro do monolito primeiro. Em Go essa fronteira é uma interface:

```go
// Inside the monolith: order depends on an interface, not a package
type UserService interface {
    GetUser(ctx context.Context, id string) (*User, error)
}

// Later, extracted: same interface, now a network call
type HTTPUserService struct {
    baseURL string
}

func (s *HTTPUserService) GetUser(ctx context.Context, id string) (*User, error) {
    // the caller's code does not change—only the failure modes do
}
```

A interface continua idêntica. O que muda é tudo aquilo sobre o que o DDIA avisa na mesma frase: aquela chamada in-process que nunca falhava agora atravessa uma rede que "pode ser interrompida, ou o serviço pode estar sobrecarregado ou cair, e portanto qualquer requisição pode dar timeout sem receber resposta. Nesse caso, não sabemos se o serviço recebeu a requisição, e simplesmente tentar de novo pode não ser seguro".

**O trade-off.** Continuar monolítico nos comprou simplicidade: um deploy, uma transação de banco, nenhum distributed tracing para debugar uma única requisição, nenhum quebra-cabeça de idempotência de retry. Custou-nos escalar de forma independente e autonomia de time — até que esses custos ficaram reais o suficiente para pagar pela complexidade distribuída. **O que teria mudado minha ideia mais cedo:** um gargalo medido que o monolito não conseguisse absorver com cache e índices, não um gargalo teórico. Não tivemos um por muito tempo, então não mudamos.

## Decisão 2: Compartilhamos um Banco Entre Serviços, De Propósito

Aqui está o Capítulo 1 enunciando a regra da forma mais direta possível:

> É comum que cada serviço tenha seus próprios bancos de dados e não compartilhe bancos entre serviços. Compartilhar um banco efetivamente tornaria toda a estrutura do banco parte da API do serviço, e então essa estrutura seria difícil de mudar. Bancos compartilhados também poderiam fazer com que as queries de um serviço impactassem negativamente o desempenho de outros serviços.

Nós compartilhamos um banco entre serviços. Três deles — o monolito Django, o `r10-notifications` e o `r10-odds` — leem e escrevem na mesma instância PostgreSQL. Pelo livro, isso é o antipadrão com nome e sobrenome: o monolito distribuído, acoplado no schema em vez de na API.

Eu sei. Eu [escrevi sobre isso em detalhe](/posts/shared-database-microservices-migration). A questão não é que não conhecíamos a regra. A questão é que, durante uma migração de monolito para microservices, a resposta "correta" — banco por serviço no dia um — exige resolver o problema de dados distribuídos antes de você ter entregue uma única feature, e o negócio não pausa para isso. Então assumimos o trade-off que o DDIA descreve, de olhos abertos, e depois gastamos nosso esforço de engenharia contendo o raio de impacto em vez de fingir que o tínhamos evitado.

A contenção é um conjunto de regras, e cada uma delas é uma resposta direta a "o schema agora faz parte da API":

```sql
-- migrations/001_create_live_activity_token.sql
-- Owned by the notifications service. Note what is NOT here.
CREATE TABLE r10_live_activity_token (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID         NOT NULL,   -- references r10_user.id, but no FK
    match_id     UUID,                    -- references r10_match.id, but no FK
    device_token VARCHAR(500) NOT NULL,
    state        VARCHAR(20)  NOT NULL DEFAULT 'registered',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, device_token)
);
```

Nenhuma foreign key entre serviços. `user_id` aponta para um usuário conceitualmente, mas não existe `REFERENCES r10_user(id)`. Isso é deliberado: uma foreign key faria uma mudança de schema em `r10_user` no monolito exigir um deploy coordenado de um serviço Go que o time do monolito nunca viu. O "difícil de mudar" do DDIA vira "impossível de mudar sem uma reunião entre times". Abrir mão da FK troca integridade referencial — linhas órfãs agora são possíveis — por independência de deploy. Numa migração, a segunda vale mais do que a primeira.

**O trade-off.** Um banco compartilhado nos permitiu extrair serviços de forma incremental sem construir pipelines de CDC ou um API gateway no dia um. Custou-nos a fronteira limpa: mudanças de schema em tabelas compartilhadas exigem um grep por três repositórios porque nenhuma ferramenta rastreia o acoplamento, e dois serviços escrevendo na mesma tabela é onde os bugs moram. **O que mudaria minha ideia:** nós já conhecemos a saída. Assim que um serviço tocar apenas suas próprias tabelas — `r10_live_activity_token` é exclusiva de notificações — cortamos essas tabelas para um banco dedicado, e o prefixo `r10_` torna o corte óbvio. O banco compartilhado é um estado do qual estamos saindo, não um destino que escolhemos.

## Decisão 3: O Monolito É o System of Record; Todo o Resto Guarda Dados Derivados

O Capítulo 1 introduz uma distinção que eu agora uso o tempo todo:

> Um system of record, também conhecido como source of truth, guarda a versão autoritativa ou canônica dos dados. [...] Se houver qualquer discrepância entre outro sistema e o system of record, o valor no system of record é (por definição) o correto.

> Dados em um sistema derivado são o resultado de pegar dados existentes de outro sistema e transformá-los ou processá-los de alguma forma. Se você perder dados derivados, pode recriá-los a partir da fonte original.

A decisão de não ter foreign keys da seção anterior é, na verdade, uma afirmação sobre qual sistema é dono de quais dados. `r10_user` é escrito pelo monolito; o monolito é o system of record dele. Quando o serviço de notificações guarda um `user_id`, ele não é co-dono daquele usuário — ele guarda uma referência derivada que poderia reconstruir a partir da fonte se precisasse. Nomear isso em voz alta muda como você raciocina sobre falhas. Uma query contra um `user_id` que não existe mais não é corrupção de dados; é um valor derivado desatualizado, e a aplicação lida com o miss:

```go
// The Go service reads the monolith's system-of-record tables directly
query := `SELECT id, role, language FROM r10_user WHERE id = $1`
// A miss here means "the source of truth moved on," not "the DB is broken."
```

(Não vou recobrir de propósito o cache de tópicos no Redis aqui — esse é o exemplo clássico de dados derivados e eu já o trabalhei como [dualidade stream-tabela](/posts/ddia-stream-processing-notification-systems) no outro post.)

O que o Capítulo 1 me fez encarar é a parte que eu não tinha resolvido: dados derivados precisam ser mantidos atualizados. Kleppmann e Riccomini são diretos ao dizer que "quando os dados em um sistema são derivados dos dados em outro, você precisa de um processo para atualizar os dados derivados quando o original no system of record muda". Neste momento os serviços Go leem `r10_user` e `r10_match` fazendo query direto na tabela fonte — o "processo de atualização" mais rudimentar possível, que é não ter cópia derivada nenhuma e simplesmente enfiar a mão na fonte. Isso só funciona porque compartilhamos o banco. No momento em que o separarmos, passamos a dever uma resposta de verdade: uma read replica, um stream de change data capture, ou uma chamada de API. Cada uma é um trade-off diferente entre desatualização, acoplamento e custo operacional, e o Capítulo 1 é honesto ao dizer que não existe versão de graça.

**O trade-off.** Tratar o monolito como a única source of truth mantém a consistência simples — existe exatamente um escritor para cada fato. Custa autonomia aos serviços Go: eles não conseguem responder uma query que a tabela fonte não serve, e herdam a disponibilidade do monolito. **O que mudaria minha ideia:** quando o acoplamento direto com `r10_user` começar a causar incidentes — o downtime do monolito virando o downtime do serviço de notificações — esse é o sinal para construir uma cópia derivada de verdade e pagar o custo de propagação.

## Decisão 4: Compramos o Fan-Out em Vez de Construí-lo

O DDIA enquadra cloud vs self-hosting como build-vs-buy, e é refrescantemente sem romantismo sobre os dois lados. O argumento a favor de comprar:

> Se você precisa de um sistema que ainda não sabe como fazer deploy e operar, adotar um serviço de cloud costuma ser mais fácil e rápido do que aprender a gerenciar o sistema.

O argumento contra, que a maioria do marketing de cloud pula:

> A maior desvantagem de um serviço de cloud é que você não tem controle sobre ele. [...] Se o serviço cai, tudo o que você pode fazer é esperar ele se recuperar. [...] tornando o vendor lock-in um problema.

O fan-out de notificações é a decisão de comprar-vs-construir mais clara que tomamos. Quando um gol é marcado, um evento precisa chegar a centenas de milhares de dispositivos. Poderíamos ter construído essa camada de fan-out — um log, offsets de consumidor, workers de entrega, estado de retry. Em vez disso, publicamos uma vez no AWS SNS e deixamos ele fazer o fan-out para FCM e APNs. Esse é o trade-off do DDIA na sua forma mais pura: terceirizamos a operação de um problema distribuído difícil para um fornecedor que o roda para milhares de clientes e, em troca, aceitamos exatamente a desvantagem que o livro nomeia. O SNS é fire-and-forget — perdemos a capacidade de replay na camada de fan-out. Se o SNS faz throttle ou degrada, esperamos; não podemos abri-lo. E a API é proprietária, então trocar de provedor de push é uma migração de verdade, não uma mudança de config.

Decidimos que a simplicidade operacional valia mais do que o controle, porque o fan-out de entrega não é a vantagem competitiva da R10 — os dados esportivos e o produto são. Essa é a própria heurística do DDIA: "coisas que são uma competência central ou uma vantagem competitiva da sua organização devem ser feitas internamente, enquanto coisas que são não-centrais, rotineiras ou corriqueiras devem ser deixadas para um fornecedor".

O mesmo raciocínio vale na direção oposta para as coisas que controlamos. Este blog é um único binário Go em uma única máquina no Fly.io — sem Kubernetes, sem autoscaling group. O DDIA chamaria isso de leitura correta: a carga é previsível e pequena, então "costuma ser mais barato comprar suas próprias máquinas e rodar o software nelas você mesmo" (ou, nesse caso, uma instância pequena). Distribuí-lo seria complexidade em busca de um problema.

**O trade-off.** Fan-out gerenciado nos deu um problema difícil resolvido por especialistas e zero infraestrutura de entrega para operar. Custou-nos replay, observabilidade profunda da entrega e portabilidade barata de provedor. **O que mudaria minha ideia:** se garantias de entrega ou auditoria por mensagem virassem um requisito de produto — digamos, entrega comprovável para um tier pago — a perda de controle começaria a superar a economia operacional, e um fan-out self-hosted ou híbrido justificaria seu custo.

## Decisão 5: O Eixo Que os Engenheiros Esquecem

O quarto pilar do Capítulo 1 é o que eu teria pulado alguns anos atrás, e aquele com que mais me importo agora que me movi em direção à segurança de aplicações: sistemas de dados, lei e sociedade. O livro é direto ao dizer que isso é uma entrada de projeto, não algo de compliance para depois:

> Considerações legais estão influenciando os próprios fundamentos do design de sistemas de dados. Por exemplo, a GDPR concede aos indivíduos o direito de ter seus dados apagados sob solicitação [...]. Porém, [...] muitos sistemas de dados dependem de construções imutáveis, como logs append-only, como parte do seu design. Como podemos garantir a deleção de alguns dados no meio de um arquivo que deveria ser imutável?

Essa pergunta cai pesado sobre qualquer design event-sourced ou append-only. Ela também reenquadra uma decisão de armazenamento como uma decisão de risco. Nós guardamos device tokens, e poderíamos guardar muito mais — logs de IP, localização detalhada de check-ins em partidas. O princípio de _minimização de dados_ do DDIA (o termo alemão _Datensparsamkeit_) é o contrapeso ao reflexo de "guardar tudo, pode ser útil":

> Uma vez que todos os riscos são levados em conta, pode ser razoável decidir que alguns dados simplesmente não valem a pena ser armazenados, e que portanto deveriam ser deletados. [...] os custos de armazenamento vão além da conta que você paga [...]. O cálculo de custo-benefício também deveria levar em conta os riscos de responsabilidade legal e dano reputacional se os dados vazassem.

Não vou afirmar que resolvemos isso completamente — isso seria exatamente o tipo de arrumação fabricada que este blog tenta evitar. O estado honesto é que o Capítulo 1 transformou "o que devemos logar?" de uma pergunta de debugging em um trade-off com um eixo de responsabilidade legal: cada campo que você retém é um campo que você tem que proteger, deletar sob solicitação e justificar guardar. O dado mais seguro é o dado que você nunca armazenou.

## O Capítulo Inteiro em Uma Tabela

Nenhuma dessas decisões tem uma resposta certa — apenas um lado escolhido e um custo conhecido. Essa é a tese inteira do capítulo, e vale a pena ver as quatro dispostas nos mesmos eixos:

```
Decisão               Conceito do DDIA      O que escolhemos     O que abrimos mão          O que mudaria minha ideia
--------------------  --------------------  -------------------  -------------------------  ----------------------------
Monolito vs           distribuído vs        ficar monolítico     escalar de forma           um gargalo medido que o
 distribuído          single-node; "não      até ter necessi-    independente, autonomia    monolito não absorva com
                      corra pro distribuído"  dade real          de time (por um tempo)     cache/índices
Banco compartilhado   "compartilhar o banco banco Postgres       fronteiras limpas;         um serviço tocando só suas
 vs banco por serviço  torna o schema parte  compartilhado       mudança de schema exige    tabelas (aí separamos)
                       da API"                                    grep entre repos
System of record vs   source of truth vs    monolito é o SoR,    autonomia dos serviços     acoplamento direto com
 dados derivados      dados derivados        outros guardam       Go; herdam a               r10_user causando incidentes
                                             refs derivadas       disponibilidade do          entre serviços
                                                                  monolito
Cloud vs              build vs buy;          comprar: fan-out     replay, observabilidade    entrega comprovável virar
 self-hosting         vendor lock-in         gerenciado (SNS)     de entrega, portabili-     requisito de produto
                                                                  dade de provedor
Dados e sociedade     minimização de dados; minimizar o que      features que "mais         um risco de responsabilidade/
                      direito ao esqueci-    guardamos            dados" poderiam ter        privacidade superando o valor
                      mento                                       habilitado                 do dado
```

Os melhores engenheiros com quem trabalhei não têm um saco de respostas certas. Eles têm um senso apurado de qual trade-off estão pisando e do que seria preciso para mudar. É isso que o Capítulo 1 te dá — não soluções, mas os eixos sobre os quais raciocinar. Toda vez que me arrependi de uma decisão de arquitetura, não foi porque escolhi o lado errado. Foi porque não percebi que havia um trade-off até ele quebrar em produção.

Leia o capítulo com seus próprios sistemas abertos em outra janela. Para cada uma das suas decisões estruturais, faça a única pergunta que o capítulo não para de fazer: o que você abriu mão para conseguir isso, e o que faria o outro lado valer a pena?

---
