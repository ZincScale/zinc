# zinc-php Implementation Plan

## Overview

Thin PHP transpiler — same architecture as zinc-python. Read `.zn`, apply transforms, write `.php`. FrankenPHP for deployment.

## Transforms (in order)

### 1. Add `$` sigils to variables
- Function/method parameters: `function greet(name)` → `function greet($name)`
- Assignments: `x = 5` → `$x = 5`
- Variable references: `echo x` → `echo $x`
- Skip: keywords, function names, class names, constants (UPPERCASE), `this`, `true/false/null`
- `this` → `$this` (special case)

### 2. Dot → arrow/double-colon
- Instance access: `obj.method()` → `$obj->method()`
- `this.name` → `$this->name`
- Static access: `ClassName.method()` → `ClassName::method()` (detect via uppercase first char)
- String concat uses `.` in PHP but we use `+` in zinc (or just rely on f-strings)

### 3. Add semicolons
- Add `;` at end of statements
- Skip: lines ending with `{`, `}`, block keywords (if/else/for/while/etc.), comments, blank lines
- Skip: lines inside `for(;;)` header

### 4. Magic method renames
- `construct` → `__construct`
- `destruct` → `__destruct`
- `toString` → `__toString`
- `invoke` → `__invoke`
- `get` → `__get`, `set` → `__set`
- `isset` → `__isset`, `unset` → `__unset`
- Same approach as zinc-python: regex on `function methodname(`

### 5. Add `<?php` header
- Prepend `<?php` to every generated `.php` file

## Architecture

```
zinc-php/
  compiler/
    transpiler.py    — the transforms
    zinc.py          — CLI (run, build, init, build --native)
    zinc             — entry point wrapper
  tests/
    test_transpiler.py
    run_e2e.sh
    e2e/             — test .zn files + expected output
  install.sh
  README.md
```

Mirror zinc-python structure exactly. Share nothing — each is standalone.

## CLI

Same as zinc-python but PHP-oriented:

- `zinc run file.zn` — transpile → run with `php`
- `zinc build src/ -o build/` — transpile to `.php` files
- `zinc build --native src/` — transpile → FrankenPHP single binary
- `zinc init myapp` — scaffold with `zinc.toml`

### zinc.toml for PHP

```toml
[project]
name = "myapp"
version = "0.1.0"
main = "index.zn"

[php]
version = ">=8.3"
deps = []              # composer packages
```

## FrankenPHP Integration

### Worker mode
- Generate a `worker.php` entry point that keeps the process alive
- FrankenPHP embeds PHP in a Go binary — single file deployment
- No nginx, no php-fpm config

### Build flow
```
zinc build --native src/
  1. Transpile .zn → .php
  2. Generate worker.php entry point
  3. Download FrankenPHP binary (cached)
  4. Package app + FrankenPHP → single binary
```

## Key Challenges

### Variable detection
Need to distinguish what gets `$`:
- YES: local variables, parameters, object properties after `$this->`
- NO: function names, class names, constants, `this`, `true/false/null`, string literals

Approach: track context. After `=`, in function args, after `echo`/`return` — those are variable positions. Or simpler: prefix everything that looks like a bareword in expression position, except known keywords and uppercase constants.

### Static vs instance
`Foo.bar()` → `Foo::bar()` (static)
`foo.bar()` → `$foo->bar()` (instance)

Heuristic: first char uppercase = class (static), lowercase = variable (instance). Works 99% of the time.

### String interpolation
PHP already has `"$var"` in double-quoted strings. Zinc uses `"{var}"` style.
Transform: `"{expr}"` → `"{$expr}"` in PHP strings. Complex expressions need `"{${expr}}"`.

## Implementation Order

1. **transpiler.py** — 5 transforms
2. **tests** — unit tests for each transform
3. **zinc.py** — CLI (run/build/init)
4. **e2e tests** — real PHP programs
5. **FrankenPHP integration** — native binary build
6. **Self-host** — write transpiler in zinc

## Dependencies

- PHP 8.3+ (for running transpiled code)
- Composer (for PHP deps)
- FrankenPHP (for native builds — downloaded on demand)
- Python 3.12+ (for running the transpiler itself, until self-hosted)
