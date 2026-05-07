package codegen_go

import (
	"zinc-go/internal/parser"
)

// inferChannelTypes pre-walks a function body before emit. It finds every
// untyped `Channel(N)` declaration (`var ch = Channel(N)` with no type arg
// and no explicit Channel<T> annotation), then walks all subsequent
// statements for `ch.send(x)` sites against those vars. If every send arg
// resolves to the same Go type, the channel's element type is inferred and
// stored in g.inferredChanElem so the constructor emit can pick a typed
// `chan T` instead of falling back to `chan interface{}`.
//
// Conservative on purpose:
//   - Only function-local channels (vars in the function body). Channels
//     stored in fields or top-level slots aren't tracked — those should
//     carry an explicit Channel<T> annotation.
//   - Only `ch.send(x)` shape. `ch <- x` raw send and `for x in ch` recv
//     bindings aren't currently mined; sends are usually plentiful enough
//     to fix the type, and recvs depend on the same type.
//   - Mixed-type sends produce no inference (warning still fires).
//   - The walk ranges into nested blocks (if/while/for/match/spawn/etc.)
//     and into expressions where statements can be embedded (lambdas are
//     handled because their bodies appear as BlockStmts).
func (g *Generator) inferChannelTypes(block *parser.BlockStmt) {
	if block == nil || g.bound == nil {
		return
	}

	// Phase 1: find untyped Channel(N) decls reachable through nested blocks.
	decls := map[string]*parser.CallExpr{}
	g.walkUntypedChannelDecls(block, decls)
	if len(decls) == 0 {
		return
	}

	// Phase 2: walk statements, collect element types observed at send sites.
	observed := map[*parser.CallExpr]map[string]bool{}
	g.walkChannelSends(block, decls, observed)

	// Phase 3: unify per channel — single observed type wins, otherwise skip.
	for call, types := range observed {
		if len(types) != 1 {
			continue
		}
		if g.inferredChanElem == nil {
			g.inferredChanElem = map[*parser.CallExpr]string{}
		}
		for t := range types {
			g.inferredChanElem[call] = t
		}
	}
}

// walkUntypedChannelDecls finds `var x = Channel(N)` in this block and any
// nested blocks, recording {varName → CallExpr} into decls. The CallExpr
// pointer is the same one the codegen will emit later; identity comparison
// at emit time looks up the inferred element type.
func (g *Generator) walkUntypedChannelDecls(block *parser.BlockStmt, decls map[string]*parser.CallExpr) {
	if block == nil {
		return
	}
	for _, s := range block.Stmts {
		if vs, ok := s.(*parser.VarStmt); ok {
			if call, ok := vs.Value.(*parser.CallExpr); ok && isUntypedChannelCall(call) {
				if vs.Type == nil {
					decls[vs.Name] = call
				}
			}
		}
		g.walkUntypedChannelDecls(stmtNestedBlock(s), decls)
		for _, b := range stmtNestedBlocks(s) {
			g.walkUntypedChannelDecls(b, decls)
		}
	}
}

// walkChannelSends finds `ch.send(x)` calls anywhere in the block tree
// where `ch` is a tracked untyped-Channel var. For each such site, the Go
// type of `x` is read from the bind side-map and added to observed[call].
func (g *Generator) walkChannelSends(block *parser.BlockStmt, decls map[string]*parser.CallExpr, observed map[*parser.CallExpr]map[string]bool) {
	if block == nil {
		return
	}
	for _, s := range block.Stmts {
		g.collectSendsInStmt(s, decls, observed)
		g.walkChannelSends(stmtNestedBlock(s), decls, observed)
		for _, b := range stmtNestedBlocks(s) {
			g.walkChannelSends(b, decls, observed)
		}
	}
}

