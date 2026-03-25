// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package zinc.compiler;

public record Token(TokenType type, String literal, int line, int col) {

    @Override
    public String toString() {
        return type + "(" + literal + ") at " + line + ":" + col;
    }
}
