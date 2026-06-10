import 'dart:io';
import '../lib/lexer.dart';
import '../lib/parser.dart';
import '../lib/codegen.dart';

void main(List<String> args) {
  if (args.length != 2) {
    stderr.writeln('usage: main.dart <file.src> <outdir>');
    exit(2);
  }
  try {
    final src = File(args[0]).readAsStringSync();
    final toks = lex(src);
    final fns = Parser(toks).parseProgram();
    final modules = CodeGen(fns).generateModules();
    final outDir = Directory(args[1])..createSync(recursive: true);
    for (final e in modules.entries) {
      final path = '${outDir.path}/${e.key}.erl';
      File(path).writeAsStringSync(e.value);
      stdout.writeln(path);
    }
  } on FormatException catch (e) {
    stderr.writeln(e.message);
    exit(1);
  }
}
