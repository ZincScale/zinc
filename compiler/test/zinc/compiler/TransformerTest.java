// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

public class TransformerTest {

    static int passed = 0;
    static int failed = 0;

    public static void main(String[] args) {
        testHelloWorld();
        testFunction();
        testClassDecl();
        testInterface();
        testDataClass();
        testVarAndArithmetic();
        testIfElse();
        testForIn();
        testLambda();
        testOrHandler();
        testOrHandlerBlock();
        testReturnError();
        testReturnErrorCustomType();
        testExprOrHandler();
        testSpawn();
        testConcurrent();
        testParallelFor();
        testEquality();
        testArrayLiteral();
        testMapLiteral();
        testMethodAliases();
        testStreamChain();
        testStreamIt();
        testInOperator();
        testSealedClass();
        testExpressionIf();
        testMatchExpr();
        testDefaultParams();
        testSingleExprFn();
        testVarArgs();
        testConstField();
        testForWithIndex();
        testInheritance();
        testDataClassToString();
        testPrimitiveEquality();
        testExpressionLambdaVoidContext();
        testExpressionLambdaValueContext();
        testArrayFieldDefault();

        System.out.println("\nResults: " + passed + " passed, " + failed + " failed");
        if (failed > 0) System.exit(1);
    }

    static String transpile(String zinc) {
        var tokens = new Lexer(zinc).tokenize().unwrap();
        var program = new Parser(tokens).parse();
        var result = new Transformer().transformAll(program);
        var sb = new StringBuilder();
        for (var cu : result.unwrap()) {
            sb.append(cu.toString()).append("\n");
        }
        return sb.toString();
    }

    static void testHelloWorld() {
        var java = transpile("print(\"Hello, World!\")");
        assertContains("hello: println", java, "System.out.println");
        assertContains("hello: string", java, "Hello, World!");
        assertContains("hello: main", java, "public static void main");
        System.out.println("--- Hello World ---");
        System.out.println(java);
    }

    static void testFunction() {
        var java = transpile("""
            fn add(int a, int b): int {
                return a + b
            }
            """);
        // Functions without script stmts — need to verify
        System.out.println("--- Function ---");
        System.out.println(java);
    }

    static void testClassDecl() {
        var java = transpile("""
            class Dog {
                init String name

                init(String name) {
                    this.name = name
                }

                pub fn bark(): String {
                    return "Woof!"
                }
            }
            """);
        assertContains("class: Dog", java, "class Dog");
        assertContains("class: field", java, "String name");
        assertContains("class: ctor", java, "Dog(String name)");
        assertContains("class: method", java, "bark()");
        assertContains("class: getter", java, "getName()");
        System.out.println("--- Class ---");
        System.out.println(java);
    }

    static void testInterface() {
        var java = transpile("""
            interface Speaker {
                fn speak(): String
            }
            """);
        assertContains("iface: interface", java, "interface Speaker");
        assertContains("iface: method", java, "String speak()");
        assertNotContains("iface: no throws", java, "throws Exception");
        System.out.println("--- Interface ---");
        System.out.println(java);
    }

    static void testDataClass() {
        var java = transpile("data Point(int x, int y)");
        assertContains("data: record", java, "record Point");
        assertContains("data: params", java, "int x, int y");
        System.out.println("--- Data Class ---");
        System.out.println(java);
    }

    static void testVarAndArithmetic() {
        var java = transpile("""
            var x = 1 + 2 * 3
            print(x)
            """);
        assertContains("var: declaration", java, "var x");
        assertContains("var: println", java, "System.out.println");
        System.out.println("--- Var + Arithmetic ---");
        System.out.println(java);
    }

    static void testIfElse() {
        var java = transpile("""
            if x > 0 {
                print("positive")
            } else {
                print("negative")
            }
            """);
        assertContains("if: condition", java, "x > 0");
        assertContains("if: then", java, "positive");
        assertContains("if: else", java, "negative");
        System.out.println("--- If/Else ---");
        System.out.println(java);
    }

    static void testForIn() {
        var java = transpile("""
            for item in items {
                print(item)
            }
            """);
        assertContains("for: foreach", java, "for (var item : items)");
        System.out.println("--- For In ---");
        System.out.println(java);
    }

    static void testLambda() {
        var java = transpile("""
            var f = x -> x * 2
            """);
        assertContains("lambda: arrow", java, "->");
        System.out.println("--- Lambda ---");
        System.out.println(java);
    }

    static void testOrHandler() {
        var java = transpile("""
            var x = parseInt("42") or { -1 }
            """);
        assertContains("or: try", java, "try");
        assertContains("or: catch", java, "catch (Exception err)");
        assertContains("or: default", java, "-1");
        System.out.println("--- Or Handler ---");
        System.out.println(java);
    }

