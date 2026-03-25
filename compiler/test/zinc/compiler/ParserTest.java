// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import static zinc.compiler.Ast.*;

public class ParserTest {

    static int passed = 0;
    static int failed = 0;

    public static void main(String[] args) {
        testHelloWorld();
        testFunction();
        testClassDecl();
        testInterface();
        testDataClass();
        testEnum();
        testVarDecl();
        testIfElse();
        testForIn();
        testWhile();
        testMatch();
        testOrHandler();
        testLambda();
        testMultiParamLambda();
        testImports();
        testBinaryExpr();
        testMethodCall();
        testNewExpr();
        testListLit();
        testMapLit();
        testStringInterp();
        testSpawn();
        testParallelFor();
        testConcurrent();
        testSealedClass();
        testAnnotation();

        System.out.println("\nResults: " + passed + " passed, " + failed + " failed");
        if (failed > 0) System.exit(1);
    }

    static Program parse(String src) {
        var tokens = new Lexer(src).tokenize();
        var parser = new Parser(tokens);
        var prog = parser.parse();
        if (!parser.errors().isEmpty()) {
            System.out.println("  Parse errors: " + parser.errors());
        }
        return prog;
    }

    // --- Tests ---------------------------------------------------------------

    static void testHelloWorld() {
        var prog = parse("print(\"Hello, World!\")");
        expect("hello: 1 stmt", prog.stmts().size(), 1);
        expect("hello: is ExprStmt", prog.stmts().getFirst() instanceof ExprStmt, true);
    }

    static void testFunction() {
        var prog = parse("fn add(int a, int b): int { return a + b }");
        expect("fn: 1 decl", prog.decls().size(), 1);
        var fn = (FnDecl) prog.decls().getFirst();
        expect("fn: name", fn.name(), "add");
        expect("fn: 2 params", fn.params().size(), 2);
        expect("fn: return type", ((SimpleType) fn.returnType()).name(), "int");
        expect("fn: body has 1 stmt", fn.body().stmts().size(), 1);
    }

    static void testClassDecl() {
        var prog = parse("""
            class Dog : Animal {
                init String name
                var int age = 0

                init(String name) {
                    this.name = name
                }

                pub fn bark(): String {
                    return "Woof!"
                }
            }
            """);
        expect("cls: 1 decl", prog.decls().size(), 1);
        var cls = (ClassDecl) prog.decls().getFirst();
        expect("cls: name", cls.name(), "Dog");
        expect("cls: parent", cls.parents().getFirst(), "Animal");
        expect("cls: 2 fields", cls.fields().size(), 2);
        expect("cls: 1 ctor", cls.ctors().size(), 1);
        expect("cls: 1 method", cls.methods().size(), 1);
        expect("cls: method name", cls.methods().getFirst().name(), "bark");
    }

    static void testInterface() {
        var prog = parse("""
            interface Speaker {
                fn speak(): String
                fn volume(): int
            }
            """);
        var iface = (InterfaceDecl) prog.decls().getFirst();
        expect("iface: name", iface.name(), "Speaker");
        expect("iface: 2 methods", iface.methods().size(), 2);
        expect("iface: method1", iface.methods().getFirst().name(), "speak");
    }

    static void testDataClass() {
        var prog = parse("data Point(int x, int y)");
        var dc = (DataClassDecl) prog.decls().getFirst();
        expect("data: name", dc.name(), "Point");
        expect("data: 2 params", dc.params().size(), 2);
    }

    static void testEnum() {
        var prog = parse("enum Color { Red, Green, Blue }");
        var en = (EnumDecl) prog.decls().getFirst();
        expect("enum: name", en.name(), "Color");
        expect("enum: 3 variants", en.variants().size(), 3);
    }

    static void testVarDecl() {
        var prog = parse("var x = 42");
        var stmt = (VarStmt) prog.stmts().getFirst();
        expect("var: name", stmt.name(), "x");
        expect("var: value is IntLit", stmt.value() instanceof IntLit, true);
    }

    static void testIfElse() {
        var prog = parse("if x > 0 { print(\"pos\") } else { print(\"neg\") }");
        var stmt = (IfStmt) prog.stmts().getFirst();
        expect("if: has then", stmt.then().stmts().size(), 1);
        expect("if: has else", stmt.elseStmt() != null, true);
    }

    static void testForIn() {
        var prog = parse("for item in items { print(item) }");
        var stmt = (ForStmt) prog.stmts().getFirst();
        expect("for: is range", stmt.isRange(), true);
        expect("for: item", stmt.item(), "item");
    }

    static void testWhile() {
        var prog = parse("while running { process() }");
        var stmt = (WhileStmt) prog.stmts().getFirst();
        expect("while: has cond", stmt.cond() instanceof Ident, true);
        expect("while: has body", stmt.body().stmts().size(), 1);
    }

    static void testMatch() {
        var prog = parse("""
            match x {
                case 1 { print("one") }
                case 2 { print("two") }
                case _ { print("other") }
            }
            """);
        var stmt = (MatchStmt) prog.stmts().getFirst();
        expect("match: 3 cases", stmt.cases().size(), 3);
    }

