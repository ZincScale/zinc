# Zinc — formal grammar (1.0 target)

**Status:** Phase 1 deliverable. Formal grammar applying the 22 decisions in `00-lessons-learned.md`. Drafted from the v2 parser; validated against `examples/`, `examples-fail/`, and `zinc-flow-go/src/`.

**This is the syntactic ground truth for the rebuild.** Static and dynamic semantics live in `02-semantics.md`. Type system lives in `03-type-system.md`.

## Notation

```
production       = rhs ;
[ optional ]
{ zero-or-more }
( grouping )
alt1 | alt2          alternatives, left-to-right priority
'literal'            literal characters
TOKEN_NAME           lexer-produced terminal
// comment           grammar-level comment
```

When alternatives overlap, the **first** match wins (PEG-style). Productions whose left side is **lowercase** are syntactic. Productions whose left side is `UPPERCASE_LIKE_THIS` are lexical. Whitespace and comments are skipped between syntactic tokens but never between characters of a lexical token.

---

## 1. Lexical structure

### 1.1 Whitespace and comments

```
ws_char          = ' ' | '\t' ;
newline          = '\n' | '\r\n' ;
line_comment     = '//' { any_char_except_newline } newline ;
block_comment    = '/*' { any_char } '*/' ;
```

Whitespace and comments are token separators; they don't produce tokens. Newlines are significant — they end statements (see §3.4).

### 1.2 Identifiers and keywords

```
IDENT_START      = 'a'..'z' | 'A'..'Z' | '_' ;
IDENT_CONT       = IDENT_START | '0'..'9' ;
IDENT            = IDENT_START { IDENT_CONT } ;
```

A lexed `IDENT` whose text matches a keyword is reclassified to that keyword's token type.

**Keywords (44, after `fn`, `construct`, and `end` removal):**

```
class       interface   data        enum        const       type
init        new         override    readonly
pub         this        super
import      from        package     use
return      if          else        for         while       break       continue
match       case        is          as          in
not         and         or
true        false       null
var         print       defer
spawn       parallel    timeout     select      with        using
```

