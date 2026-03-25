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
