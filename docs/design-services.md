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
- Codegen: detect service constructors, resolve dependency graph, emit wiring
- Compile errors: circular deps, missing deps
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

### Phase 4 — AWS Lambda handler
- `zinc deploy` or `zinc lambda` command
- Generates Lambda handler wrapping the service graph
- Produces a deployment-ready zip
- **Effort:** Medium

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