    static void testOrHandlerBlock() {
        var java = transpile("""
            var data = readFile("config.json") or {
                print("file not found")
                return
            }
            """);
        assertContains("or_block: try", java, "try");
        assertContains("or_block: catch", java, "catch (Exception err)");
        assertContains("or_block: print", java, "System.out.println");
        System.out.println("--- Or Handler Block ---");
        System.out.println(java);
    }

    static void testReturnError() {
        var java = transpile("""
            fn loadConfig(): String {
                var data = readFile("config.json") or {
                    return Error("config missing")
                }
                return data
            }
            loadConfig()
            """);
        assertContains("ret_err: throw", java, "throw new RuntimeException");
        assertContains("ret_err: msg", java, "config missing");
        System.out.println("--- Return Error ---");
        System.out.println(java);
    }

    static void testReturnErrorCustomType() {
        var java = transpile("""
            fn findUser(String id): User {
                return Error(NotFound("user not found"))
            }
            findUser("1")
            """);
        assertContains("ret_custom: throw", java, "throw new NotFound");
        assertContains("ret_custom: msg", java, "user not found");
        System.out.println("--- Return Error Custom Type ---");
        System.out.println(java);
    }

    static void testExprOrHandler() {
        var java = transpile("""
            doSomething() or {
                print("failed")
            }
            """);
        assertContains("expr_or: try", java, "try");
        assertContains("expr_or: catch", java, "catch (Exception err)");
        assertContains("expr_or: handler", java, "System.out.println");
        System.out.println("--- Expr Or Handler ---");
        System.out.println(java);
    }

    static void testSpawn() {
        var java = transpile("""
            var task = spawn {
                print("working")
            } or {
                print("failed")
            }
            """);
        assertContains("spawn: CompletableFuture", java, "CompletableFuture");
        assertContains("spawn: Thread.ofVirtual", java, "Thread.ofVirtual");
        assertContains("spawn: complete", java, "_f.complete(null)");
        assertContains("spawn: completeExceptionally", java, "_f.completeExceptionally");
        System.out.println("--- Spawn ---");
        System.out.println(java);
    }

    static void testConcurrent() {
        var java = transpile("""
            concurrent {
                fetchUser(id)
                fetchOrders(id)
            }
            """);
        assertContains("concurrent: scope", java, "StructuredTaskScope");
        assertContains("concurrent: fork", java, "_scope.fork");
        assertContains("concurrent: join", java, "_scope.join");
        assertContains("concurrent: joiner", java, "awaitAllSuccessfulOrThrow");
        System.out.println("--- Concurrent ---");
        System.out.println(java);
    }

    static void testParallelFor() {
        var java = transpile("""
            parallel for item in items {
                process(item)
            }
            """);
        assertContains("pfor: scope", java, "StructuredTaskScope");
        assertContains("pfor: fork", java, "_scope.fork");
        assertContains("pfor: foreach", java, "for (var item : items)");
        System.out.println("--- Parallel For ---");
        System.out.println(java);
    }

    static void testEquality() {
        var java = transpile("var x = a == b\nvar y = a === b\nvar z = a != b");
        assertContains("eq: Objects.equals", java, "Objects.equals(a, b)");
        assertContains("eq: ref ==", java, "a == b");
        assertContains("eq: !Objects.equals", java, "!java.util.Objects.equals(a, b)");
    }

    static void testArrayLiteral() {
        var java = transpile("int[] nums = [1, 2, 3]");
        assertContains("arr: new int[]", java, "new int[]");
        assertContains("arr: elements", java, "1, 2, 3");
    }

    static void testMapLiteral() {
        var java = transpile("var m = {\"a\": 1, \"b\": 2}");
        assertContains("map: LinkedHashMap", java, "LinkedHashMap");
        assertContains("map: put", java, "put");
    }

    static void testMethodAliases() {
        var java = transpile("var x = s.upper()\nvar y = s.lower()\nvar z = s.trim()");
        assertContains("alias: upper", java, "toUpperCase");
        assertContains("alias: lower", java, "toLowerCase");
        assertContains("alias: trim", java, "strip");
    }

    static void testStreamChain() {
        var java = transpile("var x = nums.filter(x -> x > 0).map(x -> x * 2).toList()");
        assertContains("stream: .stream()", java, ".stream()");
        assertContains("stream: .filter(", java, ".filter(");
        assertContains("stream: .map(", java, ".map(");
        assertContains("stream: .toList()", java, ".toList()");
    }

    static void testStreamIt() {
        var java = transpile("var x = nums.filter(it > 5)");
        assertContains("it: _it", java, "_it");
        assertContains("it: lambda", java, "->");
        assertContains("it: stream", java, ".stream()");
    }

    static void testInOperator() {
        var java = transpile("var x = \"hello\" in list");
        assertContains("in: contains", java, ".contains(");
    }

