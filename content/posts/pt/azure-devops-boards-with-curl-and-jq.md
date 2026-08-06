---
title: "Azure DevOps Boards com Nada Além de curl e jq"
date: "2026-08-04"
description: "Um cliente completo de Boards — buscar, criar, transicionar, linkar, reparentar, WIQL — em um único script bash sem SDK. A maior parte do post são as quatro falhas que o moldaram: um problema de permissão que chega como HTTP 500, pais que se marcam como seu trabalho, uma flag que órfã cartões em silêncio, e dois tokens de tamanho idêntico onde um estava revogado."
tags:
  [
    "azure-devops",
    "bash",
    "api",
    "automacao",
    "ferramentas",
  ]
---

Eu precisava que um fluxo de trabalho lesse um work item, criasse Stories e Tasks, movesse cartões pelo board e anexasse links de PR. Os caminhos óbvios são o CLI `az` ou um SDK de linguagem. Eu não queria nenhum dos dois: quem chama é um script bash, precisa funcionar headless, e adicionar uma dependência Python a uma toolchain de shell para fazer quatro chamadas HTTP é uma troca ruim.

A API REST é tranquila de usar diretamente. `curl` para transporte, `jq` tanto para parsing quanto para construir requisições, cerca de 200 linhas. O que segue são principalmente as quatro coisas que deram errado, porque são as partes que você não lê na documentação.

## A forma

Um script, um subcomando por operação:

```bash
azure-workitem.sh fetch 6758                    # JSON compacto: tipo, estado, título, AC, repro
azure-workitem.sh create "User Story" "titulo" --parent 6760 --description "<p>…</p>"
azure-workitem.sh transition 6758 "Code Review"
azure-workitem.sh link 6758 "<url-do-pr>" "PR #42"
azure-workitem.sh parent 6793 6792
azure-workitem.sh wiql 'SELECT [System.Id] FROM WorkItems WHERE …'
```

Duas decisões estruturais se pagaram. **Toda escrita passa por uma única função**, então o `--dry-run` é imposto em um lugar em vez de por subcomando. E **corpos de requisição são construídos com `jq`, nunca por interpolação de string** — o formato JSON Patch que o Azure quer é chato, e títulos contêm aspas e travessões:

```bash
body="$(jq -n --arg title "$title" --arg assign "$assign" '
  [ {op:"add", path:"/fields/System.Title", value:$title} ]
  + (if $assign != "" then [{op:"add", path:"/fields/System.AssignedTo", value:$assign}] else [] end)')"
```

Nomes de projeto contêm espaços, então codifique-os em vez de escapar à mão: `jq -rn --arg s "$PROJECT" '$s|@uri'`. Isso permite que o arquivo de configuração guarde `R10 Score Development` como está escrito.

## Falha 1: um problema de permissão chega como HTTP 500

Meu primeiro `create` retornou isto, sem corpo:

```
curl: (22) The requested URL returned error: 500
```

Um 500 te manda procurar uma requisição malformada. Passei um tempo verificando o documento de patch. A mensagem real estava lá quando removi o `curl -f`, que suprime o corpo da resposta em status de erro:

```
VS403410: You don't have suppress notifications permission.
```

Eu vinha anexando `suppressNotifications=true` em toda escrita, na teoria razoável de que um script espelhando progresso não deveria mandar e-mail ao time em cada transição. Esse parâmetro exige uma permissão de **nível de coleção** que minha conta não tem — e a resposta do Azure para usá-lo sem permissão não é um 403 no parâmetro. Ele derruba a escrita inteira, como um 500.

Duas coisas a levar disso. **Remova o `-f` durante o desenvolvimento**, ou você joga fora a única parte útil de uma resposta de erro. E desconfie de um 500 numa requisição que você nunca fez com sucesso: pode ser um problema de permissão vestido de erro de servidor.

O parâmetro sumiu. Notificações seguem as regras normais do projeto, que é o custo de não ser admin da coleção.

## Falha 2: pais se marcam como seu trabalho

Construí uma consulta para "work items que toquei hoje" para alimentar uma planilha de horas:

```sql
SELECT [System.Id] FROM WorkItems
WHERE [System.ChangedBy] = 'eu@exemplo.com'
  AND [System.ChangedDate] >= '2026-08-04'
```

`ChangedBy` em vez de `AssignedTo`, deliberadamente — um cartão que outra pessoa move não é meu dia de trabalho.

