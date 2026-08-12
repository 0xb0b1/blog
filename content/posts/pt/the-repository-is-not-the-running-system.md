---
title: "O Repositório Não É o Sistema em Execução"
date: "2026-08-09"
description: "Três vezes num projeto, código que estava correto, revisado, merjado e deployado não fez absolutamente nada — porque o valor de que ele precisava nunca atravessou uma das nove fronteiras entre onde é declarado e onde é lido. Sobre um sintoma que quatro bugs independentes produziram, um erro de permissão que chegou disfarçado de ausência, e quatro endpoints que passaram em todos os testes e responderam 404."
tags:
  [
    "operacoes",
    "deploy",
    "configuracao",
    "testes",
    "aws",
  ]
---

Contexto primeiro. Estamos extraindo a metade de assinaturas e pagamentos de um monolito Django para um serviço em Go — webhooks de App Store e Google Play, os endpoints de compra que o app mobile chama, a lógica que decide quem recebe o tier pago. O novo serviço roda em ECS Fargate. Sua configuração é apenas variáveis de ambiente, validadas no startup, vindas do Secrets Manager e injetadas por um deploy em GitHub Actions que escreve uma task definition do ECS. Infraestrutura é Terraform. Existem duas contas AWS com topologias deliberadamente diferentes.

Isso é um setup ordinário, e quero contar uma coisa sobre ele. Para uma credencial de banco chegar ao código que abre uma conexão, ela atravessa: Terraform, Secrets Manager, uma policy de IAM, o workflow de deploy, a task definition, o ambiente do container, o carregador de config, o construtor da DSN, o pool de conexão. **Nove fronteiras.** Em cada uma, dois artefatos individualmente corretos podem discordar.

Três vezes neste projeto, trabalho que estava correto no repositório não fez nada em produção. Cada vez o código estava certo, os testes passavam, a revisão não achou nada, e o deploy estava verde. Parei de tratar isso como descuido, porque ler o código com mais cuidado não teria encontrado nenhuma delas.

## Um sintoma, quatro causas independentes

O serviço tinha um pool de escrita. Tinha um tipo writer carregando um método marcador, então entregar um pool somente-leitura para algo que escreve é um erro de compilação em vez de um erro de runtime. Processadores só montavam quando o pool de escrita existia. Testes passavam.

Toda escrita de loja que ele fizesse em produção seria rejeitada, e havia quatro razões para isso simultaneamente. Cada uma era suficiente por si só.

**Uma: a função nunca era chamada.** `db.ConnectWrite` existia, era testada, e tinha exatamente duas referências — sua declaração e seu teste. Nada no caminho de startup a invocava. O pool de escrita que todo processador exigia nunca era construído.

**Duas: o hostname apontava para uma réplica.** O host de banco configurado era o endpoint *reader* do Aurora. Correto para o pool de leitura, que é limitado e roda `default_transaction_read_only = on` de propósito. Mas a DSN de escrita era construída a partir do mesmo campo de configuração, então o pool de escrita — uma vez construído — conectaria numa réplica somente-leitura e falharia uma camada abaixo com o mesmo erro.

**Três: a role de escrita não estava configurada.** A role do Postgres existia. Seu secret existia. Os dois existiam havia fases. Nada na task definition referenciava nenhum deles.

**Quatro: o deploy copiava a task definition antiga para frente.** O workflow lê a task definition em execução e adiciona a ela, então qualquer coisa que o Terraform introduza nunca chega ao serviço. O Terraform criou a revisão 22; o serviço em execução ficou na 21, porque o serviço ECS carrega `ignore_changes = [task_definition]`. Os dois são individualmente razoáveis. Juntos significam que um `terraform apply` correto não muda nada sobre o que o processo lê.

Quatro mecanismos, um sintoma: `cannot execute INSERT in a read-only transaction`, ou seu equivalente uma camada acima, ou nada porque o processador se recusou a montar.

