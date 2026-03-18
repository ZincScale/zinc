---
name: No pointer syntax in Zinc
description: Never expose pointer concepts (*Type, &, address-of) in Zinc code — pointers are a Go implementation detail
type: feedback
---

Never use pointer syntax (`*Type`, `&`, address-of) anywhere in Zinc user-facing code, docs, examples, or design docs. Pointers are a Go implementation detail that the transpiler handles internally.

**Why:** Zinc targets OO developers from Java/C#/Kotlin/Python who think in terms of objects, not pointers. Exposing Go pointer semantics violates the abstraction.

**How to apply:** When designing features that interact with Go pointer types (like pointer inference for Go type construction), all inference must be automatic and invisible. The transpiler decides `&` vs value based on context — the user never sees or writes pointer syntax.
