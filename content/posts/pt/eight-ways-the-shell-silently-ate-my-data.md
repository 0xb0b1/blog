---
title: "Oito Formas do Shell Comer Meus Dados em Silêncio"
date: "2026-08-01"
description: "Cada uma delas produziu nenhum erro, nenhum aviso, e um resultado errado de aparência plausível. Coletadas de um único dia escrevendo cola em bash, jq e git: heredocs que roubam o stdin, tab como espaço em branco do IFS, bytes NUL desaparecendo em substituição de comando, o ponto do jq mudando de contexto depois de um pipe, e quatro outras."
tags:
  [
    "bash",
    "shell",
    "jq",
    "zsh",
    "debugging",
    "ferramentas",
  ]
---

Passei um dia escrevendo cola — bash chamando `curl`, encanando em `jq`, lendo `git log`, saindo para Python para o parsing que bash não deveria fazer. Trabalho comum. No fim do dia eu tinha oito bugs distintos, e o que eles tinham em comum é o que vale registrar: **nenhum deles produziu um erro.**

Nenhum código de saída diferente de zero, nenhum aviso em stderr, nenhum crash. Cada um produziu um resultado que parecia inteiramente razoável e estava errado. Esse é o perigo específico da cola em shell: o modo de falha é silencioso, e a saída é plausível o suficiente para ir para produção.

Aqui estão eles, com o sintoma primeiro, porque é assim que você vai encontrá-los.

## 1. Um heredoc rouba o stdin do programa que ele alimenta

**Sintoma.** Um pipeline não produz nada. Sem erro. O script Python no fim aparentemente roda e emite um resultado vazio.

```bash
collect_activities | python3 - <<'PY'
import sys
for line in sys.stdin:      # nunca vê uma única linha
    print(line.strip())
PY
```

**Causa.** `python3 -` significa *leia o programa do stdin*. O heredoc está ligado ao stdin, então o Python lê seu próprio código-fonte dali e chega ao EOF. O pipe à esquerda é descartado inteiramente.

**Correção.** Coloque o programa em outro descritor e deixe o stdin para os dados:

```bash
collect_activities | python3 /dev/fd/3 3<<'PY'
```

Esse me custou duas vezes, porque bati nele novamente em um comando de verificação improvisado uma hora depois de corrigi-lo no script.

## 2. Tab é espaço em branco do *IFS*, então campos vazios iniciais colapsam

**Sintoma.** Todo campo desloca uma posição para a esquerda, mas só para registros cujo primeiro campo é vazio. Uma execução sem ticket vinculado reportava seu slug interno onde deveria haver uma descrição legível.

```bash
# a linha é: "\tAlguma descrição\tfeature-slug"
while IFS=$'\t' read -r id desc slug; do
  # id="Alguma descrição", desc="feature-slug", slug=""
```

**Causa.** Espaço, tab e newline são *espaço em branco do IFS*, que o bash trata de forma especial: sequências no início e no fim são removidas e ocorrências consecutivas colapsam em uma. Definir `IFS=$'\t'` não desativa esse comportamento, porque tab continua sendo espaço em branco.

**Correção.** Use um delimitador que não seja espaço em branco. O separador de unidade existe para isso:

```bash
while IFS=$'\x1f' read -r id desc slug; do
```

## 3. Substituição de comando remove bytes NUL

**Sintoma.** Um aviso que você seria perdoado por ignorar — `command substitution: ignored null byte in input` — e uma variável sem seu delimitador.

Eu havia usado `\0` para separar dois valores em uma única string capturada, com o raciocínio de que nenhum conteúdo real o conteria. Correto, e inútil: `$( )` descarta bytes NUL, então o delimitador era justamente a única coisa garantidamente incapaz de sobreviver.

**Correção.** Não invente delimitadores para dados estruturados. Emita JSON e deixe o `jq` ler de volta:

```bash
merged="$(build | python3 …)"     # imprime {"value": "...", "added": [...]}
value="$(jq -r .value <<<"$merged")"
```

Isso também corrigiu a segunda metade do bug, que era `grep '^\x00'` — não é uma regex básica válida, e o `grep` diz isso com um aviso ameno sobre "stray \ before x" em vez de falhar.

## 4. O `-e` do jq reflete o valor, não a validade

**Sintoma.** Um helper rejeitou `null` como JSON inválido. `null` é JSON válido, e no meu caso era o valor significativo — limpar um campo.

```bash
echo "$val" | jq -e . >/dev/null || die "JSON inválido: $val"
```

**Causa.** `-e` define o código de saída a partir da *saída*: `null` e `false` dão exit 1. Não é uma verificação de sintaxe.

