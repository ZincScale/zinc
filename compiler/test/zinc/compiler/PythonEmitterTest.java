// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

public class PythonEmitterTest {

    static int passed = 0;
    static int failed = 0;

    public static void main(String[] args) {
        testHelloWorld();
        testFunction();
        testReturnError();
        testClassDecl();
        testDataClass();
        testSealedClass();
        testVarAndArithmetic();
        testIfElse();
        testForIn();
        testForWithIndex();
        testWhile();
        testMatch();
        testOrHandler();
        testStringInterp();
        testListLit();
        testMapLit();
        testStreamChain();
        testStreamIt();
        testLambda();
        testEquality();
        testSpawn();
        testSpawnOr();
        testSpawnJoin();
        testLock();
        testSleep();
        testChannel();
        testInOperator();
        testDefaultParams();

        System.out.println("\nResults: " + passed + " passed, " + failed + " failed");
        if (failed > 0) System.exit(1);
    }

    static String transpile(String zinc) {
        var tokens = new Lexer(zinc).tokenize().unwrap();
        var program = new Parser(tokens).parse();
        return new PythonEmitter("Test").render(program);
    }

    // --- Basic ---

    static void testHelloWorld() {
        var py = transpile("print(\"Hello, World!\")");
        assertContains("hello: print", py, "print(\"Hello, World!\")");
        assertContains("hello: main", py, "def main():");
        assertContains("hello: entry", py, "if __name__");
    }

    static void testFunction() {
        var py = transpile("""
            fn add(int a, int b): int {
                return a + b
            }
            """);
        assertContains("fn: def", py, "def add(a: int, b: int) -> int:");
        assertContains("fn: return", py, "return (a + b)");
    }

    static void testReturnError() {
        var py = transpile("""
            fn fail(): int {
                return Error("bad")
            }
            """);
        assertContains("err: raise", py, "raise ZincError(\"bad\")");
        assertContains("err: import", py, "from .zinc_runtime import ZincError");
        assertNotContains("err: no return Error", py, "return Error");
    }

    // --- Classes ---

