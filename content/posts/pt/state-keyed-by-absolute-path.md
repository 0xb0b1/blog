---
title: "Quando o Estado É Indexado por Caminho Absoluto, Sincronizar Arquivos Não Basta"
date: "2026-07-31"
description: "Uma ferramenta que indexa estado por projeto usando o diretório de trabalho absoluto. Copie os arquivos para uma segunda máquina com um home diferente e você não funde o histórico — você acumula cópias paralelas dele. Um repositório tinha três. Sobre identificadores derivados de fatos do ambiente, e por que fazer os ambientes concordarem vence traduzir para sempre."
tags:
  [
    "ferramentas",
    "experiencia-do-desenvolvedor",
    "sincronizacao",
    "unix",
    "portabilidade",
  ]
---

Trabalho nos mesmos projetos de um desktop Linux e de um MacBook. Minha configuração de editor, shell e dotfiles me seguem entre máquinas há anos através de um repositório git, e eu presumi que estender isso ao estado local de uma ferramenta era questão de escolher um mecanismo de sincronização.

Então listei o diretório de estado e encontrei isto:

```
-home-vicente-github-r10-r10-hub
-Users-paulovicente-github-r10-r10-hub
-Users-paulovicente-github-r10-hub
```

Três diretórios. Um repositório. Dois deles na mesma máquina.

## O mecanismo

A ferramenta indexa estado por projeto usando o diretório de trabalho absoluto de onde foi lançada, achatado num nome de diretório. `/home/vicente/github/r10/r10-hub` se torna `-home-vicente-github-r10-r10-hub`.

Esse é um design razoável. Ela precisa de um identificador estável por projeto, não pode depender de o projeto ser um repositório git, e o diretório de trabalho está ali. É também a coisa que torna o estado não portável, porque o identificador codifica um fato sobre a máquina — onde fica seu diretório home.

O Linux me dá `/home/vicente`. O macOS me dá `/Users/paulovicente`. Então o mesmo projeto tem duas identidades, e copiar arquivos entre máquinas produz as duas lado a lado em vez de um histórico fundido. O terceiro diretório foi culpa minha: eu havia clonado o mesmo repositório em `~/github/r10-hub` numa máquina e em `~/github/r10/r10-hub` na outra, dividindo o histórico novamente sem envolvimento nenhum do sistema operacional.

A falha é silenciosa. Nada dá erro. A ferramenta funciona perfeitamente nas duas máquinas. Você só não consegue retomar a sessão de ontem, porque no que diz respeito à ferramenta ontem aconteceu num projeto diferente.

## Duas saídas

**Traduzir em cada sincronização.** Reescrever os nomes de diretório, e os caminhos absolutos dentro dos arquivos, cada vez que o estado se move. Funciona, e é o que acabei fazendo para a transferência pontual.

**Fazer os ambientes concordarem.** Escolher um caminho canônico e torná-lo válido nas duas máquinas, de forma que o identificador seja o mesmo em todo lugar e nenhuma tradução seja necessária.

A segunda é melhor, e uma direção é muito mais barata que a outra. No Linux, `/Users` não é usado, então:

```bash
sudo mkdir -p /Users && sudo ln -s /home/vicente /Users/paulovicente
```

Agora `/Users/paulovicente/github/r10/r10-hub` resolve nas duas máquinas, e se eu habitualmente fizer `cd` para lá, toda sessão escreve num único diretório independentemente de qual máquina eu esteja. O inverso — criar `/home` no macOS — exige `/etc/synthetic.conf` e briga com o autofs, que já reivindica esse caminho. Possível, mais frágil, e não há razão para escolher a direção mais difícil.

Essa é a forma geral: quando um identificador é derivado de um fato específico do ambiente, você pode traduzir para sempre ou normalizar o fato uma vez. Tradução precisa ser reexecutada e pode ser esquecida; normalização é um custo único que segue rendendo.

## A transferência pontual, e o que ela ensinou

Eu ainda precisava mover o estado existente, então empacotei. Isso trouxe à superfície três categorias em que eu não havia pensado.

