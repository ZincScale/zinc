import 'ast.dart';

/// Lowers the imperative AST to Erlang module sources (module name -> source).
/// A program with no actors produces a single 'main' module; actors will add
/// one gen_server module each plus a supervisor (Phase 1).
/// Lowerings (validated in beam-lab/LOWERING_SPEC.md):
///  - mutable locals -> SSA; mutated-across-loop -> threaded accumulators
///  - for/while -> direct tail-recursive helper (free vars captured, mutated threaded)
///  - if that mutates -> case returning a tuple of the mutated vars (phi)
///  - early return -> try / throw({'$ret',V}) / catch
///  - break/continue -> loop-scoped throw({'$brk'|'$cont', MutTuple}) caught by the helper
///  - struct -> map; field read -> maps:get; field set -> functional map update
///  - array -> list; index read -> lists:nth; len -> length
///  - string -> binary; interpolation -> '$fmt'-formatted binary segments
class CodeGen {
  final List<FnDecl> fns;
  int _ctr = 0;
  final List<String> _helpers = [];

  CodeGen(this.fns);

  static const _fmtHelper = "'\$fmt'(X) when is_binary(X) -> X;\n"
      "'\$fmt'(X) when is_integer(X) -> integer_to_binary(X);\n"
      "'\$fmt'(X) -> iolist_to_binary(io_lib:format(\"~p\", [X])).";

  String fresh(String base) {
    final cap = base.isEmpty ? 'V' : base[0].toUpperCase() + base.substring(1);
    return '${cap}_${_ctr++}';
  }

  String _fnName(String src) => src == 'main' ? 'user_main' : src;

  Map<String, String> generateModules() {
    final defs = fns.map(_genFn).toList();
    final body = [...defs, ..._helpers, _fmtHelper].join('\n\n');
    final main = '-module(main).\n'
        '-export([main/0]).\n'
        '-compile([nowarn_unused_vars, nowarn_unused_function]).\n\n'
        'main() -> user_main().\n\n$body\n';
    return {'main': main};
  }

  String _genFn(FnDecl fn) {
    final env = <String, String>{};
    final params = <String>[];
    for (final p in fn.params) {
      final v = fresh(p);
      env[p] = v;
      params.add(v);
    }
    final stmts = _genStmts(fn.body.stmts, env, topLevel: true, loopMut: null);
    final bodyStr = stmts.join(',\n        ');
    final head = '${_fnName(fn.name)}(${params.join(', ')})';
    if (_needsThrow(fn.body)) {
      return "$head ->\n    try\n        $bodyStr\n    catch throw:{'\$ret', V} -> V end.";
    }
    return '$head ->\n        $bodyStr.';
  }

  List<String> _genStmts(List<Stmt> stmts, Map<String, String> env,
      {required bool topLevel, required List<String>? loopMut}) {
    final out = <String>[];
    for (var i = 0; i < stmts.length; i++) {
      final s = stmts[i];
      final last = i == stmts.length - 1;
      switch (s) {
        case VarStmt(:final name, :final init):
          final v = fresh(name);
          out.add('$v = ${_genExpr(init, env)}');
          env[name] = v;
          break;
        case AssignStmt(:final name, :final op, :final value):
          final cur = env[name]!;
          final rhs = switch (op) {
            '=' => _genExpr(value, env),
            '+=' => '$cur + ${_genExpr(value, env)}',
            '-=' => '$cur - ${_genExpr(value, env)}',
            '*=' => '$cur * ${_genExpr(value, env)}',
            _ => throw FormatException('bad assign op $op'),
          };
          final v = fresh(name);
          out.add('$v = $rhs');
          env[name] = v;
          break;
        case FieldAssignStmt(:final objVar, :final field, :final op, :final value):
          final cur = env[objVar]!;
          final fe = switch (op) {
            '=' => _genExpr(value, env),
            '+=' => 'maps:get($field, $cur) + ${_genExpr(value, env)}',
            '-=' => 'maps:get($field, $cur) - ${_genExpr(value, env)}',
            '*=' => 'maps:get($field, $cur) * ${_genExpr(value, env)}',
            _ => throw FormatException('bad assign op $op'),
          };
          final v = fresh(objVar);
          out.add('$v = $cur#{$field := $fe}');
          env[objVar] = v;
          break;
        case ReturnStmt(:final value):
          final e = value == null ? 'ok' : _genExpr(value, env);
          out.add((topLevel && last) ? e : "throw({'\$ret', $e})");
          break;
        case BreakStmt():
          if (loopMut == null) {
            throw FormatException('break outside loop');
          }
          out.add("throw({'\$brk', ${_loopTuple(loopMut, env)}})");
          break;
        case ContinueStmt():
          if (loopMut == null) {
            throw FormatException('continue outside loop');
          }
          out.add("throw({'\$cont', ${_loopTuple(loopMut, env)}})");
          break;
        case ExprStmt(:final expr):
          out.add(_genExpr(expr, env));
          break;
        case IfStmt it:
          out.add(_genIf(it, env, loopMut));
          break;
        case ForRangeStmt it:
          out.add(_genForRange(it, env));
          break;
        case ForEachStmt it:
          out.add(_genForEach(it, env));
          break;
        case WhileStmt it:
          out.add(_genWhile(it, env));
          break;
      }
    }
    return out;
  }