**Um sintoma que quatro causas produzem não é uma pista.** Corrija uma e o sistema se comporta identicamente, o que se lê como "não era isso" e te manda para outro lugar. Você não recebe sinal nenhum de que estava certo. O único caminho é enumerar causas candidatas antes de corrigir qualquer uma, e esperar que a lista tenha mais de uma entrada.

Note onde as quatro vivem: em fronteiras entre artefatos que são cada um correto. O construtor do pool e seu call site estão em arquivos diferentes e os testes do construtor passam. O hostname está certo para um consumidor e errado para outro, e é um campo. A role existe em dois lugares e um terceiro não a menciona. O estado do Terraform está correto, o estado do serviço está correto, e a discordância entre eles é o bug.

Três das quatro foram encontradas consultando o serviço vivo, não lendo o repositório.

## O que tornou isso depurável

A correção que importou foi barata, e eu a buscaria primeiro na próxima vez: **fazer o log de startup nomear qual pré-condição está faltando, por componente.**

    entitlement applying is not configured; effects are derived and recorded only
    app store notification processing is not enabled  … credentials:true  write_pool:false
    google play notification processing is not enabled … credentials:false write_pool:false

Duas linhas, e elas transformam "o serviço não está processando nada" em "a App Store tem credenciais e nenhum pool de escrita; o Google Play não tem nenhum dos dois". Essa é a diferença entre um sintoma e uma localização.

Antes dessas linhas existirem, esse estado estava vivo por duas fases. Ninguém errou em não ver — o serviço estava de pé, os health checks passavam, e as duas filas estavam com profundidade 0, o que parece exatamente com "nenhum tráfego ainda" porque isso também era verdade.

A guarda em tempo de compilação de fato se pagou, e vale separar do resto. Gatear os processadores num pool de escrita não-nil significou que a falha foi *recusa em montar* em vez de um erro na primeira notificação real. Isso converte um incidente em produção numa mensagem de startup. Só faltava a mensagem dizer por quê.

## Um erro de permissão vestido de ausência

Duas fases posteriores subiram — revisadas, merjadas, deployadas, verdes — e o serviço em execução não tinha nada do que elas configuravam. Sua task definition não carregava credenciais de escrita, nenhuma configuração de identidade, e ainda carregava um valor que a segunda fase havia removido.

O workflow de deploy procura cada secret antes de injetá-lo:

```bash
arn=$(aws secretsmanager describe-secret --secret-id "$name" \
        --query ARN --output text 2>/dev/null || true)
if [ -z "$arn" ]; then
  echo "no $name secret; skipping"
else
  ...
fi
```

A role de deploy tinha `DescribeSecret` em dois ARNs de secret e em nenhum dos três que essas fases introduziram. Então a chamada retornou `AccessDenied`, `2>/dev/null` descartou a mensagem, `|| true` descartou o status de saída, e a variável voltou vazia — o que o script reporta, de boa fé, como *o secret não existe.*

Os secrets existiam. O Terraform tinha criado eles. O workflow imprimiu uma frase dizendo o contrário, em verde, e o deploy teve sucesso.

Duas falhas distintas estão sendo conflacionadas aqui, e elas pedem respostas opostas. **O secret está genuinamente ausente** — esperado em alguns ambientes, e pular é correto. **O secret existe e não podemos lê-lo** — uma má configuração da própria role de deploy, onde pular é a pior ação disponível, porque produz um serviço que inicia, passa seu health check, e não faz nada.

`2>/dev/null || true` colapsa as duas numa. A forma é sedutora: se lê como programação defensiva, como se você tivesse pensado sobre falha. O que ela de fato diz é "eu não me importo por que isso falhou", que é uma afirmação muito mais forte do que o autor pretende.

Existe uma segunda camada específica de APIs de nuvem. Vários serviços deliberadamente retornam "não encontrado" para "não permitido", para evitar vazar a existência de recursos a chamadores que não podem vê-los — `GetQueueUrl` reporta uma permissão faltante como `NonExistentQueue`. Então mesmo sem o redirecionamento, os dois casos não são distinguíveis com confiança pela resposta. Você tem que *decidir* qual vai assumir, e a suposição segura não é a conveniente.

