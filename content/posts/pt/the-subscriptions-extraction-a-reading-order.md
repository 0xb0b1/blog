---
title: "A Extração de Assinaturas: Uma Ordem de Leitura"
date: "2026-08-13"
description: "Dezessete posts sobre extrair a metade de assinaturas e pagamentos de um monolito Django para um serviço em Go — medida antes de ser desenhada, faseada para que cada passo pudesse ser desfeito, e provada contra 243.325 compras reais antes de responder a um único usuário. O que cada um acrescenta, e a ordem que faz eles fazerem sentido."
tags:
  [
    "arquitetura",
    "migracao",
    "backend",
    "extracao-de-assinaturas",
  ]
---

Ao longo de cerca de um mês escrevi dezessete posts sobre um único projeto. Foram escritos conforme o trabalho acontecia, o que significa que cada um começa de onde as coisas tinham chegado em vez de começar do início.

Este é o início.

A premissa: um app de esportes com um tier pago. Usuários assinam pela App Store da Apple, pelo Google Play, ou por Stripe na web, e uma assinatura bem-sucedida concede uma role — internamente PRO — que libera as features pagas. Tudo isso vivia num único monolito Django: os endpoints de compra que o app mobile chama, os endpoints de webhook que as lojas chamam, o catálogo do que é comprável, e a lógica que decide se uma compra dá direito ao PRO.

Extraímos a metade de assinaturas para um novo serviço em Go, e tomamos a decisão mais consequente antes de qualquer código: **os dados não se movem.** O serviço Go conecta no cluster Postgres existente do monolito e escreve as mesmas tabelas. Nenhum banco novo, nenhuma migração, nenhuma camada de dual-write.

Isso foi julgado o risco menor, e o custo é que por toda a transição **duas implementações escritas independentemente escrevem as mesmas linhas.** O que muda a definição de pronto. Uma reescrita tem sucesso quando funciona; um port nessa posição só tem sucesso quando *concorda*. Quase tudo abaixo decorre dessa única restrição.

## Medir e dimensionar

- [Quantifique a Falha Antes de Redesenhá-la](/pt/posts/quantify-the-failure-before-you-redesign-it) — "assinar não é confiável" era a premissa do projeto inteiro. Medida no log group de produção, a falha não era instabilidade: era uma máquina de estados rejeitando compras que deveria ter aceitado, 3.995 vezes em sete dias. **Comece aqui.**
- [Inventariando Toda Superfície Que um Monolito Expõe](/pt/posts/inventorying-every-surface-a-monolith-exposes) — você não pode reconstruir o que ninguém listou. Derivar as promessas do código em vez da memória, e transformar o inventário em algo que testes impõem.
- [Quem Mais Mexe Neste Banco de Dados?](/pt/posts/who-else-reaches-into-this-database) — a pergunta bloqueante de qualquer extração. Todo acoplamento com direção e evidência anexadas, mais a descoberta de que entidades chaveadas por um único provedor silenciosamente tornam seu histórico dependente de um fornecedor.

## Desenhar e comprometer

- [Uma Arquitetura Especificada em Números, Não em Adjetivos](/pt/posts/an-architecture-specified-in-numbers) — "escalável e confiável" é um desejo com boa assessoria de imprensa. Escrever o alvo como mecanismos e orçamentos, e dar a todo modo de falha conhecido um sinal consultável antes de construir qualquer coisa.
- [Um Cutover Que Pode Ser Revertido](/pt/posts/a-cutover-that-can-be-reversed) — reversibilidade como critério de aceitação em vez de um plano de rollback escrito na véspera, e por que "podemos reverter o deploy" deixa de ser verdade no momento em que dados se movem.
- [Uma Conexão de Banco Que Não Pode Machucar o Monolito](/pt/posts/a-database-connection-that-cannot-hurt-the-monolith) — durante a fase de sombra o novo serviço lê o sistema que serve todo usuário, então seus critérios são expressos como capacidades que ele deve **não ter**.
- [Tracing: Instrumentado mas Silencioso](/pt/posts/tracing-wired-but-silent) — instrumentado com OpenTelemetry no primeiro dia e configurado para não emitir nada, porque nenhum coletor tinha sido escolhido. Separar uma decisão de código de uma decisão operacional.

## Provar

- [Provando uma Reescrita Contra 243.325 Compras Reais](/pt/posts/proving-a-rewrite-against-real-purchases) — antes de responder a um único usuário, o novo serviço calculou entitlement para toda compra que tínhamos, comparado offline com as respostas do monolito. Zero discordâncias; a engenharia está inteiramente no que "zero" exigiu.
- [Cobertura, Não Apenas Concordância](/pt/posts/coverage-not-just-agreement) — um harness de paridade reportando 100% de concordância não diz nada até você saber o que ele perguntou. Transformar cobertura num portão em vez de uma estatística.
- [Uma Loja Nunca Espera Pelo Nosso Banco de Dados](/pt/posts/a-store-never-waits-on-our-database) — as lojas retentam agressivamente quando você é lento, então confirmar depois da escrita transforma uma query lenta numa tempestade de retentativas auto-amplificada.
- [As Melhores Fixtures de Teste Já Estavam em Produção](/pt/posts/replaying-real-store-notifications) — o monolito vinha armazenando toda notificação bruta de loja por anos. Todas as 720.183 transformaram um exercício de fixtures num exercício de replay.
- [Idempotência Pertence ao Banco de Dados](/pt/posts/idempotency-belongs-in-the-database) — verificações de duplicata no nível da aplicação são necessárias e insuficientes. Sob entrega concorrente, a única coisa que se sustenta de forma confiável é uma constraint.

## Portar e entregar

- [Fidelidade Vence Arrumação: Portando um Serviço de Pagamentos](/pt/posts/fidelity-beats-tidiness-porting-a-payments-service) — duas esquisitices reproduzidas de propósito, uma divergência deliberada, e por que a pressão para arrumar chega disfarçada de competência.
- [Quem Recebe PRO: Entitlement Através de Quatro Sistemas](/pt/posts/who-gets-pro-entitlement-across-four-systems) — conceder um tier pago parece definir um booleano. É um predicado aplicado a quatro sistemas sem transação entre eles.
- [O Repositório Não É o Sistema em Execução](/pt/posts/the-repository-is-not-the-running-system) — três vezes, código correto, revisado, merjado e deployado não fez nada, porque um valor nunca atravessou uma das nove fronteiras entre declaração e uso.
- [Medindo Produção e Acreditando na Coisa Errada](/pt/posts/measuring-production-and-believing-the-wrong-thing) — uma query de log deu match em 0 de 292.932.577 registros e se leu como resposta definitiva. O formato que ela buscava era 100% do tráfego vivo.
- [Os Testes que Guardam o Processo Envelhecem Mais Rápido](/pt/posts/the-tests-that-guard-the-process-age-fastest) — o portão mecânico pegou defeitos reais, e depois produziu três lições inteiramente sobre ele mesmo.

## A linha adjacente

Correndo em paralelo a isso, em outro repositório, havia uma avaliação de se um provedor de dados esportivos poderia substituir outro. Os dois projetos se encontraram exatamente uma vez, no levantamento de acoplamentos acima: entidades chaveadas a um único provedor são justamente aquelas em que uma migração de provedor deixa de ser um exercício de integração e se torna um exercício de migração de dados.

- [O Que a API Retorna vs O Que a Documentação Afirma](/pt/posts/what-the-api-returns-vs-what-the-docs-claim)
- [Um Spike de Migração Deveria Produzir uma Lista de Perdas, Não uma Recomendação](/pt/posts/turning-a-vendor-decision-into-a-document)
