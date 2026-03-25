// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

import static zinc.compiler.TokenType.*;

public class LexerTest {

    static int passed = 0;
    static int failed = 0;

    public static void main(String[] args) {
        testHelloWorld();
        testKeywords();
        testOperators();
        testStringInterpolation();
        testNumbers();
        testFunction();
        testClassDecl();
        testOrHandler();
        testRanges();
        testAnnotation();
        testRawString();
        testLineTracking();

        System.out.println("\nResults: " + passed + " passed, " + failed + " failed");
        if (failed > 0) System.exit(1);
    }

    static void testHelloWorld() {
        var tokens = new Lexer("print(\"Hello, World!\")").tokenize();
        expect("hello_world: print", tokens.get(0).type(), PRINT);
        expect("hello_world: lparen", tokens.get(1).type(), LPAREN);
        expect("hello_world: string", tokens.get(2).type(), STRING_LIT);
        expect("hello_world: string_val", tokens.get(2).literal(), "Hello, World!");
        expect("hello_world: rparen", tokens.get(3).type(), RPAREN);
        expect("hello_world: eof", tokens.get(4).type(), EOF);
    }

    static void testKeywords() {
        var tokens = new Lexer("fn class interface var pub init spawn concurrent parallel for if else match").tokenize();
        expect("kw: fn", tokens.get(0).type(), FN);
        expect("kw: class", tokens.get(1).type(), CLASS);
        expect("kw: interface", tokens.get(2).type(), INTERFACE);
        expect("kw: var", tokens.get(3).type(), VAR);
        expect("kw: pub", tokens.get(4).type(), PUB);
        expect("kw: init", tokens.get(5).type(), INIT);
        expect("kw: spawn", tokens.get(6).type(), SPAWN);
        expect("kw: concurrent", tokens.get(7).type(), CONCURRENT);
        expect("kw: parallel", tokens.get(8).type(), PARALLEL);
        expect("kw: for", tokens.get(9).type(), FOR);
        expect("kw: if", tokens.get(10).type(), IF);
        expect("kw: else", tokens.get(11).type(), ELSE);
        expect("kw: match", tokens.get(12).type(), MATCH);
    }

    static void testOperators() {
        var tokens = new Lexer("+ - * / ** == != < <= > >= -> .. ..= ... && || ?. ??").tokenize();
        expect("op: plus", tokens.get(0).type(), PLUS);
        expect("op: minus", tokens.get(1).type(), MINUS);
        expect("op: star", tokens.get(2).type(), STAR);
        expect("op: slash", tokens.get(3).type(), SLASH);
        expect("op: star_star", tokens.get(4).type(), STAR_STAR);
        expect("op: eq", tokens.get(5).type(), EQ);
        expect("op: neq", tokens.get(6).type(), NEQ);
        expect("op: lt", tokens.get(7).type(), LT);
        expect("op: lte", tokens.get(8).type(), LTE);
        expect("op: gt", tokens.get(9).type(), GT);
        expect("op: gte", tokens.get(10).type(), GTE);
        expect("op: arrow", tokens.get(11).type(), ARROW);
        expect("op: dotdot", tokens.get(12).type(), DOTDOT);
        expect("op: dotdoteq", tokens.get(13).type(), DOTDOTEQ);
        expect("op: dotdotdot", tokens.get(14).type(), DOTDOTDOT);
        expect("op: amp_amp", tokens.get(15).type(), AMP_AMP);
        expect("op: pipe_pipe", tokens.get(16).type(), PIPE_PIPE);
        expect("op: question_dot", tokens.get(17).type(), QUESTION_DOT);
        expect("op: question_question", tokens.get(18).type(), QUESTION_QUESTION);
    }

    static void testStringInterpolation() {
        var tokens = new Lexer("\"Hello, {name}!\"").tokenize();
        expect("interp: type", tokens.get(0).type(), INTERP_STRING);
        expect("interp: val", tokens.get(0).literal(), "Hello, {name}!");
    }

    static void testNumbers() {
        var tokens = new Lexer("42 3.14 0").tokenize();
        expect("num: int", tokens.get(0).type(), INT_LIT);
        expect("num: int_val", tokens.get(0).literal(), "42");
        expect("num: float", tokens.get(1).type(), FLOAT_LIT);
        expect("num: float_val", tokens.get(1).literal(), "3.14");
        expect("num: zero", tokens.get(2).type(), INT_LIT);
    }

    static void testFunction() {
        var tokens = new Lexer("fn add(int a, int b): int { return a + b }").tokenize();
        expect("fn: fn", tokens.get(0).type(), FN);
        expect("fn: name", tokens.get(1).type(), IDENT);
        expect("fn: name_val", tokens.get(1).literal(), "add");
        expect("fn: lparen", tokens.get(2).type(), LPAREN);
        // fn add ( int a , int b ) : int { return ...
        // 0  1   2  3   4 5  6  7 8  9  10 11
        expect("fn: return", tokens.get(12).type(), RETURN);
    }

    static void testClassDecl() {
        var tokens = new Lexer("class Foo : Bar { pub fn greet() { } }").tokenize();
        expect("cls: class", tokens.get(0).type(), CLASS);
        expect("cls: name", tokens.get(1).literal(), "Foo");
        expect("cls: colon", tokens.get(2).type(), COLON);
        expect("cls: parent", tokens.get(3).literal(), "Bar");
        expect("cls: pub", tokens.get(5).type(), PUB);
    }

    static void testOrHandler() {
        var tokens = new Lexer("var x = foo() or { \"default\" }").tokenize();
        expect("or: var", tokens.get(0).type(), VAR);
        // var x = foo ( ) or ...
        // 0   1 2  3  4 5  6
        expect("or: or", tokens.get(6).type(), OR);
    }

    static void testRanges() {
        var tokens = new Lexer("1..5 1..=5").tokenize();
        expect("range: start", tokens.get(0).type(), INT_LIT);
        expect("range: dotdot", tokens.get(1).type(), DOTDOT);
        expect("range: end", tokens.get(2).type(), INT_LIT);
        expect("range: start2", tokens.get(3).type(), INT_LIT);
        expect("range: dotdoteq", tokens.get(4).type(), DOTDOTEQ);
    }

    static void testAnnotation() {
        var tokens = new Lexer("@Override pub fn toString(): String { }").tokenize();
        expect("ann: at", tokens.get(0).type(), AT);
        expect("ann: name", tokens.get(1).literal(), "Override");
    }

    static void testRawString() {
        var tokens = new Lexer("`raw \\n string`").tokenize();
        expect("raw: type", tokens.get(0).type(), RAW_STRING);
        expect("raw: val", tokens.get(0).literal(), "raw \\n string");
    }

    static void testLineTracking() {
        var tokens = new Lexer("a\nb\nc").tokenize();
        expect("line: a", tokens.get(0).line(), 1);
        expect("line: b", tokens.get(1).line(), 2);
        expect("line: c", tokens.get(2).line(), 3);
    }

    // --- Test helpers ---

    static void expect(String name, Object actual, Object expected) {
        if (expected.equals(actual)) {
            passed++;
        } else {
            failed++;
            System.out.println("FAIL: " + name + " — expected " + expected + ", got " + actual);
        }
    }
}
