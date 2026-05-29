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

package pyfront

import (
	"strings"

	"zinc-go/internal/parser"
)

// parseFString lowers an f-string token into a StringInterpLit whose
// interpolated parts are routed through zincpyStr/zincpyRepr so floats and
// bools render the Python way (3.0 not 3, True not true). Literal `{{` / `}}`
// collapse to single braces; each `{expr}` field is parsed as a Python
// expression in the current scope.
func (p *Parser) parseFString(t Token) parser.Expr {
	raw := t.Value
	var parts []parser.Expr
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, &parser.StringLit{Value: lit.String()})
			lit.Reset()
		}
	}

	i := 0
	for i < len(raw) {
		c := raw[i]
		switch {
		case c == '{' && i+1 < len(raw) && raw[i+1] == '{':
			lit.WriteByte('{')
			i += 2
		case c == '}' && i+1 < len(raw) && raw[i+1] == '}':
			lit.WriteByte('}')
			i += 2
		case c == '{':
			j := matchBrace(raw, i)
			if j < 0 {
				p.errf(t, "unterminated '{' in f-string")
				return &parser.StringLit{Value: raw}
			}
			flush()
			parts = append(parts, p.fstringField(t, raw[i+1:j]))
			i = j + 1
		case c == '}':
			p.errf(t, "single '}' is not allowed in f-string")
			i++
		default:
			lit.WriteByte(c)
			i++
		}
	}
	flush()

	if len(parts) == 0 {
		return &parser.StringLit{Value: ""}
	}
	// A field-free f-string is just a plain string literal.
	if len(parts) == 1 {
		if sl, ok := parts[0].(*parser.StringLit); ok {
			return sl
		}
	}
	return &parser.StringInterpLit{Parts: parts}
}

// matchBrace returns the index of the `}` that closes the `{` at position
// open, accounting for nested brackets/parens and string literals inside the
// field expression. Returns -1 if unbalanced.
func matchBrace(s string, open int) int {
	depth := 0
	var quote byte
	for i := open + 1; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '{', '(', '[':
			depth++
		case ')', ']':
			depth--
		case '}':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// fstringField builds the interpolation expression for one `{...}` field. It
// strips an optional `!r`/`!s` conversion and rejects `:format` specifiers
// (not yet supported), then parses the remaining text as a Python expression.
func (p *Parser) fstringField(t Token, src string) parser.Expr {
	exprSrc, conv, spec, hasSpec := splitFStringField(src)
	if hasSpec && strings.TrimSpace(spec) != "" {
		p.errf(t, "f-string format specifier (:%s) is not yet supported", spec)
	}
	e := p.subExpr(t, exprSrc)
	switch conv {
	case "r":
		return callIdent("zincpyRepr", e)
	case "", "s":
		return callIdent("zincpyStr", e)
	default:
		p.errf(t, "unsupported f-string conversion !%s", conv)
		return callIdent("zincpyStr", e)
	}
}

// splitFStringField separates an f-string field into its expression text, an
// optional `!conversion`, and an optional `:format-spec`. Conversions and
// specs are recognized only at brace/paren/bracket depth zero and outside
// string literals, so `x[1:2]`, `{a: b}` dict literals, and `a != b` are not
// mistaken for them.
func splitFStringField(src string) (expr, conv, spec string, hasSpec bool) {
	depth := 0
	var quote byte
	for i := 0; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '!':
			// `!=` is a comparison, not a conversion.
			if depth == 0 && !(i+1 < len(src) && src[i+1] == '=') {
				rest := src[i+1:]
				if k := strings.IndexByte(rest, ':'); k >= 0 {
					return src[:i], rest[:k], rest[k+1:], true
				}
				return src[:i], rest, "", false
			}
		case ':':
			if depth == 0 {
				return src[:i], "", src[i+1:], true
			}
		}
	}
	return src, "", "", false
}

// subExpr parses src as a standalone Python expression, sharing the current
// parser's type scope and flags so identifier types resolve and `self` works.
func (p *Parser) subExpr(t Token, src string) parser.Expr {
	lx := New(src)
	toks := lx.Tokenize()
	if len(lx.Errors) > 0 {
		p.errf(t, "f-string expression %q: %s", src, lx.Errors[0])
		return &parser.StringLit{Value: ""}
	}
	sub := &Parser{
		toks:         toks,
		scopes:       p.scopes,
		fnRet:        p.fnRet,
		fnRetDict:    p.fnRetDict,
		elemType:     p.elemType,
		dictVars:     p.dictVars,
		dictExprMeta: p.dictExprMeta,
		inMethod:     p.inMethod,
		currentFnRet: p.currentFnRet,
	}
	e := sub.parseExpr()
	p.Errors = append(p.Errors, sub.Errors...)
	return e
}

// pyStrMethods maps a Python str method name to the runtime helper that
// reproduces its semantics. The helper is always called as
// helper(receiver, args...).
var pyStrMethods = map[string]string{
	"upper":      "zincpyUpper",
	"lower":      "zincpyLower",
	"strip":      "zincpyStrip",
	"replace":    "zincpyReplace",
	"startswith": "zincpyStartswith",
	"endswith":   "zincpyEndswith",
	"find":       "zincpyFind",
	"count":      "zincpyCount",
	"split":      "zincpySplit",
	"join":       "zincpyJoin",
}

// lowerStrMethod rewrites `recv.method(args...)` to its runtime helper when
// recv is statically a string. Returns nil when it does not apply, leaving the
// original call intact. For `sep.join(items)` the receiver is the separator,
// so the natural helper(recv, args...) order already matches Python.
func (p *Parser) lowerStrMethod(callee parser.Expr, args []parser.Expr) parser.Expr {
	sel, ok := callee.(*parser.SelectorExpr)
	if !ok || p.typeOf(sel.Object) != tStr {
		return nil
	}
	helper, ok := pyStrMethods[sel.Field]
	if !ok {
		return nil
	}
	callArgs := append([]parser.Expr{sel.Object}, args...)
	return callIdent(helper, callArgs...)
}