  String _loopTuple(List<String> mut, Map<String, String> env) =>
      _tupleOf(mut.map((m) => env[m]!).toList());

  String _genIf(IfStmt s, Map<String, String> env, List<String>? loopMut) {
    final cond = _genExpr(s.cond, env);
    final assigned = <String>{};
    _collectAssigned(s.then_, assigned);
    if (s.else_ != null) _collectAssigned(s.else_!, assigned);
    final phi = assigned.where(env.containsKey).toList();

    final thenEnv = Map<String, String>.from(env);
    final thenCode = _genStmts(s.then_.stmts, thenEnv, topLevel: false, loopMut: loopMut);
    final thenJump = _endsInJump(s.then_);

    List<String> elseCode;
    Map<String, String> elseEnv;
    bool elseJump;
    if (s.else_ != null) {
      elseEnv = Map<String, String>.from(env);
      elseCode = _genStmts(s.else_!.stmts, elseEnv, topLevel: false, loopMut: loopMut);
      elseJump = _endsInJump(s.else_!);
    } else {
      elseEnv = env;
      elseCode = [];
      elseJump = false;
    }

    if (phi.isEmpty) {
      final t = thenCode.isEmpty ? 'ok' : thenCode.join(', ');
      final e = elseCode.isEmpty ? 'ok' : elseCode.join(', ');
      return 'case $cond of true -> $t; false -> $e end';
    }

    String arm(List<String> code, Map<String, String> benv, bool jump) {
      if (jump) return code.join(', ');
      final vals = phi.map((v) => benv[v]!).toList();
      final tup = vals.length == 1 ? vals[0] : '{${vals.join(', ')}}';
      return [...code, tup].join(', ');
    }

    final tArm = arm(thenCode, thenEnv, thenJump);
    final eArm = arm(elseCode, elseEnv, elseJump);
    final newNames = phi.map(fresh).toList();
    final lhs = newNames.length == 1 ? newNames[0] : '{${newNames.join(', ')}}';
    for (var i = 0; i < phi.length; i++) {
      env[phi[i]] = newNames[i];
    }
    return '$lhs = case $cond of true -> $tArm; false -> $eArm end';
  }

  String _genForRange(ForRangeStmt s, Map<String, String> env) {
    final startCode = _genExpr(s.start, env);
    final endCode = _genExpr(s.end, env);
    final mut = _mutated(s.body, env);
    final free = _freeVars(s.body, env, {s.varName, ...mut});

    final helper = 'loop_${_ctr++}';
    final iVar = fresh(s.varName);
    final endVar = fresh('end');
    final freeIn = {for (final f in free) f: fresh(f)};
    final mutIn = {for (final m in mut) m: fresh(m)};

    final benv = <String, String>{s.varName: iVar};
    for (final f in free) benv[f] = freeIn[f]!;
    for (final m in mut) benv[m] = mutIn[m]!;
    final bodyCode = _genStmts(s.body.stmts, benv, topLevel: false, loopMut: mut);
    final mutOut = mut.map((m) => benv[m]!).toList();

    final freeP = free.map((f) => freeIn[f]!).toList();
    final head1 = [iVar, endVar, ...freeP, ...mut.map((m) => mutIn[m]!)];
    final recursePrefix = ['$iVar + 1', endVar, ...freeP];
    final base = [
      '_$iVar',
      '_$endVar',
      ...free.map((f) => '_${freeIn[f]}'),
      ...mut.map((m) => mutIn[m]!)
    ];
    final result = _tupleOf(mut.map((m) => mutIn[m]!).toList());
    final clauseBody = _loopClauseBody(s.body, bodyCode, mutOut, mut, recursePrefix, helper);
    _helpers.add('$helper(${head1.join(', ')}) when $iVar < $endVar ->\n'
        '        $clauseBody;\n'
        '$helper(${base.join(', ')}) ->\n        $result.');

    final callArgs = [
      startCode,
      endCode,
      ...free.map((f) => env[f]!),
      ...mut.map((m) => env[m]!)
    ];
    return _bindLoop('$helper(${callArgs.join(', ')})', mut, env);
  }