**Correção.** `jq empty` valida a sintaxe e não emite nada:

```bash
echo "$val" | jq empty >/dev/null 2>&1 || die "JSON inválido: $val"
```

## 5. No jq, o `.` muda de contexto depois de um pipe

**Sintoma.** `jq: error: Cannot index array with string ("stage")`, de uma expressão que se lê perfeitamente bem.

```jq
sort_by([ (["done","failed"] | index(.stage)) != null, .updatedAt ])
```

**Causa.** Dentro de `["done","failed"] | index(.stage)`, o ponto já mudou para aquele array literal. `.stage` está pedindo a um array uma chave de string.

**Correção.** Vincule antes do pipe:

```jq
sort_by([ ((.stage // "") as $s | (["done","failed"] | index($s))) != null, .updatedAt ])
```

O que tornou esse caro foi que o shell ao redor tinha `2>/dev/null || true` na chamada, então o erro nunca apareceu. A função simplesmente retornava vazio e tudo adiante tratava isso como "nenhuma correspondência encontrada". Uma expressão idêntica em outro lugar da mesma base de código estava correta, e é por isso que eu não havia suspeitado do padrão.

## 6. `@csv` coloca strings entre aspas, e APIs rejeitam as aspas

**Sintoma.** HTTP 400 de uma requisição cujo parâmetro de ids parecia correto.

```jq
[.relations[] | (.url | split("/") | last)] | @csv    # → "6764","6765"
```

**Causa.** `@csv` coloca valores de string entre aspas, corretamente, porque é o que CSV exige. A API queria `6764,6765`. Uma chamada quase idêntica em outro lugar funcionava, porque ali os ids saíam do JSON como *números*, e `@csv` não coloca números entre aspas.

**Correção.** `join(",")` quando você quer uma lista nua — ou converta para números primeiro se você especificamente quer o escaping do `@csv`.

## 7. zsh lê `:x` depois de um `$var` nu como um modificador

**Sintoma.** Três chamadas `curl` idênticas em um laço retornam 404. A mesma URL, colada à mão, funciona.

```bash
for r in 3 5 6; do
  curl ... "https://…/values/Sheet%21L$r:clear"
done
```

**Causa.** zsh suporta modificadores de estilo histórico em expansão de parâmetro, e `$r:clear` é interpretado como `$r` seguido de um modificador, em vez de `$r` seguido de um literal `:clear`.

**Correção.** Use chaves na expansão — `${r}:clear`. Vale saber se você escreve scripts com `#!/usr/bin/env bash` mas os cola em um zsh interativo, que é exatamente o desencontro em que eu estava.

Quero destacar o erro de diagnóstico que cometi aqui, já que é mais instrutivo que o bug: vi o 404 e presumi que a codificação de URL do `!` era a culpada, porque é o caractere de aparência interessante. Gastei duas tentativas codificando e recodificando. O caractere que estava de fato quebrado era o sem graça.

## 8. `git log --all` inclui refs/stash

**Sintoma.** Um relatório de "no que trabalhei hoje" contendo `WIP on main: 0685722` e `index on main: 0685722`.

**Causa.** `--all` significa todas as refs, e `refs/stash` é uma ref. Todo stash que você já fez é um commit com um assunto gerado, e ele cai na saída parecendo trabalho.

**Correção.** Seja explícito sobre o que você quer:

```bash
git log --branches --remotes --no-merges --author="$email" --since=…
```

`--no-merges` entra por um motivo relacionado: `Merge pull request #48 from …` é processo, não atividade, e estava em maior número que os assuntos reais.

## O padrão

Sete dos oito foram causados por uma ferramenta fazendo algo razoável sobre o qual eu não havia perguntado. `@csv` coloca aspas porque CSV precisa de aspas. `-e` reporta veracidade porque é seu trabalho documentado. Tab colapsa porque tab é espaço em branco. Nada disso é bug da ferramenta.

A única lição que eu de fato generalizaria é sobre os diagnósticos, não sobre as ferramentas. Dois desses foram caros puramente porque um erro estava sendo engolido — `2>/dev/null || true` numa chamada de jq, e um `head -1` na saída capturada que escondia tudo depois da primeira linha. As duas supressões eram deliberadas, e as duas eram razoáveis isoladamente: eu não queria um aviso ruidoso derrubando uma execução.

Se você vai descartar o stderr de uma ferramenta, descarte no ponto em que você já decidiu que a falha é sobrevivível — e garanta que algo ainda diga *que uma falha aconteceu*. Um resultado vazio e um erro silenciado são indistinguíveis de um resultado legitimamente vazio, e você vai gastar uma hora nessa diferença.
