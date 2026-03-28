JP = $(HOME)/.cache/coursier/v1/https/repo1.maven.org/maven2/com/github/javaparser/javaparser-core/3.28.0/javaparser-core-3.28.0.jar
MILL_VERSION = 1.1.3
MILL_LAUNCHER_URL = https://raw.githubusercontent.com/com-lihaoyi/mill/main/mill
UV_VERSION = 0.7.12
UV_URL = https://github.com/astral-sh/uv/releases/download/$(UV_VERSION)/uv-$(shell uname -m)-unknown-linux-gnu.tar.gz

.PHONY: build test package native clean deps

# Download dependencies (JavaParser for compile-time, Mill for bundling)
deps:
	@mkdir -p lib
	@if [ ! -f "$(JP)" ]; then \
		echo "downloading JavaParser..."; \
		mkdir -p $$(dirname $(JP)); \
		curl -sL https://repo1.maven.org/maven2/com/github/javaparser/javaparser-core/3.28.0/javaparser-core-3.28.0.jar -o $(JP); \
	fi
	@if [ ! -f lib/mill ]; then \
		echo "downloading Mill launcher..."; \
		curl -sL $(MILL_LAUNCHER_URL) -o lib/mill && chmod +x lib/mill; \
	fi
	@if [ ! -f lib/uv ]; then \
		echo "downloading uv $(UV_VERSION)..."; \
		curl -sL $(UV_URL) | tar xz -C lib/ --strip-components=1 uv-$(shell uname -m)-unknown-linux-gnu/uv && chmod +x lib/uv; \
	fi

# Build fat jar (zinc-compiler.jar = zinc compiler + JavaParser + stdlib)
build: deps
	@mkdir -p out
	javac --enable-preview --source 25 -cp $(JP) -d out src/zinc/compiler/*.java
	cp -r src/zinc/stdlib out/zinc/
	cd out && jar xf $(JP)
	cd out && jar cfe ../zinc-compiler.jar zinc.compiler.Main .
	@echo "built: zinc-compiler.jar"

# Run tests — unit tests need javac (build-time only), e2e uses zinc binary
test: package
	javac --enable-preview --source 25 -cp $(JP) -d out src/zinc/compiler/*.java test/zinc/compiler/*.java
	java --enable-preview -cp out zinc.compiler.LexerTest
	java --enable-preview -cp out zinc.compiler.ParserTest
	java --enable-preview -cp out:$(JP) zinc.compiler.TransformerTest
	java --enable-preview -cp out zinc.compiler.PythonEmitterTest
	bash run_e2e.sh

# Default: jpackage + jlink with JDK modules + bundled Mill
package: build deps
	@rm -rf dist pkg-input
	@mkdir -p pkg-input
	cp zinc-compiler.jar pkg-input/
	cp lib/mill pkg-input/
	cp lib/uv pkg-input/
	jdeps --print-module-deps --ignore-missing-deps --multi-release 25 zinc-compiler.jar > /tmp/zinc-modules.txt 2>/dev/null || echo "java.base" > /tmp/zinc-modules.txt
	@echo "modules: $$(cat /tmp/zinc-modules.txt)"
	@which jpackage >/dev/null 2>&1 || (echo "error: jpackage not found. Install JDK 25 (not just JRE)." && exit 1)
	jpackage --type app-image \
		--name zinc \
		--input pkg-input \
		--main-jar zinc-compiler.jar \
		--main-class zinc.compiler.Main \
		--dest dist \
		--add-modules $$(cat /tmp/zinc-modules.txt),jdk.compiler,jdk.jlink,jdk.jdeps,jdk.jpackage \
		--java-options "--enable-preview" \
		--jlink-options "--strip-debug --no-man-pages --no-header-files --compress=zip-6" \
		--verbose
	@rm -rf pkg-input
	@echo "jpackage output:" && ls -la dist/
	@du -sh dist/zinc/ 2>/dev/null || du -sh dist/*
	@echo "built: dist/zinc/ (self-contained: JDK + Mill + zinc compiler)"

# Optional: GraalVM native-image (smaller + faster, but no javac/jpackage internally)
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
	rm -rf out dist pkg-input zinc-compiler.jar zinc-native lib/mill
