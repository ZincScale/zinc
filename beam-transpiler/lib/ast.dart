class FnDecl {
  final String name;
  final List<String> params;
  final Block body;
  FnDecl(this.name, this.params, this.body);
}

class Block {
  final List<Stmt> stmts;
  Block(this.stmts);
}

sealed class Stmt {}

final class VarStmt extends Stmt {
  final String name;
  final Expr init;
  VarStmt(this.name, this.init);
}

final class AssignStmt extends Stmt {
  final String name;
  final String op;
  final Expr value;
  AssignStmt(this.name, this.op, this.value);
}

final class FieldAssignStmt extends Stmt {
  final String objVar;
  final String field;
  final String op;
  final Expr value;
  FieldAssignStmt(this.objVar, this.field, this.op, this.value);
}

final class IfStmt extends Stmt {
  final Expr cond;
  final Block then_;
  final Block? else_;
  IfStmt(this.cond, this.then_, this.else_);
}

final class ForRangeStmt extends Stmt {
  final String varName;
  final Expr start;
  final Expr end;
  final Block body;
  ForRangeStmt(this.varName, this.start, this.end, this.body);
}

final class ForEachStmt extends Stmt {
  final String varName;
  final Expr iterable;
  final Block body;
  ForEachStmt(this.varName, this.iterable, this.body);
}

final class WhileStmt extends Stmt {
  final Expr cond;
  final Block body;
  WhileStmt(this.cond, this.body);
}

final class ReturnStmt extends Stmt {
  final Expr? value;
  ReturnStmt(this.value);
}

final class ExprStmt extends Stmt {
  final Expr expr;
  ExprStmt(this.expr);
}

final class BreakStmt extends Stmt {}

final class ContinueStmt extends Stmt {}

sealed class Expr {}

final class IntLit extends Expr {
  final int value;
  IntLit(this.value);
}

final class FloatLit extends Expr {
  final double value;
  FloatLit(this.value);
}

final class BoolLit extends Expr {
  final bool value;
  BoolLit(this.value);
}

final class VarRef extends Expr {
  final String name;
  VarRef(this.name);
}

final class ListLit extends Expr {
  final List<Expr> elems;
  ListLit(this.elems);
}

final class StructLit extends Expr {
  final String name;
  final List<(String, Expr)> fields;
  StructLit(this.name, this.fields);
}

final class FieldAccess extends Expr {
  final Expr obj;
  final String field;
  FieldAccess(this.obj, this.field);
}

final class Index extends Expr {
  final Expr obj;
  final Expr index;
  Index(this.obj, this.index);
}

final class Binary extends Expr {
  final String op;
  final Expr left;
  final Expr right;
  Binary(this.op, this.left, this.right);
}

final class Unary extends Expr {
  final String op;
  final Expr operand;
  Unary(this.op, this.operand);
}

final class Call extends Expr {
  final String callee;
  final List<Expr> args;
  Call(this.callee, this.args);
}

final class StrLit extends Expr {
  final List<StrPart> parts;
  StrLit(this.parts);
}

sealed class StrPart {}

final class StrText extends StrPart {
  final String text;
  StrText(this.text);
}

final class StrExpr extends StrPart {
  final Expr expr;
  StrExpr(this.expr);
}