Aqui está a parte que mudou como penso sobre isso. **O mesmo perigo estava documentado três seções acima do bug, no mesmo arquivo.** Um comentário no Terraform da role de deploy explica o comportamento do `GetQueueUrl`, nota que alguém foi pego por ele, e diz o que observar. Claro, correto, escrito por alguém que pagou pelo conhecimento. Os lookups de secret foram escritos com a mesma forma, meses depois, algumas dezenas de linhas abaixo. A nota não viajou.

Não leio isso mais como desatenção. Um comentário está disponível para um leitor que por acaso passa por ele, no humor de generalizar, no momento em que está escrevendo a coisa a que ele se aplica. Essa conjunção é rara. A documentação de um perigo fica num lugar; o perigo recorre onde quer que alguém escreva uma linha parecida. **Um comentário não é um controle** — é um registro de que alguém uma vez soube de algo.

Então a correção não foi outro comentário. O lookup agora captura o stderr e faz branch sobre o que a API disse: não-encontrado avisa e continua, qualquer outra coisa — `AccessDenied` incluído — falha o deploy ruidosamente e nomeia o secret. E um teste afirma que o conjunto de secrets que o workflow lê e o conjunto de ARNs que a policy de IAM permite são o mesmo conjunto. Duas listas, dois arquivos, duas linguagens, e nada além de um teste conectando elas.

A consequência foi nomeada de antemão porque é real: **o deploy agora falha onde antes avisava.** Um ambiente mal configurado para de deployar em vez de deployar errado. Eu faria essa troca sempre; a alternativa é o que tínhamos, que eram duas fases de trabalho que todos acreditavam estar no ar.

A mesma forma de engolir erro aparece em dois outros lookups naquele workflow. Deixei como estão, e registrei. Mudar quatro coisas ao mesmo tempo num script de deploy é como uma correção se torna um incidente.

## Quatro endpoints que passaram em todos os testes e responderam 404

O terceiro é o mais incômodo, porque o trabalho era genuinamente bom.

Uma fase entregou quatro endpoints de compra: formatos de requisição, gates de role, uma ordem de validação de sete passos, aplicação de entitlement, telemetria, uma tabela de mascaramento de erros. Um harness de paridade dirigiu o controller real do monolito contra o nosso sobre 300 casos — 300 concordaram, zero divergiram. Revisados, merjados, deployados.

Em staging, todos os quatro responderam **404**. Não 500, não 401 — a resposta de um serviço que nunca ouviu falar da rota. O endpoint de catálogo ao lado deles respondeu 401, que foi o controle que tornou isso certo: o router estava bem, a auth estava bem, aqueles quatro paths simplesmente não estavam montados.

Duas causas, e a segunda é a interessante. **A linha de wiring estava faltando** — o entry point forneceu uma opção de router, o catálogo, e o router monta um grupo apenas quando sua opção forneceu um handler. Opção ausente, nenhuma rota, nenhum erro. Razoável para subsistemas genuinamente opcionais, e exatamente o que tornou isso silencioso. E **não havia nada para fornecer**: cinco interfaces das quais os endpoints dependem não tinham implementação não-de-teste em nenhum lugar do repositório. Suas únicas referências eram a declaração e o call site. Adicionar a linha de wiring faltante não teria compilado. A lacuna não era uma linha esquecida, era uma camada faltando.

Por que nada pegou, em dobro:

**Nenhuma tarefa era dona do entry point.** Toda tarefa na fase listava os arquivos que tocaria, e todas as sete listas paravam no pacote de API, no pacote de purchase, no diretório de tools ou no de testes. `cmd/server/main.go` não aparece em nenhuma e o design nunca o atribuiu. O trabalho foi decomposto por *camada*, e wiring não é uma camada — é a costura entre as camadas e o processo, e não pertencia a ninguém.

**Todo teste construía seu próprio router.** Cada um constrói um router com stubs e dirige HTTP real por ele — o design correto para o que eles provam: que uma rota fica atrás do middleware de auth, que o gate de role rejeita, que o mapeamento de erro mascara. Uma chamada direta ao handler não poderia mostrar nada disso. E cada um passa identicamente independentemente de o entry point fornecer qualquer coisa, porque eles provam *este* router montado *deste* jeito. Produção roda um router diferente, montado em outro lugar, por código que nenhum teste olha.