  String _genForEach(ForEachStmt s, Map<String, String> env) {
    final listCode = _genExpr(s.iterable, env);
    final mut = _mutated(s.body, env);
    final free = _freeVars(s.body, env, {s.varName, ...mut});

    final helper = 'loop_${_ctr++}';
    final elemVar = fresh(s.varName);
    final restVar = fresh('rest');
    final freeIn = {for (final f in free) f: fresh(f)};
    final mutIn = {for (final m in mut) m: fresh(m)};

    final benv = <String, String>{s.varName: elemVar};
    for (final f in free) benv[f] = freeIn[f]!;
    for (final m in mut) benv[m] = mutIn[m]!;
    final bodyCode = _genStmts(s.body.stmts, benv, topLevel: false, loopMut: mut);
    final mutOut = mut.map((m) => benv[m]!).toList();

    final freeP = free.map((f) => freeIn[f]!).toList();
    final head1 = ['[$elemVar | $restVar]', ...freeP, ...mut.map((m) => mutIn[m]!)];
    final recursePrefix = [restVar, ...freeP];
    final base = ['[]', ...free.map((f) => '_${freeIn[f]}'), ...mut.map((m) => mutIn[m]!)];
    final result = _tupleOf(mut.map((m) => mutIn[m]!).toList());
    final clauseBody = _loopClauseBody(s.body, bodyCode, mutOut, mut, recursePrefix, helper);
    _helpers.add('$helper(${head1.join(', ')}) ->\n        $clauseBody;\n'
        '$helper(${base.join(', ')}) ->\n        $result.');

    final callArgs = [listCode, ...free.map((f) => env[f]!), ...mut.map((m) => env[m]!)];
    return _bindLoop('$helper(${callArgs.join(', ')})', mut, env);
  }

  String _genWhile(WhileStmt s, Map<String, String> env) {
    final mut = _mutated(s.body, env);
    final refs = <String>{};
    _exprRefs(s.cond, refs);
    _blockRefs(s.body, refs);
    final free =
        refs.where((v) => env.containsKey(v) && !mut.contains(v)).toList();

    final helper = 'loop_${_ctr++}';
    final freeIn = {for (final f in free) f: fresh(f)};
    final mutIn = {for (final m in mut) m: fresh(m)};
    final benv = <String, String>{};
    for (final f in free) benv[f] = freeIn[f]!;
    for (final m in mut) benv[m] = mutIn[m]!;
    final condCode = _genExpr(s.cond, benv);
    final bodyCode = _genStmts(s.body.stmts, benv, topLevel: false, loopMut: mut);
    final mutOut = mut.map((m) => benv[m]!).toList();

    final freeP = free.map((f) => freeIn[f]!).toList();
    final head1 = [...freeP, ...mut.map((m) => mutIn[m]!)];
    final recursePrefix = [...freeP];
    final result = _tupleOf(mut.map((m) => mutIn[m]!).toList());
    final clauseBody = _loopClauseBody(s.body, bodyCode, mutOut, mut, recursePrefix, helper);
    _helpers.add('$helper(${head1.join(', ')}) ->\n'
        '        case $condCode of\n'
        '            true -> $clauseBody;\n'
        '            false -> $result\n'
        '        end.');

    final callArgs = [...free.map((f) => env[f]!), ...mut.map((m) => env[m]!)];
    return _bindLoop('$helper(${callArgs.join(', ')})', mut, env);
  }

  /// The per-iteration body of a loop helper: either a simple "run body, recurse"
  /// or, if the body has break/continue, a try that catches the loop signals.
  String _loopClauseBody(Block body, List<String> bodyCode, List<String> mutOut,
      List<String> mut, List<String> recursePrefix, String helper) {
    if (!_hasBreakContinue(body)) {
      final rec = '$helper(${[...recursePrefix, ...mutOut].join(', ')})';
      return [...bodyCode, rec].join(',\n        ');
    }
    final sig = fresh('sig');
    final payload = _tupleOf(mutOut);
    final pm = mut.map(fresh).toList();
    final pat = pm.isEmpty ? 'ok' : (pm.length == 1 ? pm[0] : '{${pm.join(', ')}}');
    final recurse = '$helper(${[...recursePrefix, ...pm].join(', ')})';
    final result = pat;
    final joined = bodyCode.join(',\n            ');
    return "$sig = try\n            $joined,\n            {'\$cont', $payload}\n"
        "        catch throw:{'\$cont', M} -> {'\$cont', M}; throw:{'\$brk', M} -> {'\$brk', M} end,\n"
        "        case $sig of {'\$cont', $pat} -> $recurse; {'\$brk', $pat} -> $result end";
  }

