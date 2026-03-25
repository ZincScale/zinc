JP = $(HOME)/.cache/coursier/v1/https/repo1.maven.org/maven2/com/github/javaparser/javaparser-core/3.28.0/javaparser-core-3.28.0.jar

.PHONY: build test package native clean

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

# Default: jpackage + jlink (fast build, works everywhere)
package: build
	@rm -rf dist
	jdeps --print-module-deps --ignore-missing-deps --multi-release 25 zinc-compiler.jar > /tmp/zinc-modules.txt 2>/dev/null || echo "java.base" > /tmp/zinc-modules.txt
	jpackage --type app-image \
		--name zinc \
		--input . \
		--main-jar zinc-compiler.jar \
		--main-class zinc.compiler.Main \
		--dest dist \
		--add-modules $$(cat /tmp/zinc-modules.txt) \
		--java-options "--enable-preview" \
		--jlink-options "--strip-debug --no-man-pages --no-header-files --compress=zip-6"
	@echo "built: dist/zinc/ (jpackage)"
	@du -sh dist/zinc/

# Optional: GraalVM native-image (slower build, smaller binary, faster startup)
native: build
	native-image --enable-preview \
		-jar zinc-compiler.jar \
		-o zinc-native \
		--no-fallback \
		-O2 -march=native \
		-H:+ReportExceptionStackTraces
	@echo "built: zinc-native"
	@ls -lh zinc-native

clean:
	rm -rf out dist zinc-compiler.jar zinc-native