    static void testOrHandler() {
        var prog = parse("var x = risky() or { \"default\" }");
        var stmt = (VarStmt) prog.stmts().getFirst();
        expect("or: has handler", stmt.orHandler() != null, true);
        expect("or: handler has body", stmt.orHandler().body() != null, true);
    }

    static void testLambda() {
        var prog = parse("var f = x -> x * 2");
        var stmt = (VarStmt) prog.stmts().getFirst();
        expect("lambda: value is lambda", stmt.value() instanceof LambdaExpr, true);
        var lam = (LambdaExpr) stmt.value();
        expect("lambda: 1 param", lam.params().size(), 1);
    }

    static void testMultiParamLambda() {
        var prog = parse("var f = (a, b) -> a + b");
        var stmt = (VarStmt) prog.stmts().getFirst();
        var lam = (LambdaExpr) stmt.value();
        expect("mlambda: 2 params", lam.params().size(), 2);
    }

    static void testImports() {
        var prog = parse("""
            import java.util.List
            import java.nio.file.Files
            print("done")
            """);
        expect("import: 2 imports", prog.imports().size(), 2);
        expect("import: path", prog.imports().getFirst().path(), "java.util.List");
    }

    static void testBinaryExpr() {
        var prog = parse("var x = 1 + 2 * 3");
        var stmt = (VarStmt) prog.stmts().getFirst();
        // Should be 1 + (2 * 3) due to precedence
        var bin = (BinaryExpr) stmt.value();
        expect("binop: top is +", bin.op(), "+");
        expect("binop: right is *", ((BinaryExpr) bin.right()).op(), "*");
    }

    static void testMethodCall() {
        var prog = parse("list.add(42)");
        var stmt = (ExprStmt) prog.stmts().getFirst();
        var call = (CallExpr) stmt.expr();
        var sel = (SelectorExpr) call.callee();
        expect("call: object", ((Ident) sel.object()).name(), "list");
        expect("call: field", sel.field(), "add");
        expect("call: 1 arg", call.args().size(), 1);
    }

    static void testNewExpr() {
        var prog = parse("var x = new ArrayList(10)");
        var stmt = (VarStmt) prog.stmts().getFirst();
        var call = (CallExpr) stmt.value();
        expect("new: isNew", call.isNew(), true);
        expect("new: callee", ((Ident) call.callee()).name(), "ArrayList");
    }

    static void testListLit() {
        var prog = parse("var items = [1, 2, 3]");
        var stmt = (VarStmt) prog.stmts().getFirst();
        var list = (ListLit) stmt.value();
        expect("list: 3 elements", list.elements().size(), 3);
    }

    static void testMapLit() {
        var prog = parse("var m = {\"a\": 1, \"b\": 2}");
        var stmt = (VarStmt) prog.stmts().getFirst();
        var map = (MapLit) stmt.value();
        expect("map: 2 keys", map.keys().size(), 2);
    }

    static void testStringInterp() {
        var prog = parse("print(\"Hello, {name}!\")");
        var stmt = (ExprStmt) prog.stmts().getFirst();
        var call = (CallExpr) stmt.expr();
        var interp = (StringInterpLit) call.args().getFirst();
        expect("interp: 3 parts", interp.parts().size(), 3);
    }

    static void testSpawn() {
        var prog = parse("""
            var t = spawn {
                print("working")
            } or {
                print("failed")
            }
            """);
        var stmt = (VarStmt) prog.stmts().getFirst();
        var spawn = (SpawnExpr) stmt.value();
        expect("spawn: has body", spawn.body().stmts().size(), 1);
        expect("spawn: has or", spawn.orHandler() != null, true);
    }

    static void testParallelFor() {
        var prog = parse("parallel(max: 4) for item in items { process(item) }");
        var stmt = (ParallelForStmt) prog.stmts().getFirst();
        expect("pfor: item", stmt.item(), "item");
        expect("pfor: max", stmt.max(), 4);
    }

    static void testConcurrent() {
        var prog = parse("""
            concurrent {
                fetchUser(id)
                fetchOrders(id)
            }
            """);
        var stmt = (ConcurrentStmt) prog.stmts().getFirst();
        expect("concurrent: 2 tasks", stmt.tasks().size(), 2);
    }

    static void testSealedClass() {
        var prog = parse("""
            sealed class Shape {
                data Circle(double radius)
                data Rect(double w, double h)
            }
            """);
        // Sealed class parsed as class with nested data decls
        expect("sealed: 1 decl", prog.decls().size(), 1);
    }

    static void testAnnotation() {
        var prog = parse("""
            @Controller
            class Foo {
                @Get("/hello")
                pub fn hello(): String {
                    return "hello"
                }
            }
            """);
        var cls = (ClassDecl) prog.decls().getFirst();
        expect("ann: class has 1 annotation", cls.annotations().size(), 1);
        expect("ann: class annotation name", cls.annotations().getFirst().name(), "Controller");
        expect("ann: method has annotation", cls.methods().getFirst().annotations().size(), 1);
    }

    // --- Helpers -------------------------------------------------------------

    static void expect(String name, Object actual, Object expected) {
        if (expected.equals(actual)) {
            passed++;
        } else {
            failed++;
            System.out.println("FAIL: " + name + " — expected " + expected + ", got " + actual);
        }
    }
}
