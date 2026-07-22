package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareProjectConfigAddsStdlibDependency(t *testing.T) {
	srcDir := t.TempDir()
	writeTestFile(t, filepath.Join(srcDir, "main.zn"), `import stdlib/config

void main() {}
`)
	cfg := &zincConfig{
		Imports:  make(map[string]string),
		Replaces: make(map[string]string),
	}

	if err := prepareProjectConfig(cfg, srcDir); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Imports[stdlibAlias]; got != stdlibModulePath {
		t.Fatalf("stdlib import = %q, want %q", got, stdlibModulePath)
	}
	wantDep := stdlibModulePath + " " + stdlibVersion
	if len(cfg.Deps) != 1 || cfg.Deps[0] != wantDep {
		t.Fatalf("deps = %#v, want [%q]", cfg.Deps, wantDep)
	}
}

func TestPrepareProjectConfigUsesPinnedStdlibCoordinates(t *testing.T) {
	srcDir := t.TempDir()
	writeTestFile(t, filepath.Join(srcDir, "main.zn"), "import stdlib/config\n\nvoid main() {}\n")
	cfg := &zincConfig{
		StdlibModule:  "modules.example.test/zinc-stdlib",
		StdlibVersion: "v2.3.4",
		Imports:       make(map[string]string),
		Replaces:      make(map[string]string),
	}

	if err := prepareProjectConfig(cfg, srcDir); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Imports[stdlibAlias]; got != cfg.StdlibModule {
		t.Fatalf("stdlib import = %q, want %q", got, cfg.StdlibModule)
	}
	wantDep := cfg.StdlibModule + " " + cfg.StdlibVersion
	if len(cfg.Deps) != 1 || cfg.Deps[0] != wantDep {
		t.Fatalf("deps = %#v, want [%q]", cfg.Deps, wantDep)
	}
}

func TestLoadZincTomlAllowsPinnedStdlibReplacement(t *testing.T) {
	projectDir := t.TempDir()
	manifestPath := filepath.Join(projectDir, "zinc.toml")
	writeTestFile(t, manifestPath, `[project]
name = "consumer"

[stdlib]
module = "modules.example.test/zinc-stdlib"
version = "v2.3.4"

[replace]
stdlib = "../stdlib-out"
`)

	cfg, err := loadZincToml(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StdlibModule != "modules.example.test/zinc-stdlib" || cfg.StdlibVersion != "v2.3.4" {
		t.Fatalf("stdlib coordinates = %s@%s", cfg.StdlibModule, cfg.StdlibVersion)
	}
	if cfg.Imports[stdlibAlias] != cfg.StdlibModule {
		t.Fatalf("stdlib alias = %q", cfg.Imports[stdlibAlias])
	}
	wantReplace := filepath.Clean(filepath.Join(projectDir, "../stdlib-out"))
	if cfg.Replaces[cfg.StdlibModule] != wantReplace {
		t.Fatalf("stdlib replace = %q, want %q", cfg.Replaces[cfg.StdlibModule], wantReplace)
	}
}

func TestWriteZincLibraryMetadata(t *testing.T) {
	srcDir := t.TempDir()
	outDir := t.TempDir()
	writeTestFile(t, filepath.Join(srcDir, "config", "config.zn"), "class Config { init() {} }\n")
	writeTestFile(t, filepath.Join(outDir, "config", "config.go"), "package config\n\n//line "+filepath.ToSlash(srcDir)+"/config/config.zn:1\ntype Config struct{}\n")

	const modulePath = "example.com/library"
	if err := writeZincLibraryMetadata(srcDir, outDir, modulePath); err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(filepath.Join(outDir, librarySourceDir, "config", "config.zn"))
	if err != nil {
		t.Fatal(err)
	}
	if string(copied) != "class Config { init() {} }\n" {
		t.Fatalf("copied source = %q", copied)
	}
	data, err := os.ReadFile(filepath.Join(outDir, libraryManifest))
	if err != nil {
		t.Fatal(err)
	}
	var manifest zincLibraryManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != librarySchema || manifest.Module != modulePath || manifest.Source != librarySourceDir {
		t.Fatalf("manifest = %#v", manifest)
	}
	generated, err := os.ReadFile(filepath.Join(outDir, "config", "config.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(generated), filepath.ToSlash(srcDir)) || !strings.Contains(string(generated), "//line zinc-src/config/config.zn:1") {
		t.Fatalf("generated line directive was not made portable:\n%s", generated)
	}
}

func TestLoadDepClassDeclsFromGoModuleCache(t *testing.T) {
	const (
		modulePath = "example.com/zinclib"
		version    = "v0.1.0"
	)
	proxyDir := t.TempDir()
	moduleFiles := map[string]string{
		"go.mod":                    "module " + modulePath + "\n\ngo 1.26.4\n",
		libraryManifest:             `{"schema":1,"module":"` + modulePath + `","source":"zinc-src"}` + "\n",
		"zinc-src/config/config.zn": "class Config { init() {} pub int port() { return 8080 } }\n",
		"config/config.go":          "package config\ntype Config struct{}\nfunc (Config) Port() int { return 8080 }\n",
	}
	writeModuleProxyVersion(t, proxyDir, modulePath, version, moduleFiles)

	consumerDir := t.TempDir()
	writeTestFile(t, filepath.Join(consumerDir, "go.mod"), "module consumer\n\ngo 1.26.4\n\nrequire "+modulePath+" "+version+"\n")
	moduleCache := filepath.Join(t.TempDir(), "modcache")
	t.Cleanup(func() { makeTreeWritable(moduleCache) })
	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxyDir))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOMODCACHE", moduleCache)
	t.Setenv("GOCACHE", filepath.Join(t.TempDir(), "gocache"))

	download := goCmd("mod", "download", "all")
	download.Dir = consumerDir
	if out, err := download.CombinedOutput(); err != nil {
		t.Fatalf("go mod download: %v\n%s", err, out)
	}

	cfg := &zincConfig{
		Imports: map[string]string{"zinclib": modulePath},
		Deps:    []string{modulePath + " " + version},
	}
	decls, err := loadDepClassDecls(cfg, consumerDir)
	if err != nil {
		t.Fatal(err)
	}
	if decls["config"]["Config"] == nil {
		t.Fatalf("cached Zinc class metadata was not loaded: %#v", decls)
	}
	dirs, err := resolvedModuleDirs(consumerDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(dirs[modulePath], moduleCache) {
		t.Fatalf("resolved module dir %q is outside GOMODCACHE %q", dirs[modulePath], moduleCache)
	}
}

func writeModuleProxyVersion(t *testing.T, proxyDir, modulePath, version string, files map[string]string) {
	t.Helper()
	versionDir := filepath.Join(proxyDir, filepath.FromSlash(modulePath), "@v")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(versionDir, "list"), version+"\n")
	writeTestFile(t, filepath.Join(versionDir, version+".info"), `{"Version":"`+version+`","Time":"2026-01-01T00:00:00Z"}`+"\n")
	writeTestFile(t, filepath.Join(versionDir, version+".mod"), files["go.mod"])

	zipPath := filepath.Join(versionDir, version+".zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	prefix := modulePath + "@" + version + "/"
	for name, content := range files {
		w, err := zw.Create(prefix + filepath.ToSlash(name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeTreeWritable(root string) {
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil {
			_ = os.Chmod(path, info.Mode()|0o700)
		}
		return nil
	})
}