A consulta retornou onze itens. Quatro eram Features e um era um Epic que eu nunca havia aberto. Estavam lá porque **criar um filho atualiza o `ChangedDate` do pai**, e o `ChangedBy` do pai se torna quem criou o filho. Eu havia criado cartões de Story e Task sob eles naquela manhã, então todo contêiner acima aparecia como trabalho que eu pessoalmente havia feito.

A consulta agora exclui tipos contêineres de forma explícita:

```sql
AND [System.WorkItemType] NOT IN ('Task','Feature','Epic','Iniciativa')
```

`Task` é excluído por um motivo diferente — um dia de trabalho orientado a spec toca uma dúzia deles sob uma única Story, o que é granular demais para uma planilha que um gestor lê. Isso levou o dia de 26 atividades para 7.

## Falha 3: um valor de flag vazio que órfã em silêncio

Criei dois work items com `--parent "$FEAT"`, e os dois voltaram com aparência perfeita: tipo certo, responsável certo, tags certas, estado certo. Os dois eram órfãos.

`$FEAT` estava vazio — definido em uma invocação anterior do shell, e estado de shell não persiste entre elas. Então a chamada era `--parent ""`, e meu tratamento de argumentos fazia isto:

```bash
--parent) parent="${2:-}"; shift 2 ;;
```

Vazio é um valor válido, então nenhum erro. E adiante, a relação de pai só era anexada quando o valor era não vazio — uma guarda estilo `// empty` que transformou "você não me deu nada" em "você não pediu um pai".

A resposta do create não deu pista alguma, porque da perspectiva da API nada estava errado. Só descobri listando os filhos do pai e recebendo zero.

```bash
--parent) parent="${2:-}"; [ -n "$parent" ] || die "--parent recebeu valor vazio"; shift 2 ;;
```

A regra geral que eu escreveria na parede: **uma flag opcional que foi explicitamente passada com valor vazio é um bug, não uma omissão.** Distinga "ausente" de "presente mas vazio" sempre que a diferença for silenciosa.

Também adicionei um subcomando `parent <id> <idDoPai>`, já que reparar os dois órfãos de outra forma significava construir um patch `/relations/-` à mão.

## Falha 4: dois tokens, mesmo tamanho, um revogado

As credenciais vinham de uma variável de ambiente primeiro, depois do cofre do sistema operacional. Ordem sensata — env para CI, keyring para uso interativo.

Toda chamada começou a retornar 401. O token no ambiente e o token no keyring tinham ambos 84 caracteres. Só um funcionava:

```
keyring  len=84  http=200  ✓
env      len=84  http=401  ✗
valores DIFEREM
```

Um token velho num perfil de shell havia sobrevivido ao bom que estava no keyring. Como o env era verificado primeiro, o morto sombreava o que funcionava, e a falha parecia exatamente um problema de permissão.

A ordem agora é cofre primeiro, ambiente como fallback:

```bash
PAT="$(secret-tool lookup service azure-devops-pat 2>/dev/null || true)"
PAT="${PAT:-${AZDO_PAT:-${AZURE_DEVOPS_EXT_PAT:-}}}"
```

Execuções headless não têm keyring, então o env ainda ganha onde é necessário. E uma entrada velha de perfil não consegue mais sombrear a credencial que você mantém ativamente.

## Coisas pequenas que ajudaram

**Valide nomes de estado contra o tipo antes de patchar.** Nosso board tem 19 estados para uma User Story e um conjunto diferente para uma Task. Um `System.State` inválido retorna um 400 que se lê como falha de autenticação, então o script busca os estados permitidos do tipo primeiro e, em caso de erro, os imprime:

```
azure-workitem: 'Estado Inexistente' não é um estado de User Story. Válidos:
  New
  Business Refinement
  …
```

**Remova HTML na leitura.** `System.Description` e `AcceptanceCriteria` são rich text. Um filtro jq que remove tags e decodifica as entidades comuns os torna usáveis como entrada de spec.

**`--dry-run` em toda escrita, canalizado por uma única função.** Essa é a que eu exigiria, e também é onde me queimei: uma adição posterior chamou o cliente da API diretamente em vez de passar por esse canal, o que significa que a flag de dry-run não se aplicava a ela. Criou três cartões reais num board compartilhado durante um teste. Uma flag de segurança imposta por local de chamada é uma convenção, não uma garantia — deveria existir exatamente um lugar de onde uma requisição pode sair.
