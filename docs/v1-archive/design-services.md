# Design: Services, Dependency Injection, and Providers

**Status:** Proposed
**Date:** 2026-03-18

## Problem

Enterprise apps need services that depend on other services. Today this means:

**Spring Boot:**
```java
@Service
public class UserService {
    @Autowired private UserRepository userRepo;
    @Autowired private EmailService emailService;
    @Autowired private AuditLogger auditLogger;
    @Autowired private CacheManager cacheManager;
    // Where do these come from? Component scanning. Good luck tracing it.
}
```

**ASP.NET Core:**
```csharp
// Register 50 of these
builder.Services.AddScoped<IUserService, UserService>();
builder.Services.AddScoped<IEmailService, SmtpEmailService>();
builder.Services.AddScoped<IAuditLogger, CloudWatchAuditLogger>();
// Forget one → runtime error: "Unable to resolve service for type 'IAuditLogger'"
```

The problems:
- **Registration boilerplate** — manually listing every service
- **Invisible wiring** — can't see what connects to what by reading the code
- **Runtime errors** — missing registrations blow up at runtime, not compile time
- **Interface ceremony** — `IUserService` / `UserService` pairs everywhere, even when there's only one implementation
- **DI container magic** — difficult to debug, hard to trace execution path

## Philosophy

**The compiler is the DI container.** Zinc knows every service, every constructor parameter, and every interface implementation at compile time. It resolves the dependency graph, detects errors, and generates the wiring code. No runtime container. No reflection. No registration boilerplate.

## Design

### The `service` Keyword

A `service` is a class that the compiler manages. Services can depend on other services via constructor parameters. The compiler wires them automatically.

```zinc
service UserRepository {
    pub User getById(Int id) {
        // database call
    }

    pub User create(User user) {
        // insert into database
    }
}

service EmailService {
    pub send(String to, String subject, String body) {
        // send email
    }
}

service UserService {
    readonly UserRepository users
    readonly EmailService email

    new(UserRepository users, EmailService email) {
        this.users = users
        this.email = email
    }

    pub User getById(Int id) {
        var user = users.getById(id)
        log.info("fetched user", userId: id)
        user
    }

    pub notifyUser(Int id, String message) {
        var user = users.getById(id)
        email.send(user.email, "Notification", message)
    }
}
```

Using a service in `main()`:

```zinc
main() {
    var svc = UserService()
    var user = svc.getById(42)
    print(user.name)
}
```

`UserService()` looks like a normal constructor call, but the compiler sees that `UserService` is a `service` with dependencies. It generates:

```csharp
var svc = new UserService(new UserRepository(), new EmailService());
```

The developer never writes the wiring. The compiler resolves it from the dependency graph.

### Compile-Time Dependency Resolution

The compiler:

1. Scans all `service` declarations across the project
2. Builds a dependency graph from constructor parameters
3. Topologically sorts the graph (leaf services instantiated first)
4. Detects circular dependencies → **compile error**
5. Detects missing services → **compile error**
6. Generates instantiation code in the correct order

**Circular dependency — caught at compile time:**

```zinc
service A {
    new(B b) { }
}

service B {
    new(A a) { }
}
// COMPILE ERROR: circular dependency: A → B → A
```

**Missing dependency — caught at compile time:**

```zinc
service UserService {
    new(UserRepository users) { }
}
// COMPILE ERROR: service UserService depends on UserRepository, which is not declared
```

Compare to Spring Boot and ASP.NET Core where both of these are runtime errors.

### Providers — Config-Driven Implementation Selection

When you have an interface with multiple implementations, `zinc.toml` selects which one to use. The compiler verifies the selection at build time.

#### Define the contract:

```zinc
interface PaymentProvider {
    pub ProcessResult charge(Order order)
    pub RefundResult refund(String transactionId)
}
```

#### Implement it:

