# zinc-php — Clean PHP

PHP without the warts. No `$` sigils, no semicolons, `.` instead of `->`, sane magic methods. Deploys as a single binary via FrankenPHP worker mode.

```zinc
class Greeter {
    public function construct(name) {
        this.name = name
    }

    public function toString() {
        return "Hello, {this.name}!"
    }

    public function greet() {
        return strtoupper(this.name)
    }
}

g = new Greeter("world")
echo g.greet()
```

Transpiles to clean, editable PHP:

```php
<?php
class Greeter {
    public function __construct($name) {
        $this->name = $name;
    }

    public function __toString() {
        return "Hello, {$this->name}!";
    }

    public function greet() {
        return strtoupper($this->name);
    }
}

$g = new Greeter("world");
echo $g->greet();
```

## Transforms

1. **Add `$` sigils** — variables get `$` prefix automatically
2. **Add semicolons** — statement-ending `;` injected
3. **`.` → `->`** — dot access becomes arrow access for objects
4. **`.` → `::`** — static access (uppercase class name = static)
5. **Magic method renames** — `construct` → `__construct`, `toString` → `__toString`, etc.

Everything else is PHP — braces, `this`, `public function`, `new`, `echo`, `array`, closures.

## Usage

```bash
zinc run hello.zn                # transpile and run
zinc build src/ -o build/        # transpile to .php
zinc build src/ --native         # single binary via FrankenPHP
zinc init myapp                  # scaffold project
```

## Deployment

FrankenPHP worker mode — single binary, Go-powered HTTP server. No nginx, no php-fpm.

```bash
zinc build src/ --native         # produces a single executable
./myapp                          # runs the app
```

## Magic Method Mappings

| Zinc | PHP |
|------|-----|
| `construct` | `__construct` |
| `destruct` | `__destruct` |
| `toString` | `__toString` |
| `invoke` | `__invoke` |
| `get` | `__get` |
| `set` | `__set` |
| `isset` | `__isset` |
| `unset` | `__unset` |
| `clone` | `__clone` |
| `serialize` | `__serialize` |
| `unserialize` | `__unserialize` |

## License

[Apache License 2.0](../LICENSE)
