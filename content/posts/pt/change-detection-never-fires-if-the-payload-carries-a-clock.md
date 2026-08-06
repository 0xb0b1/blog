---
title: "Sua Detecção de Mudança Nunca Dispara Se o Payload Carrega um Relógio"
date: "2026-08-03"
description: "Uma guarda comparava payloads JSON inteiros para evitar re-renders desnecessários. O payload incluía um timestamp gerado, então ela nunca coincidiu uma única vez — a interface se reconstruía a cada 2,5 segundos e te jogava de volta ao topo de qualquer documento que você estivesse lendo. Sobre campos voláteis, e por que condicionar um evento a 'mudou?' pode perder esse evento permanentemente."
tags:
  [
    "frontend",
    "debugging",
    "javascript",
    "ui",
    "observabilidade",
  ]
---

Tenho um pequeno dashboard que consulta um endpoint local a cada 2,5 segundos e re-renderiza. Ele tinha uma guarda contra trabalho inútil, escrita da forma óbvia:

```js
const raw = await res.text();
if (raw === lastRaw) return;      // nada mudou, pula o render
lastRaw = raw;
render(JSON.parse(raw));
```

Razoável. Barato. Errado desde o primeiro commit, e silenciosamente — porque a resposta do endpoint é assim:

```json
{ "generatedAt": "2026-08-01T00:58:01-04:00", "active": [...], "history": [...] }
```

`generatedAt` é um timestamp novo em cada requisição. A comparação nunca poderia coincidir. Aquela guarda existia desde o início do projeto e nunca havia retornado cedo uma única vez.

## O sintoma não parecia nada com isso

O bug chegou até mim como: *"quando tento rolar em qualquer página ela me manda automaticamente de volta ao começo e não consigo ler até o fim."*

Que é o que um re-render a cada 2,5 segundos faz com um documento rolado. O render substituía os filhos do painel de detalhe, e o painel do documento mostrava brevemente um placeholder `loading…` enquanto refazia o fetch — colapsando a altura do contêiner para quase nada, o que trava o offset de rolagem em zero. Quando o conteúdo voltava, a posição já tinha ido.

Então o sintoma relatado era comportamento de rolagem; o bug real era um predicado de detecção de mudança que sempre dizia "mudou"; e o mecanismo que conectava os dois era um colapso de altura que eu nunca teria adivinhado pela descrição.

## Confirmando, de forma barata

Três consultas, com hash do corpo cru e depois de uma versão com os campos voláteis removidos:

```
poll1  raw=79e69fea  signature=3d5491ba
poll2  raw=c550664c  signature=3d5491ba
poll3  raw=70708ad5  signature=3d5491ba
```

O digest cru difere sempre; a assinatura filtrada é idêntica. Esse é o bug inteiro em três linhas, e levou dois minutos para produzir. Vale fazer antes de tocar em qualquer código — caso contrário eu teria ido olhar handlers de scroll.

Havia três campos voláteis, não um:

- `generatedAt` — um timestamp do servidor por requisição
- `durationSec` — derivado de `agora - iniciadoEm`, então incrementa em toda consulta para qualquer item em execução
- `session.updatedAtMs` — um heartbeat, atualizado a cada poucos segundos

Cada um deles sozinho já bastaria para derrotar a comparação.

## A correção, e sua armadilha

Compare uma assinatura que exclui campos que mudam sem significar nada:

```js
const VOLATILE = new Set(["generatedAt", "durationSec", "updatedAtMs"]);
const signature = s => JSON.stringify(s, (k, v) => (VOLATILE.has(k) ? undefined : v));
```

O replacer do `JSON.stringify` roda em toda profundidade, então ocorrências aninhadas são tratadas sem você percorrer a árvore.

A armadilha é filtrar demais. Uma guarda que suprime mudanças reais é pior que nenhuma guarda, porque agora a interface mente em silêncio. Então verifiquei mutando um payload real e afirmando quais mutações deveriam disparar um render:

```
skip    apenas voláteis (generatedAt + durationSec + heartbeat)
RENDER  avanço de estágio · nota adicionada · decisão pendente aparece
RENDER  sessão fica ociosa · sessão morre · progresso de tarefa · item novo
```

Esse é o teste que eu exigiria para esse tipo de predicado. É fácil verificar que o caso ruidoso é suprimido e esquecer de verificar que os casos significativos não são.

Também mantive o rótulo "atualizado HH:MM:SS" se renovando em toda consulta, fora da guarda. É a escrita de um nó de texto sem consequência de layout, e sem isso um dashboard funcionando parece congelado.

## A segunda lição, que custou mais

O mesmo padrão me pegou de novo em outro componente, e esse é o ponto de design mais interessante.

Em outro lugar do sistema, transições de estágio são espelhadas para um tracker externo. A implementação óbvia é disparar quando o estágio *muda*:

```js
if (stage_before !== stage_after) { mirror(stage_after); }
```

Escrevi isso, e então testei o que acontece quando o tracker está inacessível. A escrita é best-effort por design — uma indisponibilidade nunca deve derrubar uma execução — então ela avisa e continua. O que significa: com o tracker fora no momento da transição, aquele marco é perdido **permanentemente**. O estágio nunca muda de novo, então a condição nunca vale de novo, e nada nunca tenta novamente.

A correção foi parar de condicionar à mudança e condicionar apenas a um registro do que já foi enviado:

```js
if (!alreadyPosted(event)) { if (mirror(event)) markPosted(event); }
```

Agora toda escrita subsequente de qualquer tipo é uma oportunidade de retry, e o registro previne duplicatas. Verificado desligando o tracker durante duas escritas de estágio, restaurando, e confirmando que uma atualização de campo não relacionada recuperou os dois marcos perdidos.

Existe uma versão sutil do mesmo erro que cometi duas vezes antes de acertar: um evento condicionado a "o campo de ticket acabou de ficar preenchido" tem exatamente o mesmo defeito que um condicionado a "o estágio acabou de mudar". Ambos são disparados por borda, e uma borda que você perde não volta. **Disparo por nível com um registro de idempotência sobrevive a uma indisponibilidade; disparo por borda não.**

## O que eu levaria disso

**Compare significado, não bytes.** Qualquer payload com um timestamp de servidor, uma duração calculada ou um heartbeat não pode ser comparado por inteiro. Se você tem uma guarda assim, verifique se ela já disparou alguma vez — a minha não havia, pela vida inteira do projeto, e nada iria me contar.

**Depois verifique que ela não dispara demais.** Um detector de mudança precisa de testes nas duas direções.

**Prefira disparo por nível.** "Isso já foi feito?" sobrevive a falhas. "Isso acabou de mudar?" não. O registro são algumas linhas e transforma um marco perdido em um marco atrasado.

E o sintoma raramente é o bug. O relato era sobre rolagem. A causa era uma comparação de string contra um timestamp, três componentes de distância.