**Renomear não basta por si só.** Os nomes de diretório são o índice, mas caminhos absolutos também estão embutidos *dentro* dos arquivos — manifestos de execução registrando um caminho de projeto, configuração listando raízes de repositório, transcripts registrando um diretório de trabalho. 31 arquivos precisaram de reescrita. O que levantou a questão do que *não* reescrever: arquivos de objeto do `.git` contêm caminhos em blobs comprimidos, e um busca-e-substitui cego corrompe o repositório. Pular o `.git/` inteiramente foi a correção.

**Parte do estado nasce na máquina e não deve viajar.** Esta é a lista que eu escreveria antes de empacotar qualquer coisa:

- Um registro de sessões vivas com PIDs. Copie e a outra máquina acredita em processos que não existem lá.
- Snapshots de shell capturados do shell local. Formato errado num sistema operacional diferente.
- Um binário compilado. O meu é ELF; o MacBook é arm64. Existe um arquivo marcador registrando `uname -sm` ao lado dele precisamente para que um binário velho seja refeito, e os dois não pertencem a lugar algum perto do arquivo compactado.
- Credenciais atreladas ao dispositivo. Copiar um token de autenticação fixado a um dispositivo produz um estado confuso meio-funcionando; fazer login de novo leva dez segundos.
- Caches e árvores de plugin. Regenerados, e eram 40% dos bytes.

Construí o pacote como uma **lista de permissão**, não como tudo-menos-exclusões. É mais seguro esquecer de incluir algo do que esquecer de excluir uma chave privada.

**Arquivos de configuração versionados não devem conter caminhos absolutos.** Minha reescrita de caminhos modificou dois arquivos que estavam versionados no git. Isso teria deixado a segunda máquina com uma árvore de trabalho permanentemente suja e um conflito no próximo pull.

A correção real não foi reescrever melhor — foi tornar os arquivos agnósticos de máquina. Uma configuração listando `["/home/vicente/github/r10"]` se tornou `["~/github/r10"]`, com o consumidor expandindo o `~` na leitura:

```bash
roots="$(jq -r '.repos[]? // empty' "$CONFIG" | sed "s|^~|$HOME|")"
```

Um arquivo agora serve as duas máquinas. Se você sincroniza configuração via git e um arquivo precisa de conteúdo diferente por máquina, isso é um cheiro de design no arquivo, não um problema para resolver na camada de sincronização.

## Sobre mecanismo: git é a ferramenta errada para essa metade

Configuração via git funciona bem: pequena, pouca rotatividade, e o histórico é genuinamente útil.

Estado é o oposto. O meu são 26MB de transcripts de sessão apenas-append, um arquivo por sessão. Cada commit guarda um novo blob de um arquivo que cresce, então o repositório ganharia dezenas de megabytes por semana de histórico que ninguém vai ler. E duas máquinas escrevendo num `history.jsonl` compartilhado produz conflitos de merge num arquivo que você nunca iria querer resolver à mão.

A propriedade que torna sincronização contínua de arquivos viável aqui é que **cada sessão escreve seu próprio arquivo de nome único**. Duas máquinas quase nunca tocam o mesmo arquivo, então o risco de conflito que normalmente mata essa abordagem em grande parte não se aplica. Vale verificar isso antes de escolher um mecanismo — é uma propriedade do layout dos dados, não da ferramenta de sincronização.

## O que eu levaria disso

**Verifique de que seus identificadores são feitos.** Qualquer coisa derivada de um hostname, um diretório home, um ponto de montagem ou um nome de usuário é local à máquina, por mais estável que pareça. Isso é irrelevante até o estado precisar se mover.

**Prefira normalizar o ambiente a traduzir os dados.** Um symlink é menor que um script de migração, e não pode ser esquecido.

**Empacote com uma lista de permissão e saiba o que nasce na máquina.** PIDs vivos, binários compilados e credenciais de dispositivo todos se parecem com arquivos comuns.

**Um arquivo que precisa de conteúdo diferente em máquinas diferentes não deveria ser versionado.** Ou o torne independente de caminho, ou não o sincronize. Reescrever arquivos versionados na hora da transferência troca um problema por um permanente.