```zinc
service StripePayment : PaymentProvider {
    pub ProcessResult charge(Order order) {
        // Stripe API call
    }
    pub RefundResult refund(String transactionId) {
        // Stripe refund
    }
}

service SquarePayment : PaymentProvider {
    pub ProcessResult charge(Order order) {
        // Square API call
    }
    pub RefundResult refund(String transactionId) {
        // Square refund
    }
}
```

#### Select in config:

```toml
[providers]
PaymentProvider = "StripePayment"
```

#### Use it:

```zinc
service OrderService {
    readonly PaymentProvider payments

    new(PaymentProvider payments) {
        this.payments = payments
    }

    pub checkout(Order order) {
        var result = payments.charge(order)
        log.info("payment processed", orderId: order.id)
    }
}
```

The compiler reads `zinc.toml`, sees `PaymentProvider = "StripePayment"`, verifies that `StripePayment` implements `PaymentProvider`, and generates:

```csharp
var payments = new StripePayment();
var orderService = new OrderService(payments);
```

Change the config line to `"SquarePayment"`, rebuild, different provider. No code changes. No runtime errors.

#### Compile-time verification:

```toml
[providers]
PaymentProvider = "BraintreePayment"
```

```
COMPILE ERROR: provider "BraintreePayment" not found for interface PaymentProvider
  available: StripePayment, SquarePayment
```

#### Single implementation — no config needed:

If only one service implements an interface, the compiler selects it automatically:

```zinc
interface AuditLogger {
    pub log(String action, String details)
}

service CloudWatchAuditLogger : AuditLogger {
    pub log(String action, String details) {
        // CloudWatch API call
    }
}

service UserService {
    readonly AuditLogger audit    // only one impl → auto-resolved
    new(AuditLogger audit) { this.audit = audit }
}
// No zinc.toml entry needed — CloudWatchAuditLogger is the only option
```

If a second implementation is added later, the compiler requires a `[providers]` entry to disambiguate.

### `zinc providers` CLI Command

See all interfaces and their implementations:

```bash
$ zinc providers
PaymentProvider:
  StripePayment    (active — zinc.toml)
  SquarePayment

AuditLogger:
  CloudWatchAuditLogger  (active — single implementation)

EmailProvider:
  SesEmailService  (active — zinc.toml)
  SmtpEmailService
```

### Testing — Override Dependencies

In tests, pass different implementations directly via constructor:

```zinc
service MockUserRepository {
    pub User getById(Int id) {
        User(id, "TestUser", "test@test.com")
    }
}

test_get_user() {
    var svc = UserService(users: MockUserRepository())
    var user = svc.getById(1)
    assertEqual(user.name, "TestUser")
}

test_notify_user() {
    var mockEmail = MockEmailService()
    var svc = UserService(
        users: MockUserRepository(),
        email: mockEmail
    )
    svc.notifyUser(1, "hello")
    assert(mockEmail.sent)
}
```

No interface required for mocking. The mock just needs matching method signatures. Named constructor args make it clear which dependency is being overridden.

### Service Lifetimes

Services need different lifetimes depending on what they hold:

| Lifetime | When to use | Example |
|----------|------------|---------|
| **singleton** | Stateless or shared state, expensive to create | Database connection pool, config, cache |
| **scoped** | Per-request state, must not leak across requests | Request context, current user, unit of work |
| **transient** | Lightweight, no shared state, cheap to create | Formatters, validators, mappers |

#### Syntax

```zinc
service DbConnectionPool { }                    // default — singleton
service RequestContext { scope: scoped }         // one per request
service InputValidator { scope: transient }      // new instance every time
```

No keyword clutter in the default case. Most services are singletons — connection pools, repositories, external clients. You only annotate when you need something different.

#### How it works

**Singleton** (default) — one instance for the lifetime of the application. Created once at startup, shared across all requests. The compiler instantiates these in dependency order in `main()` or the server startup.

```zinc
// Created once, shared by all requests
service UserRepository {
    readonly DbConnectionPool db
    new(DbConnectionPool db) { this.db = db }
}
```

