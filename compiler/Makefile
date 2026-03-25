JP = $(HOME)/.cache/coursier/v1/https/repo1.maven.org/maven2/com/github/javaparser/javaparser-core/3.28.0/javaparser-core-3.28.0.jar

.PHONY: build test clean

build:
	@mkdir -p out
	javac --enable-preview --source 25 -cp $(JP) -d out src/zinc/compiler/*.java
	cd out && jar xf $(JP)
	cd out && jar cfe ../zinc-compiler.jar zinc.compiler.Main .
	@echo "built: zinc-compiler.jar"

test: build
	javac --enable-preview --source 25 -cp $(JP) -d out src/zinc/compiler/*.java test/zinc/compiler/*.java
	java --enable-preview -cp out zinc.compiler.LexerTest
	java --enable-preview -cp out zinc.compiler.ParserTest
	java --enable-preview -cp out:$(JP) zinc.compiler.TransformerTest
	bash run_e2e.sh

clean:
	rm -rf out zinc-compiler.jar
