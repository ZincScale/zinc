// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

public enum TokenType {
    // Literals
    INT_LIT, FLOAT_LIT, STRING_LIT, INTERP_STRING, RAW_STRING, BOOL_LIT, NULL, IDENT,

    // Keywords
    CLASS, INTERFACE, CONSTRUCT, NEW, FN, RETURN, IF, ELSE, FOR, WHILE,
    BREAK, CONTINUE, OR, PRINT, VAR, PUB, STATIC, SUPER, THIS,
    IMPORT, AS, IN, ENUM, MATCH, CASE, PACKAGE, CONST, DEFER, IS, WITH,
    DATA, SPAWN, USE, READONLY, OVERRIDE, END, TRY, CATCH, RAISE,
    NOT, AND, FROM, PARALLEL, INIT, CONCURRENT, TIMEOUT, ABSTRACT, SEALED, LOCK,

    // Symbols
    LPAREN, RPAREN, LBRACE, RBRACE, LBRACKET, RBRACKET,
    COMMA, DOT, COLON, SEMICOLON, ASSIGN, AT,
    PLUS, MINUS, STAR, SLASH, PERCENT, BANG,
    AMP_AMP, PIPE_PIPE, STAR_STAR,
    EQ, NEQ, REF_EQ, REF_NEQ, LT, LTE, GT, GTE,
    PLUS_EQ, MINUS_EQ, STAR_EQ, SLASH_EQ,
    ARROW, QUESTION, QUESTION_DOT, QUESTION_QUESTION,
    DOTDOT, DOTDOTEQ, DOTDOTDOT, COLONASSIGN,

    EOF, ILLEGAL
}