    static void testSealedClass() {
        var java = transpile("""
            sealed class Shape {
                data Circle(double radius)
                data Rect(double w, double h)
            }
            """);
        assertContains("sealed: interface", java, "sealed interface Shape");
        assertContains("sealed: permits", java, "permits Circle, Rect");
        assertContains("sealed: Circle record", java, "record Circle");
        assertContains("sealed: Rect record", java, "record Rect");
        assertContains("sealed: implements Shape", java, "implements Shape");
    }

    static void testExpressionIf() {
        var java = transpile("var x = if true: \"yes\" else: \"no\"");
        assertContains("if_expr: ternary", java, "?");
        assertContains("if_expr: yes", java, "yes");
        assertContains("if_expr: no", java, "no");
    }

    static void testMatchExpr() {
        var java = transpile("""
            var x = match status {
                case "ok" { "success" }
                case _ { "unknown" }
            }
            """);
        assertContains("match_expr: Objects.equals", java, "Objects.equals");
        assertContains("match_expr: success", java, "success");
        assertContains("match_expr: unknown", java, "unknown");
    }

    static void testDefaultParams() {
        var java = transpile("""
            fn connect(String host, int port = 8080): String {
                return host
            }
            connect("localhost")
            """);
        assertContains("defaults: overload", java, "connect(String host)");
        assertContains("defaults: primary", java, "connect(String host, int port)");
    }

    static void testSingleExprFn() {
        var java = transpile("""
            fn doubled(int x): int = x * 2
            doubled(5)
            """);
        assertContains("single_expr: return", java, "return x * 2");
    }

    static void testVarArgs() {
        var java = transpile("""
            fn sum(int... nums): int { return 0 }
            sum(1, 2, 3)
            """);
        assertContains("varargs: int...", java, "int... nums");
    }

    static void testConstField() {
        var java = transpile("""
            class Config {
                const String VERSION = "1.0"
            }
            """);
        assertContains("const: public static final", java, "public static final");
        assertContains("const: VERSION", java, "VERSION");
    }

    static void testForWithIndex() {
        var java = transpile("""
            for k, v in map {
                print(k)
            }
            """);
        assertContains("for_idx: entrySet", java, "entrySet");
        assertContains("for_idx: getKey", java, "getKey");
        assertContains("for_idx: getValue", java, "getValue");
    }

    static void testInheritance() {
        var java = transpile("""
            interface Speaker {
                fn speak(): String
            }
            class Dog : Speaker {
                pub fn speak(): String { return "woof" }
            }
            """);
        assertContains("inherit: implements", java, "implements Speaker");
        assertNotContains("inherit: no throws", java, "throws Exception");
    }

    static void testDataClassToString() {
        // Records get toString for free — no need to generate
        var java = transpile("data Point(int x, int y)");
        assertContains("data_str: record", java, "record Point(int x, int y)");
    }

    static void testPrimitiveEquality() {
        // Primitives: use Java ==, not Objects.equals
        var java = transpile("var x = 5 == 5\nvar y = 3.14 == 3.14");
        assertNotContains("prim_eq: no Objects.equals for int", java, "Objects.equals(5, 5)");
        assertContains("prim_eq: int ==", java, "5 == 5");

        // Objects: use Objects.equals
        var java2 = transpile("var x = a == b");
        assertContains("obj_eq: Objects.equals", java2, "Objects.equals(a, b)");
    }

    static void testExpressionLambdaVoidContext() {
        // forEach with print → expression lambda, no return
        var java = transpile("""
            var items = [1, 2, 3]
            items.forEach(x -> print(x))
            """);
        assertNotContains("void_lambda: no return", java, "return System");
        assertContains("void_lambda: expression", java, "System.out.println(x)");
    }

    static void testExpressionLambdaValueContext() {
        // filter with condition → expression lambda returns value
        var java = transpile("""
            var items = [1, 2, 3]
            var evens = items.filter(x -> x % 2 == 0)
            """);
        assertContains("val_lambda: expression", java, "x % 2");
    }

    static void testArrayFieldDefault() {
        var java = transpile("""
            class Config {
                int[] ports = [80, 443]
            }
            """);
        assertContains("arr_field: new int[]", java, "new int[]");
        assertContains("arr_field: 80, 443", java, "80, 443");
    }

    // --- Helpers -------------------------------------------------------------

    static void assertContains(String name, String actual, String expected) {
        if (actual.contains(expected)) {
            passed++;
        } else {
            failed++;
            System.out.println("FAIL: " + name + " — expected to contain \"" + expected + "\"");
        }
    }

    static void assertNotContains(String name, String actual, String unexpected) {
        if (!actual.contains(unexpected)) {
            passed++;
        } else {
            failed++;
            System.out.println("FAIL: " + name + " — expected NOT to contain \"" + unexpected + "\"");
        }
    }
}