    static void testClassDecl() {
        var py = transpile("""
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
        assertContains("class: Dog", py, "class Dog:");
        assertContains("class: init", py, "def __init__");
        assertContains("class: self", py, "self._name = name");
        assertContains("class: method", py, "def bark(self)");
    }

    static void testDataClass() {
        var py = transpile("data Point(int x, int y)");
        assertContains("data: dataclass", py, "@dataclass");
        assertContains("data: frozen", py, "frozen=True");
        assertContains("data: class", py, "class Point:");
        assertContains("data: field x", py, "x: int");
        assertContains("data: field y", py, "y: int");
    }

    static void testSealedClass() {
        var py = transpile("""
            sealed class Shape {
                data Circle(double radius)
                data Rect(double w, double h)
            }
            """);
        assertContains("sealed: base", py, "class Shape:");
        assertContains("sealed: Circle", py, "class Circle(Shape):");
        assertContains("sealed: Rect", py, "class Rect(Shape):");
    }

    // --- Control flow ---

    static void testVarAndArithmetic() {
        var py = transpile("var x = 10\nvar y = x + 5\nprint(y)");
        assertContains("var: assign", py, "x = 10");
        assertContains("var: expr", py, "y = (x + 5)");
    }

    static void testIfElse() {
        var py = transpile("""
            var x = 10
            if x > 5 {
                print("big")
            } else {
                print("small")
            }
            """);
        assertContains("if: cond", py, "if (x > 5):");
        assertContains("if: else", py, "else:");
    }

    static void testForIn() {
        var py = transpile("for x in items { print(x) }");
        assertContains("for: in", py, "for x in items:");
    }

    static void testForWithIndex() {
        // Two-variable for-in is map iteration (matches Java .entrySet())
        var py = transpile("for k, v in items { print(k) }");
        assertContains("for-kv: items()", py, "for k, v in items.items():");
    }

    static void testWhile() {
        var py = transpile("while true { print(\"loop\") }");
        assertContains("while: cond", py, "while True:");
    }

    static void testMatch() {
        var py = transpile("""
            var x = 1
            match x {
                case 1 { print("one") }
                case 2 { print("two") }
            }
            """);
        assertContains("match: keyword", py, "match x:");
        assertContains("match: case", py, "case 1:");
    }

    // --- Error handling ---

    static void testOrHandler() {
        var py = transpile("var x = riskyCall() or 0");
        assertContains("or: try", py, "try:");
        assertContains("or: except", py, "except");
    }

    // --- Expressions ---

    static void testStringInterp() {
        var py = transpile("var name = \"world\"\nprint(\"hello {name}\")");
        assertContains("interp: fstring", py, "f\"hello {name}\"");
    }

    static void testListLit() {
        var py = transpile("var items = [1, 2, 3]");
        assertContains("list: literal", py, "[1, 2, 3]");
    }

    static void testMapLit() {
        var py = transpile("var m = {\"a\": 1, \"b\": 2}");
        assertContains("map: literal", py, "{\"a\": 1, \"b\": 2}");
    }

    // --- Collections ---

    static void testStreamChain() {
        var py = transpile("var result = items.filter(x -> x > 0).map(x -> x * 2)");
        assertContains("stream: comprehension", py, "for");
        assertContains("stream: filter", py, "if");
    }

    static void testStreamIt() {
        var py = transpile("var result = items.filter(it > 0)");
        assertContains("it: comprehension", py, "[_x for _x in items if (_x > 0)]");
    }

    static void testLambda() {
        var py = transpile("var f = (x) -> x * 2");
        assertContains("lambda: keyword", py, "lambda x:");
    }

    static void testEquality() {
        var py = transpile("var a = x == y\nvar b = x === y");
        assertContains("eq: ==", py, "(x == y)");
        assertContains("eq: is", py, "x is y");
    }

    // --- Concurrency ---

    static void testSpawn() {
        var py = transpile("spawn { print(\"bg\") }");
        assertContains("spawn: ZincFuture", py, "ZincFuture(_spawn_0)");
        assertContains("spawn: def", py, "def _spawn_0():");
        assertContains("spawn: import", py, "from .zinc_runtime import ZincFuture");
    }

    static void testSpawnOr() {
        var py = transpile("""
            var x = 0
            spawn {
                x = 42
            } or {
                x = -1
            }
            """);
        assertContains("spawn-or: body", py, "def _spawn_0():");
        assertContains("spawn-or: handler", py, "def _spawn_0_or():");
        assertContains("spawn-or: nonlocal", py, "nonlocal x");
        assertContains("spawn-or: call", py, "ZincFuture(_spawn_0, _spawn_0_or)");
    }

    static void testSpawnJoin() {
        var py = transpile("""
            var t = spawn { print("work") }
            t.join()
            print(t.isDone())
            print(t.isFailed())
            """);
        assertContains("join: call", py, "t.join()");
        assertContains("join: isDone", py, "t.isDone()");
        assertContains("join: isFailed", py, "t.isFailed()");
    }

    static void testLock() {
        var py = transpile("""
            var mu = new Lock()
            lock mu { print("critical") }
            """);
        assertContains("lock: threading", py, "threading.Lock()");
        assertContains("lock: with", py, "with mu:");
    }

    static void testSleep() {
        var py = transpile("sleep(100)");
        assertContains("sleep: call", py, "zinc_sleep(100)");
        assertContains("sleep: import", py, "from .zinc_runtime import zinc_sleep");
    }

    static void testChannel() {
        var py = transpile("Channel<String> ch = new Channel(10)");
        assertContains("chan: type", py, "ZincChannel");
        assertContains("chan: import", py, "from .zinc_runtime import ZincChannel");
    }

    static void testInOperator() {
        var py = transpile("var found = \"a\" in items");
        assertContains("in: operator", py, "\"a\" in items");
    }

    static void testDefaultParams() {
        var py = transpile("fn greet(String name = \"World\"): String { return \"Hi {name}\" }");
        assertContains("default: param", py, "name: str = \"World\"");
    }

    // --- Helpers ---

    static void assertContains(String name, String actual, String expected) {
        if (actual.contains(expected)) {
            passed++;
        } else {
            failed++;
            System.out.println("FAIL: " + name + " — expected to contain \"" + expected + "\"");
            System.out.println("  actual output:\n" + actual.lines().limit(10).reduce("", (a, b) -> a + "    " + b + "\n"));
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
