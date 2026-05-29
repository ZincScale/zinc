// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package pyfront is a Python front-end for the zinc-go compiler. It lexes
// and parses a subset of (valid CPython) Python source into the existing
// zinc parser.Program AST, so the unchanged typechecker + Go codegen can
// consume it. The contract: input is valid Python that also runs under
// CPython; output is the same AST a .zn file would produce.
package pyfront

// TokKind enumerates Python token categories.
type TokKind int

const (
	TEOF TokKind = iota
	TNewline
	TIndent
	TDedent
	TName   // identifiers and keywords
	TNumber // int or float literal
	TString  // string literal (inner text, unquoted)
	TFString // f-string literal (raw inner text, {…} markers intact)
	TOp      // operator or punctuation (text in Value)
)

func (k TokKind) String() string {
	switch k {
	case TEOF:
		return "EOF"
	case TNewline:
		return "NEWLINE"
	case TIndent:
		return "INDENT"
	case TDedent:
		return "DEDENT"
	case TName:
		return "NAME"
	case TNumber:
		return "NUMBER"
	case TString:
		return "STRING"
	case TFString:
		return "FSTRING"
	case TOp:
		return "OP"
	}
	return "?"
}

// Token is one lexical unit.
type Token struct {
	Kind  TokKind
	Value string // literal text (NAME/NUMBER/STRING/OP); empty for layout tokens
	Line  int    // 1-indexed source line
	Col   int    // 1-indexed column (start of token)
}
