---
title: "Começando com Go"
date: 2025-01-15
description: "Um guia para iniciantes sobre a linguagem de programação Go e seu ecossistema."
tags: ["golang", "iniciante", "tutorial"]
---

Go (também conhecido como Golang) é uma linguagem de programação de código aberto criada pelo Google em 2009. Ela foi projetada para ser simples, eficiente e fácil de aprender, tornando-a uma excelente escolha tanto para iniciantes quanto para desenvolvedores experientes.

## Por que Go?

Go oferece várias vantagens que o tornaram cada vez mais popular:

1. **Simplicidade**: Go tem uma sintaxe limpa e mínima que é fácil de ler e escrever
2. **Performance**: Código compilado que roda quase tão rápido quanto C
3. **Concorrência**: Suporte integrado para programação concorrente com goroutines e channels
4. **Tipagem Estática**: Detecta erros em tempo de compilação, não em produção
5. **Biblioteca Padrão Excelente**: Rica coleção de pacotes para tarefas comuns
6. **Ferramentas**: Excelentes ferramentas integradas para formatação, testes e construção

## Instalando Go

Para começar, baixe e instale Go do [site oficial](https://golang.org/dl/). Após a instalação, verifique se está funcionando:

```bash
go version
```

Você deve ver algo como:

```
go version go1.21.0 linux/amd64
```

## Seu Primeiro Programa em Go

Vamos criar o tradicional programa "Hello, World!":

```go
package main

import "fmt"

func main() {
    fmt.Println("Olá, Mundo!")
}
```

Salve isso como `main.go` e execute:

```bash
go run main.go
```

## Entendendo a Estrutura

Vamos analisar o que cada parte faz:

- `package main`: Declara que este é o pacote principal (ponto de entrada do programa)
- `import "fmt"`: Importa o pacote de formatação para saída de texto
- `func main()`: A função principal onde a execução do programa começa
- `fmt.Println()`: Imprime texto no console

## Variáveis e Tipos

Go é estaticamente tipado, mas tem inferência de tipos:

```go
package main

import "fmt"

func main() {
    // Declaração explícita de tipo
    var nome string = "Paulo"
    var idade int = 30

    // Declaração curta (inferência de tipo)
    cidade := "São Paulo"
    ativo := true

    fmt.Printf("Nome: %s, Idade: %d\n", nome, idade)
    fmt.Printf("Cidade: %s, Ativo: %t\n", cidade, ativo)
}
```

## Tipos Básicos

Go tem vários tipos básicos:

- **Inteiros**: `int`, `int8`, `int16`, `int32`, `int64`
- **Inteiros sem sinal**: `uint`, `uint8`, `uint16`, `uint32`, `uint64`
- **Ponto flutuante**: `float32`, `float64`
- **Booleano**: `bool`
- **String**: `string`
- **Byte**: `byte` (alias para uint8)
- **Runa**: `rune` (alias para int32, representa um ponto de código Unicode)

## Funções

Funções em Go são cidadãos de primeira classe:

```go
package main

import "fmt"

// Função simples
func saudacao(nome string) string {
    return "Olá, " + nome + "!"
}

// Função com múltiplos retornos
func dividir(a, b float64) (float64, error) {
    if b == 0 {
        return 0, fmt.Errorf("divisão por zero")
    }
    return a / b, nil
}

func main() {
    msg := saudacao("Mundo")
    fmt.Println(msg)

    resultado, err := dividir(10, 2)
    if err != nil {
        fmt.Println("Erro:", err)
    } else {
        fmt.Println("Resultado:", resultado)
    }
}
```

## Estruturas de Controle

### If/Else

```go
if x > 10 {
    fmt.Println("x é maior que 10")
} else if x == 10 {
    fmt.Println("x é igual a 10")
} else {
    fmt.Println("x é menor que 10")
}
```

### For Loop

Go tem apenas um tipo de loop - `for`:

```go
// Loop tradicional
for i := 0; i < 5; i++ {
    fmt.Println(i)
}

// While-style
for condição {
    // código
}

// Loop infinito
for {
    // código (use break para sair)
}

// Range sobre slices/maps
numeros := []int{1, 2, 3, 4, 5}
for índice, valor := range numeros {
    fmt.Printf("Índice: %d, Valor: %d\n", índice, valor)
}
```

## Slices e Maps

### Slices

Slices são arrays dinâmicos:

```go
// Criando slices
numeros := []int{1, 2, 3, 4, 5}
vazio := make([]string, 0)

// Adicionando elementos
numeros = append(numeros, 6, 7)

// Fatiando
primeiros := numeros[:3]  // [1, 2, 3]
ultimos := numeros[3:]    // [4, 5, 6, 7]
```

### Maps

Maps são dicionários chave-valor:

```go
// Criando maps
idades := map[string]int{
    "Paulo": 30,
    "Maria": 25,
}

// Adicionando/atualizando
idades["João"] = 35

// Verificando existência
idade, existe := idades["Paulo"]
if existe {
    fmt.Println("Idade do Paulo:", idade)
}

// Deletando
delete(idades, "Maria")
```

## Structs

Structs são tipos de dados compostos:

```go
package main

import "fmt"

type Pessoa struct {
    Nome  string
    Idade int
    Email string
}

// Método associado ao struct
func (p Pessoa) Saudacao() string {
    return fmt.Sprintf("Olá, meu nome é %s!", p.Nome)
}

func main() {
    pessoa := Pessoa{
        Nome:  "Paulo",
        Idade: 30,
        Email: "paulo@exemplo.com",
    }

    fmt.Println(pessoa.Saudacao())
}
```

## Próximos Passos

Agora que você tem o básico, aqui estão os próximos passos recomendados:

1. **Pratique**: Resolva problemas no [Exercism](https://exercism.io/tracks/go) ou [LeetCode](https://leetcode.com/)
2. **Leia**: [Effective Go](https://golang.org/doc/effective_go) é leitura obrigatória
3. **Construa**: Crie pequenos projetos - uma API REST, uma ferramenta CLI, etc.
4. **Explore**: Aprenda sobre goroutines, channels e a biblioteca padrão

## Recursos

- [Documentação Oficial do Go](https://golang.org/doc/)
- [Go by Example](https://gobyexample.com/)
- [A Tour of Go](https://tour.golang.org/)
- [Go Playground](https://play.golang.org/) - Teste código online

Go é uma linguagem poderosa e prática. Com esses fundamentos, você está pronto para começar a construir aplicações reais!