**Note:** `void` is matched literally as the "no return type" marker (parses as IDENT in the lexer; the parser's `return_type` production accepts the literal text `void`). It is not a lexer-reserved keyword.

### 1.3 Integer and float literals

```
INT_LIT          = digit { digit | '_' } ;
HEX_LIT          = '0x' hex_digit { hex_digit | '_' } ;
BIN_LIT          = '0b' bin_digit { bin_digit | '_' } ;
OCT_LIT          = '0o' oct_digit { oct_digit | '_' } ;

FLOAT_LIT        = digit { digit | '_' } '.' digit { digit | '_' }
                   [ ('e' | 'E') [ '+' | '-' ] digit { digit } ] ;

digit            = '0'..'9' ;
hex_digit        = digit | 'a'..'f' | 'A'..'F' ;
bin_digit        = '0' | '1' ;
oct_digit        = '0'..'7' ;
```

`_` separators are allowed for readability (`1_000_000`). Stripped before the value is interpreted.

### 1.4 String literals (three forms)

```
STRING_LIT       = '"' { string_char } '"' ;
RAW_STRING_LIT   = '`' { any_char_except_backtick } '`' ;
INTERP_STRING    = '"' { interp_part } '"' ;       // when contains "${"

string_char      = any_char_except_quote_backslash_or_newline | escape_seq ;
escape_seq       = '\' ( '"' | '\\' | 'n' | 't' | 'r' | '0' | 'x' hex_digit hex_digit ) ;
interp_part      = string_char | '${' expr '}' ;
```

A double-quoted literal that contains `${` is lexed as `INTERP_STRING`; otherwise as `STRING_LIT`. Raw strings (backtick) do not process escapes and may span multiple lines.

### 1.5 Boolean and null

```
BOOL_LIT         = 'true' | 'false' ;
NULL_LIT         = 'null' ;
```

### 1.6 Operators and punctuation

After the 2026-05-01 token cleanup, the punctuation token set is:

```
'(' ')' '{' '}' '[' ']'
',' '.' ':'
'='
'+' '-' '*' '/' '%' '**'
'!' '&&' '||'
'&' '|' '^'              // binary bitwise (& also has prefix FFI form)
'==' '!=' '<' '<=' '>' '>='
'+=' '-=' '*=' '/='
'..' '..='               // range
'?.'                     // safe-nav
'?'                      // postfix optional-type marker
'...'                    // spread / variadic
'@'                      // annotation prefix
'**'                     // power (right-assoc)
```

**Removed from the lexer (2026-05-01):** `===` `!==` `??` `<-` `:=`. Encountering any of these is a lex error.

**`->` retained.** Used by match-expression cases (`case pat -> expr`), lambda alternatives, and other parser sites at 6 known consumption points. Not removed despite earlier draft listing.

**`;` retained as optional statement separator.** Newline is the canonical separator; `;` is also accepted between statements (one-liner support). Mandatory inside C-style `for` headers (`for (init; cond; post)`).

`<<` and `>>` are not single tokens — they are two adjacent `<` or `>` tokens with no whitespace gap (see §3.6 for shift handling and generic disambiguation).

---

## 2. Compilation unit

```
program          = [ package_decl ] { import_decl } { top_level_decl } ;

package_decl     = 'package' STRING_LIT newline ;

import_decl      = 'import' import_path newline ;
import_path      = IDENT { '/' IDENT } ;            // e.g. fabric/registry
```

A `.zn` file is a compilation unit. The package declaration is optional (default: `package main`). Imports come before any other declarations.

`from X import Y` is **not** supported (Python-ism, removed). `import "path/to/pkg"` quoted form is **not** supported (slashy imports, removed). The only form is `import alias_or_path`.

---

## 3. Top-level declarations

```
top_level_decl   = annotation_list ( fn_decl
                                   | class_decl
                                   | sealed_class_decl
                                   | data_class_decl
                                   | interface_decl
                                   | enum_decl
                                   | const_decl
                                   | type_alias_decl
                                   | test_decl
                                   ) ;

annotation_list  = { annotation } ;
annotation       = '@' IDENT [ '(' [ STRING_LIT { ',' STRING_LIT } ] ')' ] ;
```

**Annotation set is closed for 1.0** — only `@Json`, `@Yaml`, `@Toml`, `@Avro`, `@Test` are recognized. Unknown `@Name` is a compile error.

### 3.1 Function declaration

```
fn_decl          = [ 'pub' ] return_type IDENT [ type_params ] '(' [ param_list ] ')' fn_body ;

return_type      = 'void' | type_expr | tuple_type ;

fn_body          = block | '=' expr newline ;       // block or single-expression form

type_params      = '<' IDENT { ',' IDENT [ ':' bound_list ] } '>' ;
bound_list       = type_expr { '+' type_expr } ;    // T : Comparable + Hashable
```

**Examples:**

```zinc
pub int square(int x) = x * x
pub (int, error) parseNum(String s) { ... }
pub error validate(String input) { ... }
void main() { ... }
pub <T : Comparable> T max(T a, T b) { ... }
```

`return_type` is **type-first** (`int square(...)`), no `fn` keyword.
A function whose return type ends in `error` (in a tuple, or as the bare return) is a **thrower**.
Single-expression body (`= expr`) implicitly wraps `return expr`.

### 3.2 Parameter list

```
param_list       = param { ',' param } [ ',' ] ;   // trailing comma allowed

param            = [ 'const' ] [ '*' | '**' ] type_expr [ '...' ] IDENT [ '=' expr ]
                 | [ 'const' ] [ '*' | '**' ] IDENT [ '=' expr ] ;     // untyped fallback

variadic         = '*' IDENT
                 | '**' IDENT
                 | type_expr '...' IDENT ;
```

The two `param` alternatives express: typed-with-name vs untyped-name-only (rare; mostly for `*args`/`**kwargs`).

### 3.3 Class declarations

```
class_decl       = 'class' IDENT [ type_params ] [ ':' parent_list ] '{' class_body '}' ;

sealed_class_decl = 'sealed' 'class' IDENT [ type_params ] [ ':' parent_list ]
                    '{' { sealed_member } '}' ;

sealed_member    = data_class_decl                 // a variant
                 | method_decl
                 | field_decl ;

parent_list      = parent_ref { ',' parent_ref } ;
parent_ref       = qualified_name [ '<' type_arg_list '>' ] ;
qualified_name   = IDENT { '.' IDENT } ;

class_body       = { class_member } ;
class_member     = annotation_list ( field_decl | ctor_decl | method_decl ) ;

ctor_decl        = 'init' '(' [ param_list ] ')' block ;

method_decl      = [ 'pub' ] [ 'override' ] return_type IDENT [ type_params ]
                   '(' [ param_list ] ')' fn_body ;

field_decl       = [ 'pub' ] [ 'readonly' ] type_expr IDENT [ '=' expr ] newline
                 | [ 'pub' ] 'init' type_expr IDENT newline               // init field, frozen after ctor
                 | [ 'pub' ] 'const' type_expr IDENT '=' expr newline ;
```

**Sealed-variant placement (decided 2026-05-01):** variants are nested data-class declarations inside the sealed body, **newline-separated**:

```zinc
sealed class Result<T> {
    data Ok(T value)
    data Err(String message)
}
```

### 3.4 Data class declaration

```
data_class_decl  = 'data' IDENT [ type_params ] [ ':' parent_list ]
                   '(' [ data_param_list ] ')' [ '{' { method_decl } '}' ] ;

data_param_list  = data_param { ',' data_param } [ ',' ] ;
data_param       = [ 'pub' ] type_expr IDENT [ '=' expr ] ;
```

A data class's parameters become its fields. `pub` field-by-field controls cross-package visibility.

### 3.5 Interface, enum, const, type alias, test

```
interface_decl   = 'interface' IDENT [ type_params ] '{' { interface_member } '}' ;
interface_member = [ 'pub' ] return_type IDENT '(' [ param_list ] ')' newline ;

enum_decl        = 'enum' IDENT '{' enum_variant { ',' enum_variant } [ ',' ] '}' ;
enum_variant     = IDENT ;

const_decl       = [ 'pub' ] 'const' [ type_expr ] IDENT '=' expr newline ;

type_alias_decl  = 'type' IDENT '=' type_expr newline ;

test_decl        = 'test' STRING_LIT block ;        // *_test.zn files
```

---

## 4. Type expressions

```
type_expr        = optional_type ;

optional_type    = base_type [ '?' ] ;              // T?

base_type        = simple_type
                 | generic_type
                 | array_type
                 | function_type ;

simple_type      = qualified_name ;                 // int, String, core.Schema

generic_type     = qualified_name '<' type_arg_list '>' ;

type_arg_list    = type_expr { ',' type_expr } ;

array_type       = base_type '[' ']' ;              // int[], String[]

function_type    = 'Fn' '<' '(' [ type_arg_list ] ')' ',' return_type '>' ;

tuple_type       = '(' type_expr ',' type_expr { ',' type_expr } ')' ;
                                                    // tuple has >= 2 elements
```

**Position constraints:**
- `tuple_type` is allowed **only** in function/method/Fn return-type position. Not as a value, var, field, or generic argument.
- `T?` postfix produces `OptionalType{Inner: T}`. The inner cannot itself be `T?` (no `T??`).

**`<` `>` parsing.** When parsing a type and the next token after a `qualified_name` is `<`, attempt `generic_type`. The implementation is allowed to backtrack if the inside doesn't lex as a `type_arg_list`. (At expression position, the same `<` is comparison; see §6 for disambiguation.)

---

## 5. Statements

```
stmt             = block
                 | var_stmt
                 | tuple_var_stmt
                 | assign_stmt
                 | return_stmt
                 | if_stmt
                 | for_stmt
                 | while_stmt
                 | match_stmt
                 | select_stmt
                 | spawn_stmt
                 | parallel_for_stmt
                 | with_stmt
                 | timeout_stmt
                 | defer_stmt
                 | assert_stmt
                 | break_stmt
                 | continue_stmt
                 | print_stmt
                 | expr_stmt
                 | fn_decl                          // nested fn
                 ;

block            = '{' { stmt } '}' ;
```

A statement ends at a newline (or the close `}` of the enclosing block). `;` is also accepted as an optional statement separator — useful for multiple short statements on a single line (`var x = 1; var y = 2`). Newline is canonical; `;` is a tolerance affordance.

### 5.1 Var, tuple-var, assign, return

```
var_stmt         = 'var' IDENT [ '=' expr [ or_handler ] ] newline
                 | type_expr IDENT [ '=' expr [ or_handler ] ] newline
                 | 'const' [ type_expr ] IDENT '=' expr newline ;

// Forbidden: `var Type IDENT = expr` (the hybrid). var is for inference; named type drops `var`.

tuple_var_stmt   = 'var' IDENT ',' IDENT { ',' IDENT } '=' expr [ or_handler ] newline ;

assign_stmt      = lvalue assign_op expr [ or_handler ] newline ;
lvalue           = postfix_expr ;                   // ident, field access, index
assign_op        = '=' | '+=' | '-=' | '*=' | '/=' ;

return_stmt      = 'return' [ expr_list ] newline ;
expr_list        = expr { ',' expr } ;
```

`Type x = call() or { ... }` is allowed (decided 2026-05-01).

### 5.2 Control flow

```
if_stmt          = 'if' '(' expr ')' block [ 'else' ( if_stmt | block ) ] ;

for_stmt         = 'for' '(' for_c_init ';' expr ';' for_c_post ')' block         // C-style
                 | 'for' for_range_clause block ;
for_c_init       = var_stmt | assign_stmt ;
for_c_post       = assign_stmt ;
for_range_clause = '(' IDENT ',' IDENT ')' 'in' expr                              // (i, item) in coll
                 | IDENT 'in' expr ;

while_stmt       = 'while' '(' expr ')' block ;

match_stmt       = 'match' '(' expr ')' '{' { match_case } '}' ;
match_case       = 'case' match_pattern [ '->' ] block ;
match_pattern    = '_'                                          // wildcard
                 | IDENT '(' [ pattern_arg { ',' pattern_arg } ] ')'  // sealed-variant destructure
                 | expr ;
pattern_arg      = '_' | IDENT ;

break_stmt       = 'break' newline ;
continue_stmt    = 'continue' newline ;
```

**Parens around `if`/`while`/`for`/`match` headers are required** (2.6 in lessons-learned).

### 5.3 Concurrency / resources

```
select_stmt      = 'select' '{' { select_case } [ default_case ] '}' ;
select_case      = 'case' [ IDENT '=' ] expr ':' block ;
                                       // expr is restricted to chan.recv() / chan.send(v)
default_case     = 'default' ':' block ;

spawn_stmt       = 'spawn' block [ or_handler ] ;
parallel_for_stmt = 'parallel' [ '(' 'max' ':' expr ')' ] for_range_clause block [ or_handler ] ;

with_stmt        = 'with' '(' resource_list ')' block
                 | 'using' '(' single_resource ')' block        // single resource sugar
                 | 'lock' '(' expr ')' block ;                  // mutex form

resource_list    = single_resource { ',' single_resource } ;
single_resource  = 'var' IDENT '=' expr [ or_handler ] ;

timeout_stmt     = 'timeout' '(' expr ')' block [ or_handler ] ;

defer_stmt       = 'defer' expr newline ;
```

**`with`/`using`/`lock` lower to a single AST node** (decided 2026-05-01); the semantic distinction is documented in `02-semantics.md`.

### 5.4 Or-handler

```
or_handler       = 'or' block ;                     // body has `err` in scope
```

The `or match err { case T -> ... }` form is **dropped 2026-05-01**. To switch on error type, use:

```zinc
var x = call() or {
    match (err) {
        case ParseError(_) { ... }
        case _             { ... }
    }
}
```

### 5.5 Other

```
assert_stmt      = 'assert' expr [ ',' expr ] newline ;
print_stmt       = 'print' '(' expr ')' newline ;
expr_stmt        = expr [ or_handler ] newline ;
```

---

## 6. Expressions (with precedence)

Precedence table (lowest to highest binding). Each level produces an `expr` that the next level wraps.

| # | Level | Operators | Associativity |
|---|---|---|---|
| 1 | ternary / if-expr | `if cond : a else : b` | right |
| 2 | logical-or | `\|\|` | left |
| 3 | logical-and | `&&` (or `and`) | left |
| 4 | not | `!`, `not` | prefix |
| 5 | bitwise-or | `\|` | left |
| 6 | bitwise-xor | `^` | left |
| 7 | bitwise-and | `&` (binary) | left |
| 8 | comparison | `==` `!=` `<` `<=` `>` `>=` `is` `in` `is not` `not in` | non-assoc |
| 9 | range | `..`, `..=` | non-assoc |
| 10 | as-cast | `expr as type_expr` | left |
| 11 | shift | `<<`, `>>` (adjacent only) | left |
| 12 | add/sub | `+`, `-` | left |
| 13 | mul/div | `*`, `/`, `%` | left |
| 14 | unary | `-x`, `&x` (FFI) | prefix |
| 15 | power | `**` | right |
| 16 | postfix | `.field`, `?.field`, `[idx]`, `[lo:hi]`, `(args)`, `<T>(args)` | left |
| 17 | primary | literals, ident, `(expr)`, list/map/tuple lits, lambda, spawn, if/match expr | — |

```
expr             = if_expr | or_expr ;

if_expr          = 'if' expr ':' or_expr 'else' ':' or_expr ;        // expression-position only

or_expr          = and_expr { '||' and_expr } ;
and_expr         = not_expr { ( '&&' | 'and' ) not_expr } ;
not_expr         = ( '!' | 'not' ) not_expr | bor_expr ;
bor_expr         = bxor_expr { '|' bxor_expr } ;
bxor_expr        = band_expr { '^' band_expr } ;
band_expr        = cmp_expr { '&' cmp_expr } ;
cmp_expr         = range_expr { cmp_op range_expr } ;
cmp_op           = '==' | '!=' | '<' | '<=' | '>' | '>=' | 'is' | 'in' | 'is not' | 'not in' ;

range_expr       = as_expr [ ( '..' | '..=' ) as_expr ] ;

as_expr          = shift_expr { 'as' type_expr } ;

shift_expr       = addsub_expr { ( '<<' | '>>' ) addsub_expr } ;     // adjacency-checked
addsub_expr      = muldiv_expr { ( '+' | '-' ) muldiv_expr } ;
muldiv_expr      = unary_expr { ( '*' | '/' | '%' ) unary_expr } ;

unary_expr       = ( '-' | '&' ) unary_expr | power_expr ;
                                       // '&' restricted to FFI arg position by static check
power_expr       = postfix_expr [ '**' unary_expr ] ;                // right-assoc

postfix_expr     = primary_expr { postfix_op } ;
postfix_op       = '.' IDENT
                 | '?.' IDENT
                 | '[' expr ']'
                 | '[' [ expr ] ':' [ expr ] ']'        // slice
                 | '(' [ arg_list ] ')'                 // call
                 | '<' type_arg_list '>' '(' [ arg_list ] ')'
                                                        // call with type args (e.g. parse<Config>(...))
                 ;

primary_expr     = INT_LIT | HEX_LIT | BIN_LIT | OCT_LIT | FLOAT_LIT
                 | STRING_LIT | RAW_STRING_LIT | INTERP_STRING
                 | BOOL_LIT | NULL_LIT
                 | IDENT
                 | 'this' | 'super' '(' [ arg_list ] ')'
                 | '(' expr ')'
                 | '(' expr ',' expr { ',' expr } ')'   // tuple lit (>= 2)
                 | list_lit
                 | map_lit
                 | sized_array_expr
                 | capacity_expr
                 | lambda_expr
                 | spawn_expr
                 | if_expr
                 | match_expr
                 | 'new' IDENT [ '<' type_arg_list '>' ] [ '(' [ arg_list ] ')' ] ;

list_lit         = '[' [ expr_list [ ',' ] ] ']'
                 | 'List' '<' type_expr '>' '[' [ expr_list [ ',' ] ] ']' ;

map_lit          = '{' [ map_entry { ',' map_entry } [ ',' ] ] '}'
                 | 'Map' '<' type_expr ',' type_expr '>' '{' [ map_entry { ',' map_entry } [ ',' ] ] '}' ;
map_entry        = expr ':' expr ;

sized_array_expr = simple_type '[' expr ']' ;          // byte[16], int[N]
capacity_expr    = generic_type '(' expr ')' ;         // List<int>(1024)

lambda_expr      = '(' [ param_list ] ')' [ ':' return_type ] '=>' ( expr | block ) ;

spawn_expr       = 'spawn' block [ or_handler ] ;       // also valid as expression

match_expr       = 'match' '(' expr ')' '{' { match_expr_case } '}' ;
match_expr_case  = 'case' match_pattern '->' expr ;     // expression form
```

```
arg_list         = arg { ',' arg } [ ',' ] ;
arg              = expr [ '...' ]                       // positional, optional spread
                 | IDENT '=' expr ;                     // named arg
```

### 6.1 Generic disambiguation at call sites

`f<T>(args)` parses as call-with-type-arg when:
- The immediate prefix is a `qualified_name` (or `postfix_expr` resolving to one),
- The token after `<` lex-disambiguates to a `type_expr` start (an IDENT not followed by an arithmetic op),
- The type-arg list closes with `>` followed by `(`.

If those conditions don't hold, `<` is comparison. Implementation backtracks.

### 6.2 `&` (FFI prefix) static restriction

The grammar accepts `&expr` as `unary_expr`. A separate static check (run after parsing, before semantic analysis) ensures every `&expr` is the **top-level argument expression** of a call into a Go-imported package or method-on-Go-typed-receiver. Anywhere else is a compile error.

The check is a tree walk; it doesn't change the grammar.

---

## 7. Validation against existing examples

Sampled productions match the following (see `examples/`):

| Example | Productions exercised |
|---|---|
| `examples/classes.zn` | data_class_decl, class_decl, ctor_decl, method_decl, parent_list, super(...) |
| `examples/error_explicit.zn` | fn_decl with `(T, error)` return, `or` handler, return-with-error |
| `examples/error_subclass.zn` | class extending BaseError, transitive thrower-ness |
| `examples/exhaustive_match.zn` | sealed_class_decl with nested data variants, match_stmt, sealed-variant pattern |
| `examples/channels.zn` | spawn_stmt, channel `.send()`/`.recv()` method calls |
| `examples/concurrency.zn` | parallel_for_stmt, with_stmt (lock form) |
| `examples/casting_comprehensive.zn` | as_expr, type_expr in cast position |
| `examples/control_flow.zn` | if_stmt, for_stmt (C-style + range), while_stmt |
| `examples/collections_generics.zn` | generic_type, capacity_expr, list_lit |

**Negative examples** (`examples-fail/`) — each rejects via a specific grammar or static rule:

| Fail case | Rejection rule |
|---|---|
| `addrof_outside_ffi.zn` | §6.2 static check |
| `var_type_hybrid.zn` | §5.1 — `var Type IDENT` is forbidden |
| `control_flow_bare.zn` | §5.2 — parens required around `if` cond |
| `fn_keyword_removed.zn` | §1.2 — `fn` is no longer a keyword |
| `non_exhaustive_match.zn` | semantic check (`02-semantics.md` §match exhaustivity) |
| `unknown_field_annotation.zn` | §3 — annotation set is closed for 1.0 |
| `data_class_mutation.zn` | semantic check (data class fields are immutable) |

---

## 8. Known gaps from this draft (issues for Phase 1.1)

1. **String escape coverage.** The `escape_seq` rule lists common escapes; need to confirm Unicode escape (`\u{1F600}`) syntax — currently undefined.
2. **Numeric literal suffixes.** Are typed-int suffixes (`42L` for long, `1.5f` for float32) accepted? Today's lexer doesn't seem to support them. Spec choice: defer or define.
3. **Annotation argument types.** Currently restricted to `STRING_LIT { ',' STRING_LIT }` — i.e., string args only. Confirm before locking.
4. **`use` keyword.** Reserved but no production uses it. Drop or define.
5. **`from` keyword.** Reserved (was for `from X import Y`). Drop or define.
6. **`init` keyword overlap.** Used for both ctor body (`init(...)`) and field type (`init Type IDENT`). Disambiguation is positional; spec needs to be explicit.
7. **`@` annotation syntax** for non-string args. Some use cases (e.g., `@Test(timeout=5)`) might need int/bool args. Defer or define.

These are minor and don't block Phase 2.

---

## 9. Differences from the current parser

For implementers building the new parser from this grammar:

1. **Drop tokens:** `===`, `!==`, `??`, `<-`, `:=`, `fn`, `go` keyword (already gone). `->` stays (used by match-expr cases and lambda alternatives). `;` stays in lexer; stmt-position use becomes a parse error per 3.1.6.
2. **Drop AST fields:** `OrHandler.MatchCases`, `OrHandler.MatchVar`, `SendExpr`, `ReceiveExpr`.
3. **`or_handler`** simplifies to a single block; no match form.
4. **Generic type params** parse `[':' bound_list]` after each type param name; AST gains `bounds [][]TypeExpr` per param.
5. **Annotation set is closed** — parser rejects unknown `@Name` rather than accepting and propagating.
6. **`;` retained as optional statement separator** (matches current parser behavior — see §3.1.6 in 04-rebuild-plan.md).
7. **Tuple type** parses only at fn/method/Fn return positions; rejected elsewhere with a clear error.
8. **Channel arrow tokens** removed; `.send()`/`.recv()` only.

---

## 10. Sign-off checklist for Phase 2

Before drafting `02-semantics.md`:

- [ ] Notation choice confirmed (EBNF as defined in "Notation")
- [ ] §3 top-level decl set is complete (no missing decl kinds)
- [ ] §5 statement set is complete (no missing stmt kinds)
- [ ] §6 precedence table reviewed end-to-end
- [ ] §8 known gaps — accept as Phase 1.1 follow-ups, or address now
- [ ] §9 differences from current parser — confirmed