  String _tupleOf(List<String> xs) =>
      xs.isEmpty ? 'ok' : (xs.length == 1 ? xs[0] : '{${xs.join(', ')}}');

  String _bindLoop(String call, List<String> mut, Map<String, String> env) {
    if (mut.isEmpty) return call;
    final newNames = mut.map(fresh).toList();
    final lhs = newNames.length == 1 ? newNames[0] : '{${newNames.join(', ')}}';
    for (var i = 0; i < mut.length; i++) {
      env[mut[i]] = newNames[i];
    }
    return '$lhs = $call';
  }

  String _genExpr(Expr e, Map<String, String> env) {
    switch (e) {
      case IntLit(:final value):
        return '$value';
      case FloatLit(:final value):
        return '$value';
      case BoolLit(:final value):
        return value ? 'true' : 'false';
      case VarRef(:final name):
        final v = env[name];
        if (v == null) throw FormatException('undefined variable: $name');
        return v;
      case ListLit(:final elems):
        return '[${elems.map((x) => _genExpr(x, env)).join(', ')}]';
      case StructLit(:final fields):
        final entries =
            fields.map((f) => '${f.$1} => ${_genExpr(f.$2, env)}').join(', ');
        return '#{$entries}';
      case FieldAccess(:final obj, :final field):
        return 'maps:get($field, ${_genExpr(obj, env)})';
      case Index(:final obj, :final index):
        return 'lists:nth((${_genExpr(index, env)}) + 1, ${_genExpr(obj, env)})';
      case Unary(:final op, :final operand):
        final inner = _genExpr(operand, env);
        return op == '!' ? '(not $inner)' : '($op$inner)';
      case Binary(:final op, :final left, :final right):
        return '(${_genExpr(left, env)} ${_erlOp(op)} ${_genExpr(right, env)})';
      case Call(:final callee, :final args):
        if (callee == 'print') {
          return 'io:format("~p~n", [${_genExpr(args[0], env)}])';
        }
        if (callee == 'println') {
          return 'io:format("~ts~n", [${_genExpr(args[0], env)}])';
        }
        if (callee == 'len') {
          return 'length(${_genExpr(args[0], env)})';
        }
        return '${_fnName(callee)}(${args.map((x) => _genExpr(x, env)).join(', ')})';
      case StrLit(:final parts):
        if (parts.isEmpty) return '<<>>';
        final segs = parts.map((p) => switch (p) {
              StrText(:final text) => '"${_escErl(text)}"/utf8',
              StrExpr(:final expr) => "('\$fmt'(${_genExpr(expr, env)}))/binary",
            }).join(', ');
        return '<<$segs>>';
    }
  }

  String _escErl(String s) => s.replaceAll('\\', '\\\\').replaceAll('"', '\\"');

  String _erlOp(String op) => switch (op) {
        '+' => '+',
        '-' => '-',
        '*' => '*',
        '/' => '/',
        '%' => 'rem',
        '&&' => 'andalso',
        '||' => 'orelse',
        '==' => '=:=',
        '!=' => '=/=',
        '<' => '<',
        '>' => '>',
        '<=' => '=<',
        '>=' => '>=',
        _ => throw FormatException('bad op $op'),
      };

  // ---- analysis ----
  bool _needsThrow(Block b) {
    for (var i = 0; i < b.stmts.length; i++) {
      final s = b.stmts[i];
      if (s is ReturnStmt && i != b.stmts.length - 1) return true;
      if (s is IfStmt &&
          (_hasReturn(s.then_) || (s.else_ != null && _hasReturn(s.else_!)))) {
        return true;
      }
      if (s is ForRangeStmt && _hasReturn(s.body)) return true;
      if (s is ForEachStmt && _hasReturn(s.body)) return true;
      if (s is WhileStmt && _hasReturn(s.body)) return true;
    }
    return false;
  }

  bool _hasReturn(Block b) {
    for (final s in b.stmts) {
      if (s is ReturnStmt) return true;
      if (s is IfStmt &&
          (_hasReturn(s.then_) || (s.else_ != null && _hasReturn(s.else_!)))) {
        return true;
      }
      if (s is ForRangeStmt && _hasReturn(s.body)) return true;
      if (s is ForEachStmt && _hasReturn(s.body)) return true;
      if (s is WhileStmt && _hasReturn(s.body)) return true;
    }
    return false;
  }

