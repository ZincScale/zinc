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

        System.out.println("\nResults: " + passed + " passed, " + failed + " failed");
        if (failed > 0) System.exit(1);
    }

    static String transpile(String zinc) {
        var tokens = new Lexer(zinc).tokenize().unwrap();
        var program = new Parser(tokens).parse();
        var result = new Transformer().transform(program);
        return result.unwrap().toString();
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
        assertContains("data: class", java, "class Point");
        assertContains("data: field x", java, "int x");
        assertContains("data: field y", java, "int y");
        assertContains("data: ctor", java, "Point(int x, int y)");
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