// collectSendsInStmt inspects a single statement for `ch.send(x)` patterns.
// Statement-level: ExprStmt wrapping a CallExpr is the common send form.
// Doesn't recurse into expressions deeply — sends inside nested expressions
// are rare and the warning still covers any miss.
func (g *Generator) collectSendsInStmt(s parser.Stmt, decls map[string]*parser.CallExpr, observed map[*parser.CallExpr]map[string]bool) {
	es, ok := s.(*parser.ExprStmt)
	if !ok {
		return
	}
	call, ok := es.Expr.(*parser.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Callee.(*parser.SelectorExpr)
	if !ok || sel.Field != "send" || len(call.Args) != 1 {
		return
	}
	id, ok := sel.Object.(*parser.Ident)
	if !ok {
		return
	}
	chanCall, found := decls[id.Name]
	if !found {
		return
	}
	t, ok := g.bound.NodeTypes[call.Args[0]]
	if !ok {
		return
	}
	goType := g.goTypeFromV2(t)
	if goType == "" {
		return
	}
	if observed[chanCall] == nil {
		observed[chanCall] = map[string]bool{}
	}
	observed[chanCall][goType] = true
}

// isUntypedChannelCall reports whether a CallExpr is `Channel(N)` or `Chan(N)`
// with no type argument. Used to decide whether the call is a candidate for
// element-type inference.
func isUntypedChannelCall(c *parser.CallExpr) bool {
	if c == nil || len(c.TypeArgs) > 0 {
		return false
	}
	id, ok := c.Callee.(*parser.Ident)
	if !ok {
		return false
	}
	return id.Name == "Channel" || id.Name == "Chan" || id.Name == "channel"
}

// stmtNestedBlock returns the single block contained in a statement, when
// it has exactly one (e.g. for/while/spawn body). Returns nil when the
// statement either has no nested block or has multiple (use stmtNestedBlocks
// for those).
func stmtNestedBlock(s parser.Stmt) *parser.BlockStmt {
	switch st := s.(type) {
	case *parser.WhileStmt:
		return st.Body
	case *parser.ForStmt:
		return st.Body
	case *parser.GoStmt:
		return st.Body
	case *parser.ParallelForStmt:
		return st.Body
	case *parser.WithStmt:
		return st.Body
	}
	return nil
}

// stmtNestedBlocks returns all blocks contained in a statement that has more
// than one (if/else, match cases, select cases). Also extracts blocks from
// ExprStmt-wrapped expressions that carry a body — currently SpawnExpr.
// Used by the channel inference walker to recurse uniformly.
func stmtNestedBlocks(s parser.Stmt) []*parser.BlockStmt {
	switch st := s.(type) {
	case *parser.IfStmt:
		var out []*parser.BlockStmt
		if st.Then != nil {
			out = append(out, st.Then)
		}
		if elseBlock, ok := st.ElseStmt.(*parser.BlockStmt); ok {
			out = append(out, elseBlock)
		}
		// `else if` chains land in ElseStmt as another *IfStmt; recurse.
		if elseIf, ok := st.ElseStmt.(*parser.IfStmt); ok {
			out = append(out, stmtNestedBlocks(elseIf)...)
			if elseIf.Then != nil {
				out = append(out, elseIf.Then)
			}
		}
		return out
	case *parser.MatchStmt:
		var out []*parser.BlockStmt
		for _, c := range st.Cases {
			if c.Body != nil {
				out = append(out, c.Body)
			}
		}
		return out
	case *parser.SelectStmt:
		var out []*parser.BlockStmt
		for _, c := range st.Cases {
			if c.Body != nil {
				out = append(out, c.Body)
			}
		}
		if st.Default != nil {
			out = append(out, st.Default)
		}
		return out
	case *parser.ExprStmt:
		// `spawn { body }` parses as ExprStmt{SpawnExpr{Body}}; recurse so
		// channel sends inside the spawn body are visible to inference.
		if spawn, ok := st.Expr.(*parser.SpawnExpr); ok && spawn.Body != nil {
			return []*parser.BlockStmt{spawn.Body}
		}
	}
	return nil
}