**Scoped** — one instance per request. In a web server (`serve()`), each HTTP request gets a fresh instance. In Lambda, each invocation is one scope. Scoped services can depend on singletons but not the other way around.

```zinc
service RequestContext { scope: scoped }
    pub readonly String requestId
    pub readonly String userId

    new() {
        this.requestId = generateId()
        this.userId = ""
    }

    pub setUser(String id) {
        this.userId = id
    }
}

service AuditLogger {
    readonly RequestContext context    // scoped — gets current request's context
    new(RequestContext context) { this.context = context }

    pub log(String action) {
        log.info("{action}", requestId: context.requestId, userId: context.userId)
    }
}
```

**Transient** — new instance every time it's requested. No shared state. Useful for lightweight objects that shouldn't carry state between uses.

```zinc
service InputValidator { scope: transient }
    pub Bool validate(String input) {
        input.length() > 0
    }
}
```

#### Compile-time lifetime validation

The compiler enforces lifetime rules:

```zinc
service RequestContext { scope: scoped }

service CacheManager {                         // singleton (default)
    readonly RequestContext context             // COMPILE ERROR
    new(RequestContext context) { this.context = context }
}
// ERROR: singleton service CacheManager cannot depend on scoped service RequestContext
//   singletons outlive scoped services — the reference would become stale
```

| Depends on → | Singleton | Scoped | Transient |
|---|---|---|---|
| **Singleton** | OK | **Error** | OK |
| **Scoped** | OK | OK | OK |
| **Transient** | OK | OK | OK |

A singleton can't hold a scoped dependency (the scoped instance would outlive its scope). This is a common bug in ASP.NET Core that produces a runtime warning — Zinc catches it at compile time.

#### C# mapping

| Zinc | C# Emit |
|------|---------|
| `service Foo { }` (singleton) | Single `new Foo()` at startup, passed by reference |
| `service Foo { scope: scoped }` | Created per-request in `app.Use()` middleware or Lambda handler |
| `service Foo { scope: transient }` | `new Foo()` at every injection point |

For Lambda (single request = single invocation), singleton and scoped behave the same — everything is created once per invocation. Lifetimes matter when running as a long-lived server via `serve()`.

#### Lifetime in zinc.toml (optional override)

For testing or environment-specific behavior, lifetimes can be overridden in config:

```toml
[services]
RequestContext.scope = "transient"    # override to transient for testing
```

This is rare — lifetime should live in the code. But it's useful for debugging (make everything transient to isolate state bugs).

### Convention Routing (Phase 2)

When a service is annotated or declared as an API, its methods become HTTP endpoints:

```zinc
api UserApi {
    readonly UserService users

    new(UserService users) { this.users = users }

    // GET /users/:id
    pub User getById(Int id) {
        users.getById(id)
    }

    // POST /users
    pub User create(User user) {
        users.create(user)
    }

    // GET /users
    pub List<User> getAll() {
        users.getAll()
    }
}

main() {
    serve(port: 8080)
}
```

Route inference rules:

| Method signature | HTTP verb | Route |
|---|---|---|
| `get*()` | GET | `/{resource}` |
| `get*(Int id)` | GET | `/{resource}/:id` |
| `create(T body)` | POST | `/{resource}` |
| `update(Int id, T body)` | PUT | `/{resource}/:id` |
| `delete(Int id)` | DELETE | `/{resource}/:id` |
| `list*()` or returns `List<T>` | GET | `/{resource}` |

Resource name is inferred from the api class name: `UserApi` → `/users`.

## C# Mapping

| Zinc | C# Emit |
|------|---------|
| `service Foo { }` | `public class Foo { }` (with generated wiring) |
| `service Foo : IBar { }` | `public class Foo : IBar { }` |
| `Foo()` in main (service) | `new Foo(new Dep1(), new Dep2())` (compiler-generated) |
| `[providers] IBar = "Foo"` | `IBar bar = new Foo();` |
| `api UserApi { }` | ASP.NET Core Minimal API `MapGet`/`MapPost` calls |
| `serve(port: 8080)` | `app.Run()` |