**Um teste que constrói seu próprio sujeito não pode provar nada sobre como o sujeito é construído em produção.** Não é um teste fraco. É um teste de uma coisa diferente da que você assumiu.

A correção estrutural é uma função de montagem que tanto o entry point quanto os testes chamam, com a intenção de deployment explícita: uma superfície fornecida significa *sirva isso*, nil significa *este deployment não serve*. Dentro de uma superfície é tudo-ou-nada, porque metade de uma superfície é a falha sendo corrigida. Pedir uma superfície fornecendo metade das dependências agora é fatal no startup e nomeia a metade que falta. Note a mudança de pergunta — o router perguntava *este handler é nil?*, que tem resposta silenciosa; o startup agora pergunta *este deployment pediu isso?*, que tem resposta ruidosa.

Então a armadilha dentro da correção: **todo teste constrói seu próprio conjunto de superfícies também**, então todos ainda passam independentemente de o entry point popular uma. O bug tinha subido um nível, não ido embora. Então existe um teste que lê o *código-fonte* do entry point e afirma que ele chama a montagem compartilhada e não anexa opções de router diretamente.

É grosseiro. Afirmar sobre texto de código-fonte testa ortografia, não comportamento, e eu normalmente argumentaria contra. É também a única verificação na suíte que falha quando alguém reverte para montagem inline, e eu fiz mutation testing para ter certeza. Grosseiro e estrutural. Prefiro um teste feio que pega o que de fato aconteceu a um elegante que não consegue.

E o entry point agora loga quais superfícies montaram. A *ausência* dessa linha é o que deixou quatro endpoints mortos sobreviverem uma fase inteira. A verificação foi uma sonda de rotas contra o binário real iniciado a partir de configuração real — não um router construído por teste:

    /subscriptions/app-store/subscribe          404  ->  401
    /subscriptions/google-play/subscribe        404  ->  401
    /api/v1/subscriptions/{store}/subscribe     404  ->  401
    /subscriptions/app-store      (controle)    401  ->  401

401 é a condição de sucesso: montado, atrás da auth, rejeitando um chamador não autenticado. É tudo que se pode afirmar sem um token, e tudo que estava faltando.

## O que eu levaria disso

**Conte as fronteiras que um valor de configuração atravessa.** Se são nove, quatro falhas simultâneas não é azar — é a taxa base.

**Leia o sistema em execução, não o repositório.** Um repositório te diz o que deveria acontecer. Essa classe inteira de bug vive na diferença.

**Um sintoma que várias causas produzem não é uma pista.** Enumere antes de corrigir, porque corrigir uma não te dá feedback.

**Logue a pré-condição faltante por componente, por nome.** Não "not enabled" — `credentials:true write_pool:false`. Uma linha; a ausência dela escondeu duas destas três.

**Prefira recusar iniciar a falhar no primeiro uso.** Uma dependência nil que recusa montar é uma mensagem de startup. A mesma dependência usada uma vez é um incidente na frente de um usuário.

**`2>/dev/null || true` afirma que você não se importa por que falhou.** Em qualquer comando cuja falha signifique duas coisas diferentes, essa afirmação é falsa — e APIs de nuvem retornam "não encontrado" para "não permitido" de propósito.

**Um deploy verde que pulou algo é pior que um vermelho.** Os dois deixam o ambiente errado; só um te avisa.

**Transforme uma nota de perigo numa verificação executável.** O comentário naquele arquivo estava correto, pago, e não preveniu sua própria recorrência quarenta linhas depois.

**Um teste que constrói seu próprio sujeito prova o sujeito, não sua construção.** Os dois valem testar e são testes diferentes — então prove o último quilômetro contra o binário real.

**Decompor por camada deixa as costuras sem dono.** Se nenhuma tarefa nomeia o entry point, nada o muda e nada o verifica.
