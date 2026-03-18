# Design: Pipe Operator (Deferred)

## Status: Deferred — revisit after P1-P3

## Concept

`|>` pipes the result of the left expression as the first argument to the right function:

```zinc
readLine() |> toInt() |> abs() |> toString() |> print()

// equivalent to:
print(toString(abs(toInt(readLine()))))
```

## Why Deferred

- Method chaining already handles the primary use case (LINQ collection operations)
- Trailing lambdas (P1) will make chains even cleaner
- Pipe operator's sweet spot (function composition) is less common in Zinc's OOP-first model
- Risk of two ways to do the same thing (pipes vs method chains)

## Implementation Notes (When Ready)

- `a |> f()` desugars to `f(a)` — first argument injection
- `a |> f(x)` desugars to `f(a, x)` — prepend to arg list
- Lexer: `|>` as new token (TOKEN_PIPE)
- Parser: left-associative binary operator, low precedence
- Codegen: trivial rewrite — no new C# construct needed
- Effort: Quick