## Example: Complete Service App

```zinc
data User(pub readonly Int id, pub readonly String name, pub readonly String email)
data Order(pub readonly Int id, pub readonly Int userId, pub readonly Float amount)

interface PaymentProvider {
    pub ProcessResult charge(Order order)
}

service StripePayment : PaymentProvider {
    pub ProcessResult charge(Order order) {
        log.info("charging via Stripe", orderId: order.id, amount: order.amount)
        ProcessResult(true, "stripe-txn-123")
    }
}

service UserRepository {
    pub User getById(Int id) {
        User(id, "Alice", "alice@example.com")
    }
}

service OrderService {
    readonly UserRepository users
    readonly PaymentProvider payments

    new(UserRepository users, PaymentProvider payments) {
        this.users = users
        this.payments = payments
    }

    pub ProcessResult checkout(Int userId, Float amount) {
        var user = users.getById(userId)
        var order = Order(1, userId, amount)
        var result = payments.charge(order)
        log.info("checkout complete", user: user.name, success: result.success)
        result
    }
}

main() {
    var orders = OrderService()
    var result = orders.checkout(42, 99.99)
    print("success: {result.success}")
}
```

```toml
[project]
name = "orderapp"
version = "0.1.0"

[build]
target = "csharp"

[providers]
PaymentProvider = "StripePayment"
```

```bash
$ zinc build
  resolving service graph...
    UserRepository (leaf)
    StripePayment → PaymentProvider (from zinc.toml)
    OrderService → UserRepository, PaymentProvider
  transpiled 1 file(s) to C#
  built orderapp (AOT native binary)

$ ./orderapp
success: true
```

## Implementation Plan

### Phase 1 — `service` keyword + compile-time DI
- AST: `ServiceDecl` node (extends ClassDecl with service semantics)
- Parser: `service Name { }` parsed like class but flagged as service
- Parser: `scope: scoped` / `scope: transient` field in service declaration
- Codegen: detect service constructors, resolve dependency graph, emit wiring
- Codegen: lifetime-aware instantiation (singleton at startup, scoped per-request, transient per-use)
- Compile errors: circular deps, missing deps, lifetime violations (singleton → scoped)
- **Effort:** Medium

### Phase 2 — Providers
- Config: parse `[providers]` section from zinc.toml
- Codegen: resolve interface → implementation from config
- Auto-resolve single implementations
- `zinc providers` CLI command
- Compile errors: missing provider, wrong type
- **Effort:** Medium

### Phase 3 — Convention routing (`api` keyword)
- AST: `ApiDecl` node
- Parser: `api Name { }` with method declarations
- Codegen: emit ASP.NET Core Minimal API calls
- `serve(port)` builtin
- Route inference from method signatures
- **Effort:** Large — needs ASP.NET Core integration

### Phase 4 — Aspects (cross-cutting concerns)
- AST: `AspectDecl` node with `before`, `after`, `error` blocks
- Parser: `aspect name { before { } after { } error { } }`
- Codegen: inline aspect code into service methods at compile time
- Built-in aspects: `@logged`, `@timed`, `@authorized`, `@retry`, `@cached`, `@timeout`
- **Effort:** Medium

### Phase 5 — AWS Lambda handler
- `zinc deploy` or `zinc lambda` command
- Generates Lambda handler wrapping the service graph
- Produces a deployment-ready zip
- **Effort:** Medium

## Aspects — Cross-Cutting Concerns

### Problem

Enterprise services need logging, metrics, auth, retries, caching, and timeouts on every method. In Spring Boot this is AOP — runtime proxies and bytecode manipulation that's invisible in the code and impossible to debug:

```java
// Spring AOP — magic that wraps your class at runtime
@Aspect
@Component
public class LoggingAspect {
    @Around("execution(* com.myapp.services.*.*(..))")
    public Object logMethod(ProceedingJoinPoint jp) throws Throwable {
        log.info("{} called", jp.getSignature());
        long start = System.currentTimeMillis();
        try {
            Object result = jp.proceed();
            log.info("{} completed in {}ms", jp.getSignature(), System.currentTimeMillis() - start);
            return result;
        } catch (Throwable t) {
            log.error("{} failed: {}", jp.getSignature(), t.getMessage());
            throw t;
        }
    }
}
// Now good luck figuring out why your stack trace has 15 proxy frames
```

### Design

Zinc aspects are **compile-time code generation**. The compiler reads the aspect definition and inlines the cross-cutting code directly into the service methods. No proxies. No bytecode weaving. The generated C# shows exactly what runs.

#### Defining aspects

```zinc
aspect logged {
    before {
        log.info("{method} called", args: args)
    }
    after {
        log.info("{method} completed", duration: elapsed)
    }
    error {
        log.error("{method} failed: {err}", duration: elapsed)
    }
}

aspect timed {
    after {
        metrics.record("{service}.{method}", elapsed)
    }
}

aspect authorized(String role) {
    before {
        if !context.hasRole(role) {
            panic("unauthorized: requires {role}")
        }
    }
}

aspect retry(Int times, Int delayMs) {
    around {
        var attempts = 0
        while attempts < times {
            attempts = attempts + 1
            var result = proceed() or {
                if attempts == times {
                    panic("failed after {times} attempts: {err}")
                }
                log.warn("{method} attempt {attempts} failed: {err}")
                sleep(delayMs)
                continue
            }
            return result
        }
    }
}

aspect cached(Int ttlMs) {
    around {
        var key = "{service}.{method}.{args}"
        var hit = cache.get(key)
        if hit != null { return hit }
        var result = proceed()
        cache.set(key, result, ttlMs)
        return result
    }
}

aspect timeout(Int ms) {
    around {
        var result = spawn { proceed() }
        result.value or {
            panic("{method} timed out after {ms}ms")
        }
    }
}
```

#### Built-in variables available in aspects

| Variable | Type | Description |
|----------|------|-------------|
| `method` | String | Name of the method being called |
| `service` | String | Name of the service class |
| `args` | dynamic | The method arguments |
| `elapsed` | Int | Milliseconds elapsed (available in `after` and `error`) |
| `err` | String | Error message (available in `error` block) |
| `proceed()` | dynamic | Execute the actual method (available in `around`) |

#### Applying aspects

Apply to an entire service (all public methods):

```zinc
@logged @timed
service OrderService {
    pub ProcessResult checkout(Int userId, Float amount) {
        // every call is logged and timed automatically
    }
}
```

Apply to specific methods:

```zinc
service PaymentService {
    @timeout(5000)
    @cached(60000)
    pub ExchangeRate getRate(String currency) {
        httpGet("https://api.exchange.com/rates/{currency}")
    }

    @authorized("finance")
    @retry(3, 1000)
    pub ProcessResult charge(Order order) {
        // retries up to 3 times, requires finance role
    }

    pub String healthCheck() {
        // no aspects — just a simple method
        "ok"
    }
}
```

#### What the compiler generates

```zinc
@logged @timed
service UserService {
    readonly UserRepository users
    new(UserRepository users) { this.users = users }

    pub User getById(Int id) {
        users.getById(id)
    }
}
```

The compiler inlines the aspect code into each public method:

```csharp
public class UserService
{
    private readonly UserRepository _users;
    public UserService(UserRepository users) { _users = users; }

    public User GetById(int id)
    {
        // @logged — before
        Log.Information("GetById called {@Args}", new { id });
        var _sw = Stopwatch.StartNew();
        try
        {
            // actual method body
            var _result = _users.GetById(id);

            _sw.Stop();
            // @logged — after
            Log.Information("GetById completed {@Duration}ms", _sw.ElapsedMilliseconds);
            // @timed — after
            Metrics.Record("UserService.GetById", _sw.ElapsedMilliseconds);
            return _result;
        }
        catch (Exception _ex)
        {
            _sw.Stop();
            // @logged — error
            Log.Error("GetById failed: {Error} {@Duration}ms", _ex.Message, _sw.ElapsedMilliseconds);
            throw;
        }
    }
}
```