  /// Break/continue belonging to THIS loop (does not descend into nested loops).
  bool _hasBreakContinue(Block b) {
    for (final s in b.stmts) {
      if (s is BreakStmt || s is ContinueStmt) return true;
      if (s is IfStmt &&
          (_hasBreakContinue(s.then_) ||
              (s.else_ != null && _hasBreakContinue(s.else_!)))) {
        return true;
      }
    }
    return false;
  }

  bool _endsInJump(Block b) {
    if (b.stmts.isEmpty) return false;
    final l = b.stmts.last;
    return l is ReturnStmt || l is BreakStmt || l is ContinueStmt;
  }

  List<String> _mutated(Block b, Map<String, String> env) {
    final a = <String>{};
    _collectAssigned(b, a);
    return a.where(env.containsKey).toList();
  }

  void _collectAssigned(Block b, Set<String> out) {
    for (final s in b.stmts) {
      switch (s) {
        case AssignStmt(:final name):
          out.add(name);
          break;
        case FieldAssignStmt(:final objVar):
          out.add(objVar);
          break;
        case IfStmt(:final then_, :final else_):
          _collectAssigned(then_, out);
          if (else_ != null) _collectAssigned(else_, out);
          break;
        case ForRangeStmt(:final body):
          _collectAssigned(body, out);
          break;
        case ForEachStmt(:final body):
          _collectAssigned(body, out);
          break;
        case WhileStmt(:final body):
          _collectAssigned(body, out);
          break;
        case VarStmt() ||
              ReturnStmt() ||
              ExprStmt() ||
              BreakStmt() ||
              ContinueStmt():
          break;
      }
    }
  }

  List<String> _freeVars(Block b, Map<String, String> env, Set<String> exclude) {
    final refs = <String>{};
    _blockRefs(b, refs);
    return refs
        .where((v) => env.containsKey(v) && !exclude.contains(v))
        .toList();
  }

  void _blockRefs(Block b, Set<String> out) {
    for (final s in b.stmts) {
      switch (s) {
        case VarStmt(:final init):
          _exprRefs(init, out);
          break;
        case AssignStmt(:final op, :final name, :final value):
          if (op != '=') out.add(name);
          _exprRefs(value, out);
          break;
        case FieldAssignStmt(:final objVar, :final value):
          out.add(objVar);
          _exprRefs(value, out);
          break;
        case ReturnStmt(:final value):
          if (value != null) _exprRefs(value, out);
          break;
        case ExprStmt(:final expr):
          _exprRefs(expr, out);
          break;
        case IfStmt(:final cond, :final then_, :final else_):
          _exprRefs(cond, out);
          _blockRefs(then_, out);
          if (else_ != null) _blockRefs(else_, out);
          break;
        case ForRangeStmt(:final start, :final end, :final body):
          _exprRefs(start, out);
          _exprRefs(end, out);
          _blockRefs(body, out);
          break;
        case ForEachStmt(:final iterable, :final body):
          _exprRefs(iterable, out);
          _blockRefs(body, out);
          break;
        case WhileStmt(:final cond, :final body):
          _exprRefs(cond, out);
          _blockRefs(body, out);
          break;
        case BreakStmt() || ContinueStmt():
          break;
      }
    }
  }

  void _exprRefs(Expr e, Set<String> out) {
    switch (e) {
      case IntLit() || FloatLit() || BoolLit():
        break;
      case VarRef(:final name):
        out.add(name);
        break;
      case ListLit(:final elems):
        for (final x in elems) {
          _exprRefs(x, out);
        }
        break;
      case StructLit(:final fields):
        for (final f in fields) {
          _exprRefs(f.$2, out);
        }
        break;
      case FieldAccess(:final obj):
        _exprRefs(obj, out);
        break;
      case Index(:final obj, :final index):
        _exprRefs(obj, out);
        _exprRefs(index, out);
        break;
      case Unary(:final operand):
        _exprRefs(operand, out);
        break;
      case Binary(:final left, :final right):
        _exprRefs(left, out);
        _exprRefs(right, out);
        break;
      case Call(:final args):
        for (final x in args) {
          _exprRefs(x, out);
        }
        break;
      case StrLit(:final parts):
        for (final p in parts) {
          if (p is StrExpr) _exprRefs(p.expr, out);
        }
        break;
    }
  }
}
