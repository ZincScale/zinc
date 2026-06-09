import 'dart:io';
import '../lib/lexer.dart';
import '../lib/parser.dart';
import '../lib/codegen.dart';

void main(List<String> args) {
  if (args.isEmpty) {
    stderr.writeln('usage: main.dart <file.src>');
    exit(2);
  }
  try {
    final src = File(args[0]).readAsStringSync();
    final toks = lex(src);
    final fns = Parser(toks).parseProgram();
    stdout.write(CodeGen(fns).generate());
  } on FormatException catch (e) {
    stderr.writeln(e.message);
    exit(1);
  }
}