**Key point:** this is the actual generated code. No proxies, no hidden behavior. You can read the `.cs` file in `.zinc-build/` and see exactly what runs. Stack traces show normal method calls, not 15 layers of proxy/interceptor frames.

#### Built-in aspects that ship with Zinc

| Aspect | Parameters | What it does |
|--------|-----------|-------------|
| `@logged` | none | Log entry, exit, errors with timing for every method |
| `@timed` | none | Record method execution time to metrics |
| `@authorized(role)` | role name | Check permissions before execution |
| `@retry(times, delay)` | attempts, delay in ms | Retry on failure with delay between attempts |
| `@cached(ttl)` | TTL in ms | Cache return value, serve from cache if fresh |
| `@timeout(ms)` | max duration in ms | Fail if method exceeds time limit |

#### Comparison

| | Spring AOP | ASP.NET Middleware | Zinc Aspects |
|---|---|---|---|
| Mechanism | Runtime proxies + bytecode | Request pipeline | **Compile-time code generation** |
| Visibility | Hidden — can't see what runs | Partially visible | **Fully visible in generated C#** |
| Debugging | Proxy stack traces | Middleware chain | **Normal stack traces** |
| Performance | Proxy overhead per call | Pipeline overhead | **Zero overhead — inlined** |
| Scope | Class-level or method-level | Request-level only | **Class-level or method-level** |
| Apply | `@Aspect` + pointcut expressions | `app.Use()` ordering | **`@name` on service or method** |

#### Complete example

```zinc
@logged
service OrderApi {
    readonly OrderService orders
    readonly PaymentProvider payments

    new(OrderService orders, PaymentProvider payments) {
        this.orders = orders
        this.payments = payments
    }

    @timed
    pub Order getById(Int id) {
        orders.getById(id)
    }

    @timed @authorized("customer")
    pub ProcessResult checkout(Int userId, Float amount) {
        var order = orders.create(userId, amount)
        payments.charge(order)
    }

    @cached(30000) @timeout(5000)
    pub List<Order> getRecent() {
        orders.recentOrders(100)
    }
}
```

This is 25 lines. The equivalent Spring Boot code (controller + aspect classes + security config + cache config) is 80-100 lines across 4 files.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Compile-time DI | Compiler resolves graph | No runtime errors, no reflection, no container. The entire graph is known at build time. |
| No DI container | Generated code | Zinc targets Lambda/containers — lightweight, fast startup. DI containers add overhead and complexity. |
| Config-driven providers | `[providers]` in zinc.toml | Change implementation without code changes. Compile-time verified. |
| Auto-resolve single impl | No config needed | Convention over configuration — if there's only one choice, make it. |
| No `IFoo`/`Foo` ceremony | Interface only when multiple impls | Most services have one implementation. Interfaces are for the provider pattern, not every service. |
| Named args for test overrides | `UserService(users: mockRepo)` | Clear, explicit, no test framework needed. Consistent with Zinc's named arg syntax. |
| `api` separate from `service` | Different concerns | Services contain business logic. APIs define HTTP boundaries. Separation of concerns. |
| Convention routing | Method name → HTTP verb | Zero annotation ceremony. Matches the "clear intent" philosophy. |
| Singleton default | Most services are stateless/shared | Convention over configuration — annotate only when different. |
| Compile-time lifetime checks | Singleton can't depend on scoped | ASP.NET catches this at runtime with a warning. Zinc catches it at compile time. |
| Compile-time aspects | Inlined code generation | No proxies, no bytecode weaving, normal stack traces, zero runtime overhead. |
| Built-in aspects | `@logged`, `@timed`, `@retry`, etc. | Most common cross-cutting concerns pre-built. One annotation instead of a separate aspect class. |
| Aspect visibility | Generated C# shows inlined code | "Clearer Spring" — you can always see what runs by reading the generated code. |
