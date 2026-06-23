import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.file.DirectoryStream;
import java.nio.file.Files;
import java.nio.file.LinkOption;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** zc — the zinc meta-builder for BEAM (patterned on compilers/zinc-go cmd/zinc). */
public class Zc {
  static Path home; // beam-transpiler root (transpiler + rebar_zinc plugin live here)

  public static void main(String[] args) throws Exception {
    String h = System.getProperty("zinc.home", System.getenv("ZINC_HOME"));
    if (h == null) h = System.getProperty("ZINC_HOME_LIB"); // jar shim passes the lib dir
    if (h == null && selfJar() != null) h = selfJar().getParent().toString();
    if (h == null) die("zc: ZINC_HOME not set (run via bin/zc)");
    home = Path.of(h).toAbsolutePath();
    if (args.length == 0) {
      usage();
      System.exit(1);
    }
    switch (args[0]) {
      case "init", "new" -> {
        boolean javaSurface = false;
        boolean legacyPy = false;
        String name = null;
        for (int i = 1; i < args.length; i++) {
          if (args[i].equals("--java")) javaSurface = true;
          else if (args[i].equals("--py")) legacyPy = true; // accepted for old scripts
          else if (!args[i].startsWith("-")) name = args[i];
        }
        if (name == null) die("usage: zc init [--java|--py] <name>");
        init(name, javaSurface, legacyPy);
      }
      case "build" -> build(projectDir(args));
      case "release" -> release(projectDir(args));
      case "run" -> {
        // --once: run the entry and exit even for an Application (main + init:stop), instead
        // of staying alive as a service. The CI/test/smoke shape -- no hang on a server.
        boolean once = args.length >= 2 && args[1].equals("--once");
        int pi = once ? 2 : 1;
        String path = args.length > pi ? args[pi] : ".";
        boolean scriptFile = path.endsWith(".zinc") || path.endsWith(".zn");
        boolean scriptDir = Files.isDirectory(Path.of(path))
            && !Files.exists(Path.of(path).resolve("zinc.toml")); // a folder of .zn, no project
        if (scriptFile || scriptDir) {
          runSingle(Path.of(path), once); // script mode: no project, no zinc.toml
        } else {
          Path dir = Files.exists(Path.of(path).resolve("zinc.toml")) ? Path.of(path)
              : projectDir(args);
          build(dir);
          if (once) runOnce(dir); else run(dir);
        }
      }
      case "test" -> {
        Path dir = projectDir(args);
        var cfg = loadToml(dir);
        useManagedOtp(cfg);
        ensureGenerated(dir, cfg);
        vendorDeps(dir, cfg);
        // two passes: rebar snapshots the test dir BEFORE the pre-compile hook
        // transpiles .zinc -> zinc_gen/*.erl, so a fresh project's first eunit
        // run would see zero tests. The compile pass materializes the .erl files;
        // eunit then picks them up. (Incremental, so the cost is one no-op pass.)
        exec(dir, rebarCmd("as", "test", "compile"));
        exec(dir, rebarCmd("eunit"));
      }
      case "clean" -> {
        Path dir = projectDir(args);
        var cfg = loadToml(dir);
        useManagedOtp(cfg);
        ensureGenerated(dir, cfg);
        exec(dir, rebarCmd("clean"));
      }
      // OPT-IN static net over the FFI basement (never on the build/run hot path):
      // xref catches calls to functions that exist nowhere; dialyzer adds success typing
      // (bad arity, type-incompatible FFI). Covers hex deps too, since both see the whole
      // loaded code path. --xref skips the (slow, PLT-building) dialyzer pass.
      case "check" -> {
        Path dir = projectDir(args);
        var cfg = loadToml(dir);
        useManagedOtp(cfg);
        ensureGenerated(dir, cfg);
        vendorDeps(dir, cfg);
        exec(dir, rebarCmd("compile"));
        System.out.println("zc: xref — calls to functions that exist nowhere ...");
        exec(dir, rebarCmd("xref"));
        if (!java.util.Arrays.asList(args).contains("--xref")) {
          System.out.println("zc: dialyzer — success typing (first run builds a PLT, "
              + "then cached) ...");
          exec(dir, rebarCmd("dialyzer"));
        }
      }
      case "deps" -> {
        var cfg = loadToml(projectDir(args));
        cfg.getOrDefault("deps", Map.of()).forEach((k, v) -> System.out.println(k + " " + v));
      }
      case "add" -> {
        if (args.length < 2) die("usage: zc add <name@version>  (hex package)");
        add(projectDir(new String[] {""}), args[1]);
      }
      case "toolchain" -> {
        if (args.length < 2) die("usage: zc toolchain <install [version] | list>");
        switch (args[1]) {
          case "install" -> toolchainInstall(args.length >= 3 ? args[2] : pinFromCwd());
          case "list" -> toolchainList();
          default -> die("usage: zc toolchain <install [version] | list>");
        }
      }
      case "doctor" -> doctor();
      case "fmt" -> {
        if (args.length < 2) die("usage: zc fmt <file.zinc|.zn | dir>");
        fmt(Path.of(args[1]));
      }
      case "version", "--version", "-v" -> System.out.println("zc 0.1.0 (zinc on BEAM)");
      default -> {
        usage();
        System.exit(1);
      }
    }
  }

  static Path projectDir(String[] args) {
    Path dir = Path.of(args.length >= 2 && !args[1].startsWith("-") ? args[1] : ".");
    if (!Files.exists(dir.resolve("zinc.toml"))) {
      die("zc: no zinc.toml in " + dir.toAbsolutePath() + " (use 'zc init <name>')");
    }
    return dir;
  }

  // ---- init ----

  static void init(String name) throws IOException {
    init(name, false, false);
  }

  /** Default scaffolds canonical Zinc (.zn); `--py` keeps the legacy Python-shaped sample,
   *  and `--java` keeps the original legal-Java frontend sample. */
  static void init(String name, boolean javaSurface, boolean legacyPy) throws IOException {
    Path dir = Path.of(name);
    if (Files.exists(dir)) die("zc: directory '" + name + "' already exists");
    Files.createDirectories(dir.resolve("src"));
    String base = dir.getFileName().toString();
    Files.writeString(dir.resolve("zinc.toml"), """
        [project]
        name = "%s"
        version = "0.1.0"

        [otp]
        version = "29"

        [deps]
        # hex packages, e.g.:  cowboy = "2.12.0"
        """.formatted(base));
    String entry = javaSurface ? "src/Main.zinc" : "src/main.zn";
    Files.writeString(dir.resolve(entry), javaSurface ? """
        public class Main {
          public static void main(String[] args) {
            System.out.println("Hello from %s!");
          }
        }
        """.formatted(base) : legacyPy ? """
        def main() {
            print("Hello from %s!")
        }
        """.formatted(base) : """
        void main() {
            print("Hello from %s!")
        }
        """.formatted(base));
    Files.writeString(dir.resolve(".gitignore"),
        "_build/\n_checkouts/\nsrc/zinc_gen/\ntest/zinc_gen/\nrebar.config\nsrc/*.app.src\n");
    ensureGenerated(dir, loadToml(dir));
    System.out.println("created " + name + "/");
    System.out.println("  zinc.toml  " + entry);
    System.out.println("next: cd " + name + " && zc run");
  }

  // ---- generated build files (rebar.config, .app.src) -- regenerated every build ----

  static void ensureGenerated(Path dir, Map<String, Map<String, String>> cfg) throws IOException {
    ensureGenerated(dir, cfg, false);
  }

  /** forRelease adds a relx release + {mod,{zinc_app,[]}} so `rebar3 tar` builds a
   *  self-contained OTP release (ERTS + beam + boot script) that boots the tree. */
  static void ensureGenerated(Path dir, Map<String, Map<String, String>> cfg,
      boolean forRelease) throws IOException {
    var project = cfg.getOrDefault("project", Map.of());
    String name = project.getOrDefault("name", dir.toAbsolutePath().getFileName().toString());
    if (!name.matches("[a-z][a-z0-9_]*")) {
      die("zc: project name '" + name + "' must be a valid OTP application name: "
          + "lowercase letter first, then [a-z0-9_] (set [project] name in zinc.toml)");
    }
    String vsn = project.getOrDefault("version", "0.1.0");
    var deps = cfg.getOrDefault("deps", Map.of());

    var depList = new ArrayList<String>();
    var depApps = new ArrayList<String>();
    deps.forEach((k, v) -> {
      depList.add("{" + k + ", \"" + v + "\"}");
      depApps.add(", " + k);
    });
    String relx = forRelease ? """
        {relx, [{release, {%s, "%s"}, [%s, sasl]},
                {dev_mode, false}, {include_erts, true}, {extended_start_script, true}]}.
        """.formatted(name, vsn, name) : "";
    Files.writeString(dir.resolve("rebar.config"), """
        %% generated by zc from zinc.toml -- do not edit
        {plugins, [rebar_zinc]}.
        {provider_hooks, [{pre, [{compile, {zinc, compile}}]},
                          {post, [{clean, {zinc, clean}}]}]}.
        {src_dirs, [{"src", [{recursive, true}]}]}.
        {profiles, [{test, [{extra_src_dirs, [{"test", [{recursive, true}]}]}]}]}.
        {xref_checks, [undefined_function_calls, deprecated_function_calls]}.
        {deps, [%s]}.
        %s""".formatted(String.join(", ", depList), relx));
    String mod = forRelease ? "    {mod, {zinc_app, []}},\n" : "";
    Files.writeString(dir.resolve("src/" + name + ".app.src"), """
        %% generated by zc from zinc.toml -- do not edit
        {application, %s, [
            {description, "%s"},
            {vsn, "%s"},
            {registered, []},
        %s    {applications, [kernel, stdlib%s]},
            {env, []},
            {modules, []}
        ]}.
        """.formatted(name, name, vsn, mod, String.join("", depApps)));
    // the rebar_zinc plugin resolves from _checkouts until it's published
    Path checkouts = dir.resolve("_checkouts");
    Files.createDirectories(checkouts);
    Path link = checkouts.resolve("rebar_zinc");
    if (!Files.exists(link, LinkOption.NOFOLLOW_LINKS)) {
      Files.createSymbolicLink(link, home.resolve("rebar_zinc"));
    }
  }

  // ---- hex deps: zc fetches and vendors into _checkouts; rebar3 then builds offline.
  // (rebar3's own fetch uses Erlang TLS, which fingerprint-filtering egress fireworks drop;
  // zc's Java TLS passes. Vendoring also serves hermetic/reproducible builds.)

  static void vendorDeps(Path dir, Map<String, Map<String, String>> cfg) throws Exception {
    var queue = new LinkedHashMap<String, String>(cfg.getOrDefault("deps", Map.of()));
    var seen = new HashSet<String>();
    while (!queue.isEmpty()) {
      var it = queue.entrySet().iterator();
      var e = it.next();
      it.remove();
      String name = e.getKey(), vsn = e.getValue();
      if (!seen.add(name)) continue; // MVS-lite: first requirement wins (root deps first)
      Path dst = dir.resolve("_checkouts").resolve(name);
      if (Files.isDirectory(dst, LinkOption.NOFOLLOW_LINKS)) continue; // already vendored
      Path tar = fetchTarball(name, vsn);
      Path tmp = Files.createTempDirectory("zc-hex-" + name);
      exec(tmp, "tar", "xf", tar.toString());
      Files.createDirectories(dst);
      exec(dst, "tar", "xzf", tmp.resolve("contents.tar.gz").toString());
      System.err.println("zc: vendored " + name + " " + vsn + " (hex)");
      requirements(tmp.resolve("metadata.config")).forEach(queue::putIfAbsent);
    }
  }

  static Path fetchTarball(String name, String vsn) throws Exception {
    Path cache = Path.of(System.getProperty("user.home"), ".cache", "zinc", "hex");
    Files.createDirectories(cache);
    Path tar = cache.resolve(name + "-" + vsn + ".tar");
    if (Files.exists(tar)) return tar;
    String url = "https://repo.hex.pm/tarballs/" + name + "-" + vsn + ".tar";
    var client = HttpClient.newBuilder().followRedirects(HttpClient.Redirect.NORMAL).build();
    var resp = client.send(HttpRequest.newBuilder(URI.create(url)).build(),
                           HttpResponse.BodyHandlers.ofByteArray());
    if (resp.statusCode() != 200) die("zc: fetch " + url + " -> HTTP " + resp.statusCode());
    Files.write(tar, resp.body());
    return tar;
  }

  // requirement entries in metadata.config are proplists with keys in alphabetical order:
  // {<<"app">>,<<"cowlib">>}, {<<"optional">>,false}, {<<"requirement">>,<<"2.13.0">>}
  static final Pattern REQ = Pattern.compile(
      "\\{<<\"app\">>,<<\"([a-z0-9_]+)\">>\\},\\s*\\{<<\"optional\">>,(true|false)\\},\\s*"
          + "\\{<<\"requirement\">>,<<\"([^\"]+)\">>\\}");

  static Map<String, String> requirements(Path metadataConfig) throws IOException {
    var reqs = new LinkedHashMap<String, String>();
    Matcher m = REQ.matcher(Files.readString(metadataConfig));
    while (m.find()) {
      if (m.group(2).equals("false")) reqs.put(m.group(1), minVersion(m.group(3)));
    }
    return reqs;
  }

  // MVS: a hex requirement ("2.13.0", "~> 2.13", ">= 1.8.0") admits a minimum version;
  // build against exactly that. Reproducible without a lock file.
  static String minVersion(String req) {
    Matcher m = Pattern.compile("\\d+(\\.\\d+){0,2}").matcher(req);
    if (!m.find()) die("zc: unsupported hex requirement: " + req);
    String v = m.group();
    return v + ".0.0".substring(0, 2 * (3 - v.split("\\.").length));
  }

  // ---- managed toolchain: ~/.zc/otp/<ver> (rustup model — users never apt-install
  // erlang). Prebuilt portable OTP from hexpm/bob (builds.hex.pm, the Livebook/burrito
  // source; decision #7: reuse, don't build — revisit if compliance demands it).
  // Downloads go through Java TLS: corp egress drops Erlang's TLS fingerprint, Java passes.

  static final String[] OTP_FLAVORS = {"ubuntu-22.04", "ubuntu-24.04"};

  static Path zcHome() {
    String h = System.getenv("ZC_HOME");
    return h != null ? Path.of(h) : Path.of(System.getProperty("user.home"), ".zc");
  }

  static String pinFromCwd() throws IOException {
    Path here = Path.of(".");
    if (!Files.exists(here.resolve("zinc.toml"))) {
      die("zc toolchain install: give a version, or run inside a project ([otp] pin)");
    }
    String pin = loadToml(here).getOrDefault("otp", Map.of()).get("version");
    if (pin == null) die("zc toolchain install: zinc.toml has no [otp] version");
    return pin;
  }

  static void toolchainInstall(String pin) throws Exception {
    var client = HttpClient.newBuilder().followRedirects(HttpClient.Redirect.NORMAL).build();
    if (resolveOtp(pin) != null) {
      System.out.println("zc: OTP " + resolveOtp(pin).getFileName() + " already installed");
      ensureRebar3(client);
      ensureJre(client);
      return;
    }
    for (String flavor : OTP_FLAVORS) {
      String base = "https://builds.hex.pm/builds/otp/" + flavor + "/";
      var builds = client.send(HttpRequest.newBuilder(URI.create(base + "builds.txt")).build(),
          HttpResponse.BodyHandlers.ofString());
      if (builds.statusCode() != 200) continue;
      String best = null;
      String sha = null;
      for (String line : builds.body().split("\n")) {
        String[] f = line.strip().split("\\s+");
        if (f.length < 4 || !f[0].startsWith("OTP-") || f[0].contains("-rc")) continue;
        String v = f[0].substring(4);
        if (!(v.equals(pin) || v.startsWith(pin + "."))) continue;
        if (best == null || cmpVersion(v, best) > 0) {
          best = v;
          sha = f[3];
        }
      }
      if (best == null) continue;
      System.out.println("zc: downloading OTP " + best + " (" + flavor + ") ...");
      var tgz = client.send(
          HttpRequest.newBuilder(URI.create(base + "OTP-" + best + ".tar.gz")).build(),
          HttpResponse.BodyHandlers.ofByteArray());
      if (tgz.statusCode() != 200) continue;
      String got = sha256(tgz.body());
      if (!got.equalsIgnoreCase(sha)) {
        die("zc: OTP " + best + " checksum mismatch (want " + sha + ", got " + got + ")");
      }
      Path tmp = Files.createTempDirectory("zc-otp");
      Path tarball = tmp.resolve("otp.tar.gz");
      Files.write(tarball, tgz.body());
      exec(tmp, "tar", "xzf", tarball.toString());
      Path root = tmp.resolve("OTP-" + best);
      if (!Files.isDirectory(root)) root = tmp; // tolerate flat tarballs
      execQuiet(root, "sh", "Install", "-minimal", root.toAbsolutePath().toString());
      // sanity: erl boots and crypto's NIFs load against THIS box's libs — the
      // check is what picks the flavor, not platform guesswork
      if (!sanityOtp(root)) {
        System.out.println("zc: OTP " + best + " (" + flavor + ") does not run here, "
            + "trying next flavor");
        continue;
      }
      Path dst = zcHome().resolve("otp").resolve(best);
      Files.createDirectories(dst.getParent());
      Files.move(root, dst);
      // Install burned the tmp path into the launch scripts: re-run against the new home
      execQuiet(dst, "sh", "Install", "-minimal", dst.toAbsolutePath().toString());
      if (!sanityOtp(dst)) die("zc: OTP " + best + " failed sanity after move");
      Files.writeString(dst.resolve(".zc-flavor"), flavor + "\n");
      System.out.println("zc: installed OTP " + best + " -> " + dst);
      ensureRebar3(client);
      ensureJre(client);
      return;
    }
    // RHEL/Fedora fallback: the builds.hex.pm tarballs are Debian-built and their crypto
    // NIF can need OpenSSL symbols (e.g. SM4) that RHEL strips, so it fails to load. RabbitMQ
    // ships el<N> RPMs built against the host's OpenSSL — crypto loads. Gated by sanityOtp.
    String elMajor = elMajor();
    String elVer = elMajor == null ? null : resolveElVersion(client, pin); // RabbitMQ's own tags
    if (elVer != null && commandExists("rpm2cpio") && commandExists("cpio")) {
      String arch = System.getProperty("os.arch").contains("aarch64") ? "aarch64" : "x86_64";
      String url = "https://github.com/rabbitmq/erlang-rpm/releases/download/v" + elVer
          + "/erlang-" + elVer + "-1.el" + elMajor + "." + arch + ".rpm";
      System.out.println("zc: downloading OTP " + elVer + " (rabbitmq el" + elMajor + ") ...");
      var rpm = client.send(HttpRequest.newBuilder(URI.create(url)).build(),
          HttpResponse.BodyHandlers.ofByteArray());
      if (rpm.statusCode() == 200) {
        Path otpDir = zcHome().resolve("otp");
        Files.createDirectories(otpDir);
        Path tmp = Files.createTempDirectory(otpDir, "zc-otp-el"); // same fs -> move is atomic
        Path rpmf = tmp.resolve("otp.rpm");
        Files.write(rpmf, rpm.body());
        exec(tmp, "sh", "-c", "rpm2cpio '" + rpmf + "' | cpio -idm --quiet");
        Path root = tmp.resolve("usr/lib64/erlang"); // RPM payload root (relocatable erl)
        if (Files.isDirectory(root) && sanityOtp(root)) {
          Path dst = otpDir.resolve(elVer);
          Files.move(root, dst);
          if (!sanityOtp(dst)) die("zc: OTP " + elVer + " failed sanity after move");
          Files.writeString(dst.resolve(".zc-flavor"), "rabbitmq-el" + elMajor + "\n");
          System.out.println("zc: installed OTP " + elVer + " -> " + dst);
          ensureRebar3(client);
          ensureJre(client);
          return;
        }
      }
    }
    die("zc: no runnable OTP build found for pin '" + pin + "' (tried hexpm "
        + String.join(", ", OTP_FLAVORS) + (elMajor != null ? " + rabbitmq el" + elMajor : "")
        + ")");
  }

  /** RHEL/Fedora major version if this is an rpm-family host, else null (drives the el RPM). */
  static String elMajor() {
    try {
      String os = Files.readString(Path.of("/etc/os-release"));
      boolean rpmFamily = os.matches("(?s).*\\bID=\"?(rhel|centos|rocky|almalinux|fedora|ol)\"?.*")
          || (os.contains("ID_LIKE=") && (os.contains("rhel") || os.contains("fedora")));
      if (!rpmFamily) return null;
      var m = java.util.regex.Pattern.compile("VERSION_ID=\"?(\\d+)").matcher(os);
      return m.find() ? m.group(1) : null;
    } catch (Exception e) {
      return null;
    }
  }

  /** Highest RabbitMQ erlang-rpm tag matching the pin, read from its own git refs (smart-HTTP
   *  info/refs — no GitHub API rate limit, no git binary). Pin "29" -> "29.0.2". Decoupled
   *  from builds.hex.pm so a version skew between the two can't 404 the el RPM. null if none. */
  static String resolveElVersion(HttpClient client, String pin) {
    try {
      var refs = client.send(HttpRequest.newBuilder(URI.create(
          "https://github.com/rabbitmq/erlang-rpm.git/info/refs?service=git-upload-pack"))
          .build(), HttpResponse.BodyHandlers.ofString());
      if (refs.statusCode() != 200) return null;
      var m = java.util.regex.Pattern.compile("refs/tags/v([0-9][0-9.]*[0-9])").matcher(refs.body());
      String best = null;
      while (m.find()) {
        String v = m.group(1);
        if (!(v.equals(pin) || v.startsWith(pin + "."))) continue;
        if (best == null || cmpVersion(v, best) > 0) best = v;
      }
      return best;
    } catch (Exception e) {
      return null;
    }
  }

  static boolean commandExists(String cmd) {
    try {
      return new ProcessBuilder("sh", "-c", "command -v " + cmd)
          .redirectOutput(ProcessBuilder.Redirect.DISCARD)
          .redirectError(ProcessBuilder.Redirect.DISCARD).start().waitFor() == 0;
    } catch (Exception e) {
      return false;
    }
  }

  static final String JAVA_MAJOR = "25"; // pinned major; bump deliberately

  /** End users never see Java: a pinned Temurin JDK lands next to the pinned OTP,
   *  and bin/zc prefers it — after one toolchain install, zc needs no host Java.
   *  (Full JDK, not JRE: zc runs via the java SOURCE launcher today; drops to a
   *  JRE + compiled zc when the installer ships a jar — Phase 4.2.) */
  static void ensureJre(HttpClient client) throws Exception {
    Path dst = zcHome().resolve("java");
    if (Files.isDirectory(dst.resolve("bin"))) return;
    String os = System.getProperty("os.name").toLowerCase().contains("mac") ? "mac" : "linux";
    String arch = System.getProperty("os.arch").contains("aarch64") ? "aarch64" : "x64";
    String url = "https://api.adoptium.net/v3/binary/latest/" + JAVA_MAJOR
        + "/ga/" + os + "/" + arch + "/jdk/hotspot/normal/eclipse?project=jdk";
    System.out.println("zc: downloading JDK " + JAVA_MAJOR + " (" + os + "/" + arch + ") ...");
    var resp = client.send(HttpRequest.newBuilder(URI.create(url)).build(),
        HttpResponse.BodyHandlers.ofByteArray());
    if (resp.statusCode() != 200) die("zc: fetch " + url + " -> HTTP " + resp.statusCode());
    Path tmp = Files.createTempDirectory("zc-java");
    Path tarball = tmp.resolve("java.tar.gz");
    Files.write(tarball, resp.body());
    exec(tmp, "tar", "xzf", tarball.toString());
    Path root = tmp;
    try (DirectoryStream<Path> ds = Files.newDirectoryStream(tmp)) {
      for (Path p : ds) {
        if (Files.isDirectory(p.resolve("bin"))) root = p; // jdk-25.x.y+z/
      }
    }
    if (root == tmp) die("zc: unexpected JDK tarball layout");
    Files.createDirectories(dst.getParent());
    Files.move(root, dst);
    System.out.println("zc: installed JDK -> " + dst);
  }

  /** rebar3 is one escript — managed next to the OTP it runs on. */
  static void ensureRebar3(HttpClient client) throws Exception {
    Path dst = zcHome().resolve("bin").resolve("rebar3");
    if (Files.exists(dst)) return;
    String url = "https://github.com/erlang/rebar3/releases/latest/download/rebar3";
    System.out.println("zc: downloading rebar3 ...");
    var resp = client.send(HttpRequest.newBuilder(URI.create(url)).build(),
        HttpResponse.BodyHandlers.ofByteArray());
    if (resp.statusCode() != 200) die("zc: fetch " + url + " -> HTTP " + resp.statusCode());
    Files.createDirectories(dst.getParent());
    Files.write(dst, resp.body());
    dst.toFile().setExecutable(true);
    System.out.println("zc: installed rebar3 -> " + dst);
  }

  /** Managed escript when present (~/.zc, then the legacy cache), else PATH rebar3. */
  static String[] rebarCmd(String... args) {
    var cmd = new ArrayList<String>();
    String escript = managedOtpBin != null
        ? managedOtpBin.resolve("escript").toString() : "escript";
    Path managed = zcHome().resolve("bin").resolve("rebar3");
    Path cached = Path.of(System.getProperty("user.home"), ".cache", "zinc", "rebar3");
    if (Files.exists(managed)) {
      cmd.addAll(List.of(escript, managed.toString()));
    } else if (Files.exists(cached) && managedOtpBin != null) {
      cmd.addAll(List.of(escript, cached.toString()));
    } else {
      cmd.add("rebar3");
    }
    cmd.addAll(List.of(args));
    return cmd.toArray(String[]::new);
  }

  static boolean sanityOtp(Path root) {
    try {
      // a REAL crypto op, not application:ensure_all_started(crypto): the latter can return
      // ok even when the NIF's on_load failed, so it let a build whose crypto.so needs
      // symbols this host's OpenSSL lacks (e.g. SM4 on RHEL) pass. crypto:hash forces the
      // NIF to load and actually run.
      var pb = new ProcessBuilder(root.resolve("bin/erl").toString(), "-noshell", "-eval",
          "true=is_binary(crypto:hash(sha256,<<\"zc\">>)),io:format(\"ok\"),init:stop().");
      pb.redirectErrorStream(false);
      Process p = pb.start();
      String out = new String(p.getInputStream().readAllBytes());
      return p.waitFor() == 0 && out.contains("ok");
    } catch (Exception e) {
      return false;
    }
  }

  static void toolchainList() throws IOException {
    Path otp = zcHome().resolve("otp");
    if (!Files.isDirectory(otp)) {
      System.out.println("no toolchains installed (zc toolchain install <version>)");
      return;
    }
    try (DirectoryStream<Path> ds = Files.newDirectoryStream(otp)) {
      for (Path d : ds) {
        if (!Files.isDirectory(d)) continue;
        String flavor = Files.exists(d.resolve(".zc-flavor"))
            ? Files.readString(d.resolve(".zc-flavor")).strip() : "?";
        System.out.println("otp " + d.getFileName() + "  (" + flavor + ")  " + d);
      }
    }
  }

  /** Highest installed OTP matching the pin ("29" matches 29.0.1); null pin = highest
   *  of any version (script mode / pinless projects); null if nothing installed. */
  static Path resolveOtp(String pin) throws IOException {
    Path otp = zcHome().resolve("otp");
    if (!Files.isDirectory(otp)) return null;
    Path best = null;
    try (DirectoryStream<Path> ds = Files.newDirectoryStream(otp)) {
      for (Path d : ds) {
        String v = d.getFileName().toString();
        if (!Files.isDirectory(d.resolve("bin"))) continue;
        if (pin != null && !(v.equals(pin) || v.startsWith(pin + "."))) continue;
        if (best == null || cmpVersion(v, best.getFileName().toString()) > 0) best = d;
      }
    }
    return best;
  }

  static int cmpVersion(String a, String b) {
    String[] xs = a.split("\\."), ys = b.split("\\.");
    for (int i = 0; i < Math.max(xs.length, ys.length); i++) {
      int x = i < xs.length ? Integer.parseInt(xs[i]) : 0;
      int y = i < ys.length ? Integer.parseInt(ys[i]) : 0;
      if (x != y) return Integer.compare(x, y);
    }
    return 0;
  }

  static String sha256(byte[] data) throws Exception {
    var md = java.security.MessageDigest.getInstance("SHA-256");
    var sb = new StringBuilder();
    for (byte b : md.digest(data)) sb.append(String.format("%02x", b));
    return sb.toString();
  }

  static void doctor() throws Exception {
    System.out.println("zc 0.1.0");
    System.out.println("  ZINC_HOME  " + home);
    System.out.println("  ZC_HOME    " + zcHome());
    System.out.println("  java       " + System.getProperty("java.version") + " ("
        + System.getProperty("java.home") + ")"
        + (Files.isDirectory(zcHome().resolve("java").resolve("bin")) ? "" : "  [host java;"
            + " 'zc toolchain install' adds a managed JDK]"));
    toolchainList();
    Path here = Path.of(".");
    if (Files.exists(here.resolve("zinc.toml"))) {
      String pin = loadToml(here).getOrDefault("otp", Map.of()).get("version");
      Path managed = resolveOtp(pin);
      System.out.println("  project    [otp] " + pin + " -> "
          + (managed != null ? "managed " + managed
             : "PATH erl (zc toolchain install " + pin + " to manage)"));
    }
    Path rebarManaged = zcHome().resolve("bin").resolve("rebar3");
    Path rebarCached = Path.of(System.getProperty("user.home"), ".cache", "zinc", "rebar3");
    System.out.println("  rebar3     " + (Files.exists(rebarManaged) ? rebarManaged
        : Files.exists(rebarCached) ? rebarCached + " (legacy cache)" : "PATH"));
  }

  /** Like exec but output only matters on failure. */
  static void execQuiet(Path dir, String... cmd) throws Exception {
    var pb = new ProcessBuilder(cmd).directory(dir.toFile());
    pb.redirectErrorStream(true);
    Process p = pb.start();
    String out = new String(p.getInputStream().readAllBytes());
    if (p.waitFor() != 0) {
      System.err.print(out);
      die("zc: " + String.join(" ", cmd) + " failed");
    }
  }

  // ---- build / run ----

  static Path managedOtpBin; // from the project's [otp] pin, when installed

  /** Resolve the [otp] pin against ~/.zc/otp; all child processes (rebar3, erlc, erl)
   *  then see the managed toolchain first on PATH. No pin or not installed = PATH/docker
   *  as before. */
  static void useManagedOtp(Map<String, Map<String, String>> cfg) throws IOException {
    Path otp = resolveOtp(cfg.getOrDefault("otp", Map.of()).get("version"));
    if (otp != null) managedOtpBin = otp.resolve("bin");
  }

  static void build(Path dir) throws Exception {
    var cfg = loadToml(dir);
    useManagedOtp(cfg);
    ensureGenerated(dir, cfg);
    vendorDeps(dir, cfg);
    execErr(dir, rebarCmd("compile")); // build progress on stderr; stdout stays the program's
  }

  /** zc release — a self-contained OTP release (ERTS + .beam + boot script) via relx,
   *  booting the supervision tree. Only meaningful for a long-running service. */
  static void release(Path dir) throws Exception {
    if (!hasSupervised(dir)) {
      die("zc release needs a long-running service — an Application or an Actor to "
          + "supervise. A plain script just runs: use 'zc run' / 'zc build'.");
    }
    var cfg = loadToml(dir);
    useManagedOtp(cfg);
    ensureGenerated(dir, cfg, true); // relx + {mod,{zinc_app,[]}}
    vendorDeps(dir, cfg);
    exec(dir, rebarCmd("tar")); // builds the release, then tars it
    var project = cfg.getOrDefault("project", Map.of());
    String name = project.getOrDefault("name", dir.toAbsolutePath().getFileName().toString());
    String vsn = project.getOrDefault("version", "0.1.0");
    Path rel = dir.resolve("_build/default/rel").resolve(name);
    Path tar = rel.resolve(name + "-" + vsn + ".tar.gz");
    System.out.println();
    if (Files.exists(tar)) {
      System.out.println("release: " + tar);
    } else {
      System.out.println("release: " + rel + " (tarball under that dir)");
    }
    System.out.println("  self-contained (bundled ERTS). deploy: copy the tarball, "
        + "tar xzf, then bin/" + name + " daemon  (foreground|stop|remote_console too)");
  }

  /** A release needs something to supervise: an Application or an Actor in the sources. */
  static boolean hasSupervised(Path dir) throws IOException {
    Path src = dir.resolve("src");
    if (!Files.isDirectory(src)) return false;
    try (var paths = Files.walk(src)) {
      for (Path p : (Iterable<Path>) paths::iterator) {
        if (!p.toString().endsWith(".zinc") && !p.toString().endsWith(".zn")) continue;
        String s = Files.readString(p);
        if (s.contains("implements Application") || s.contains("implements Actor")
            || s.contains("(Application)") || s.contains("(Actor)")) return true;
      }
    }
    return false;
  }

  /** zc run file.zinc — transpile + erlc + run, one command, no project ceremony.
   *  once=true runs the entry and exits (main + init:stop) even for an Application. */
  static void runSingle(Path file, boolean once) throws Exception {
    if (!Files.exists(file)) die("zc: no such file: " + file);
    useManagedOtp(Map.of()); // no pin: highest installed toolchain, else PATH
    Path out = Files.createTempDirectory("zc-run");
    // transpile + erlc are plumbing: quiet on success (no .erl-path spam, no toolchain
    // noise), full output only if they fail. `zc run` shows just the program's output.
    var transpile = new ArrayList<>(List.of(transpileCmd()));
    transpile.add(file.toString());
    transpile.add(out.toString());
    execQuiet(Path.of("."), transpile.toArray(String[]::new));
    String erlc = managedOtpBin != null ? managedOtpBin.resolve("erlc").toString() : "erlc";
    var compile = new ArrayList<>(List.of(erlc, "-o", out.toString()));
    try (DirectoryStream<Path> ds = Files.newDirectoryStream(out, "*.erl")) {
      for (Path p : ds) compile.add(p.toString());
    }
    execQuiet(out, compile.toArray(String[]::new));
    String erl = managedOtpBin != null ? managedOtpBin.resolve("erl").toString() : "erl";
    // +Bd: Ctrl-C terminates instead of the BEAM BREAK menu (dev-tool expectation)
    runErl(out, out, erl, "+Bd", "-noshell", "-pa", out.toString(), "-eval",
        (once ? "main:main()" : "main:run()") + ", init:stop().");
  }

  static void runOnce(Path dir) throws Exception {
    run(dir, true);
  }

  static void run(Path dir) throws Exception {
    run(dir, false);
  }

  static void run(Path dir, boolean once) throws Exception {
    String erl = managedOtpBin != null ? managedOtpBin.resolve("erl").toString() : "erl";
    var cmd = new ArrayList<>(List.of(erl, "+Bd", "-noshell"));
    for (String d : List.of("_build/default/lib", "_build/default/checkouts")) {
      Path base = dir.resolve(d);
      if (!Files.isDirectory(base)) continue;
      try (DirectoryStream<Path> apps = Files.newDirectoryStream(base)) {
        for (Path app : apps) {
          if (Files.isDirectory(app.resolve("ebin"))) {
            cmd.add("-pa");
            cmd.add(app.resolve("ebin").toString());
          }
        }
      }
    }
    cmd.add("-eval");
    cmd.add((once ? "main:main()" : "main:run()") + ", init:stop().");
    // generated .erl (with @zinc-src + %@L markers) live under src/zinc_gen for crash mapping
    runErl(dir, dir.resolve("src").resolve("zinc_gen"), cmd.toArray(String[]::new));
  }

  /** The zc.jar we're running from, or null in dev (source-launcher) mode. */
  static Path selfJar() {
    try {
      var loc = Zc.class.getProtectionDomain().getCodeSource().getLocation();
      if (loc == null) return null;
      Path p = Path.of(loc.toURI());
      return p.toString().endsWith(".jar") ? p : null;
    } catch (Exception e) {
      return null;
    }
  }

  /** Transpiler launch prefix: from the jar in a release (`java -cp zc.jar zinc.Main`),
   *  or the source launcher in dev (`java src/zinc/Main.java`). */
  static String[] transpileCmd() {
    String java = Path.of(System.getProperty("java.home"), "bin", "java").toString();
    Path jar = selfJar();
    return jar != null
        ? new String[] {java, "-cp", jar.toString(), "zinc.Main"}
        : new String[] {java, home.resolve("src/zinc/Main.java").toString()};
  }

  /** Like exec, but the child's stdout goes to stderr — for build/progress output so a
   *  `zc run` of a project keeps stdout to the program alone. */
  static void execErr(Path dir, String... cmd) throws Exception {
    var pb = new ProcessBuilder(cmd).directory(dir.toFile());
    pb.redirectInput(ProcessBuilder.Redirect.INHERIT);
    pb.redirectError(ProcessBuilder.Redirect.INHERIT);
    pb.redirectOutput(ProcessBuilder.Redirect.appendTo(new java.io.File("/dev/stderr")));
    pb.environment().put("ZINC_HOME", home.toString());
    pb.environment().put("ZINC_JAVA",
        Path.of(System.getProperty("java.home"), "bin", "java").toString());
    if (managedOtpBin != null) {
      pb.environment().put("PATH",
          managedOtpBin + ":" + pb.environment().getOrDefault("PATH", ""));
    }
    int code = pb.start().waitFor();
    if (code != 0) System.exit(code);
  }

  static void exec(Path dir, String... cmd) throws Exception {
    var pb = new ProcessBuilder(cmd).directory(dir.toFile()).inheritIO();
    pb.environment().put("ZINC_HOME", home.toString());
    // the rebar_zinc plugin shells out to the transpiler; point it at the java we run on
    pb.environment().put("ZINC_JAVA",
        Path.of(System.getProperty("java.home"), "bin", "java").toString());
    if (managedOtpBin != null) {
      pb.environment().put("PATH",
          managedOtpBin + ":" + pb.environment().getOrDefault("PATH", ""));
    }
    int code = pb.start().waitFor();
    if (code != 0) System.exit(code);
  }

  /** Run erl with stdout inherited (program output) but stderr captured-and-teed, so on a
   *  crash we can append a zinc-flavored trace mapping erl frames back to .zinc:line.
   *  srcDir = where the generated .erl (with @zinc-src + %@L markers) live. */
  static void runErl(Path dir, Path srcDir, String... cmd) throws Exception {
    var pb = new ProcessBuilder(cmd).directory(dir.toFile());
    pb.environment().put("ZINC_HOME", home.toString());
    pb.environment().put("ERL_CRASH_DUMP_SECONDS", "0"); // no erl_crash.dump litter
    if (managedOtpBin != null) {
      pb.environment().put("PATH",
          managedOtpBin + ":" + pb.environment().getOrDefault("PATH", ""));
    }
    pb.redirectOutput(ProcessBuilder.Redirect.INHERIT);
    pb.redirectError(ProcessBuilder.Redirect.PIPE);
    Process p = pb.start();
    var captured = new StringBuilder();
    try (var r = new BufferedReader(new InputStreamReader(p.getErrorStream()))) {
      String line;
      while ((line = r.readLine()) != null) {
        // managed-toolchain noise on some hosts; not the program's doing
        if (line.contains("libtinfo.so") && line.contains("no version information")) continue;
        System.err.println(line);
        captured.append(line).append('\n');
      }
    }
    int code = p.waitFor();
    if (code != 0) {
      String trace = annotateCrash(captured.toString(), srcDir);
      if (trace != null) System.err.print(trace);
      System.exit(code);
    }
  }

  // erl stack frame: {Module, Fun, Arity, [{file,"x.erl"},{line,N}]}
  static final Pattern FRAME = Pattern.compile(
      "\\{('[^']+'|[a-zA-Z][\\w.]*)\\s*,\\s*([a-zA-Z_]\\w*)\\s*,\\s*(\\d+)\\s*,\\s*"
          + "\\[\\{file,\"([^\"]+)\"\\},\\s*\\{line,(\\d+)\\}\\]\\}");
  static final Pattern MARKER = Pattern.compile("%@L(\\d+)");

  /** Turn the raw erl stacktrace into a zinc trace: each frame from a generated module
   *  (one carrying `%% @zinc-src`) maps back to its .zinc file + the nearest `%@L` marker
   *  at/above the crash line. BEAM-internal frames are dropped. null = nothing to add. */
  static String annotateCrash(String stderr, Path srcDir) throws IOException {
    var frames = new ArrayList<String>();
    var seen = new HashSet<String>(); // a crash is reported several times; show each frame once
    Matcher m = FRAME.matcher(stderr);
    while (m.find()) {
      String erlFile = m.group(4);
      int erlLine = Integer.parseInt(m.group(5));
      if (!seen.add(erlFile + ":" + erlLine + ":" + m.group(2))) continue;
      Path erl = srcDir == null ? null : srcDir.resolve(Path.of(erlFile).getFileName());
      if (erl == null || !Files.exists(erl)) continue;
      List<String> lines = Files.readAllLines(erl);
      String src = null;
      for (String l : lines) {
        if (l.startsWith("%% @zinc-src ")) { src = l.substring(12).trim(); break; }
      }
      if (src == null) continue; // a runtime/lib module, not user code
      int srcLine = -1;
      for (int i = Math.min(erlLine, lines.size()) - 1; i >= 0; i--) {
        Matcher mk = MARKER.matcher(lines.get(i));
        if (mk.find()) { srcLine = Integer.parseInt(mk.group(1)); break; }
      }
      String where = srcLine > 0 ? src + ":" + srcLine : src;
      String fn = m.group(2).equals("user_main") ? "main" : m.group(2);
      frames.add(String.format("    %-26s %s/%s   (%s:%d)",
          where, fn, m.group(3), erlFile, erlLine));
    }
    if (frames.isEmpty()) return null;
    var sb = new StringBuilder("\n  ── zinc trace (most recent call first) ──\n");
    String reason = crashReason(stderr);
    if (reason != null) sb.append("  ").append(reason).append('\n');
    for (String f : frames) sb.append(f).append('\n');
    return sb.toString();
  }

  /** Decode the common BEAM error classes into one plain-English line. */
  static String crashReason(String s) {
    String[][] table = {
        {"zinc_badtype", "zinc_badtype — a value crossed a typed boundary with the wrong type"},
        {"zinc_exc", "uncaught zinc exception"},
        {"badarith", "badarith — bad arithmetic (e.g. divide by zero)"},
        {"function_clause", "function_clause — no method clause matched the arguments"},
        {"case_clause", "case_clause — no branch matched the value"},
        {"badkey", "badkey — read a map/struct key that isn't there"},
        {"badmatch", "badmatch — a value didn't match the expected shape"},
        {"undef", "undef — called a function that doesn't exist (check an FFI module/arity)"},
        {"badarg", "badarg — bad argument to a built-in operation"},
    };
    for (String[] r : table) if (s.contains(r[0])) return r[1];
    return null;
  }

  // ---- zinc.toml (line-based TOML subset, like zinc-go's) ----

  static Map<String, Map<String, String>> loadToml(Path dir) throws IOException {
    var cfg = new LinkedHashMap<String, Map<String, String>>();
    String section = "";
    for (String raw : Files.readAllLines(dir.resolve("zinc.toml"))) {
      String line = raw.strip();
      if (line.isEmpty() || line.startsWith("#")) continue;
      if (line.startsWith("[") && line.endsWith("]")) {
        section = line.substring(1, line.length() - 1).strip();
        continue;
      }
      int eq = line.indexOf('=');
      if (eq < 0) die("zinc.toml: bad line: " + raw);
      String key = line.substring(0, eq).strip();
      String val = line.substring(eq + 1).strip();
      if (val.startsWith("\"") && val.endsWith("\"")) val = val.substring(1, val.length() - 1);
      cfg.computeIfAbsent(section, s -> new LinkedHashMap<>()).put(key, val);
    }
    return cfg;
  }

  static void add(Path dir, String spec) throws IOException {
    int at = spec.indexOf('@');
    if (at < 0) die("usage: zc add <name@version>, e.g. zc add cowboy@2.12.0");
    String name = spec.substring(0, at);
    String ver = spec.substring(at + 1);
    Path toml = dir.resolve("zinc.toml");
    var lines = new ArrayList<>(Files.readAllLines(toml));
    String entry = name + " = \"" + ver + "\"";
    int depsAt = lines.indexOf("[deps]");
    if (depsAt < 0) {
      lines.add("");
      lines.add("[deps]");
      lines.add(entry);
    } else {
      lines.add(depsAt + 1, entry);
    }
    Files.write(toml, lines);
    System.out.println("added " + entry);
  }

  // ---- fmt: a line-based reindenter (the lexer drops comments, so we format text, not
  // tokens — preserves comments + strings, idempotent). 2-space indent by brace depth,
  // trailing whitespace trimmed, blank runs collapsed to one, single trailing newline.

  static boolean isSource(Path p) {
    return p.toString().endsWith(".zinc") || p.toString().endsWith(".zn");
  }

  static void fmt(Path target) throws IOException {
    if (Files.isDirectory(target)) {
      try (var paths = Files.walk(target)) {
        for (Path p : (Iterable<Path>) paths::iterator) {
          if (isSource(p)) fmtFile(p);
        }
      }
    } else if (isSource(target)) {
      fmtFile(target);
    } else {
      die("zc fmt: not a .zinc/.zn file or directory: " + target);
    }
  }

  static void fmtFile(Path p) throws IOException {
    String src = Files.readString(p);
    String out = reformat(src);
    if (!out.equals(src)) {
      Files.writeString(p, out);
      System.out.println("formatted " + p);
    }
  }

  static String reformat(String src) {
    var out = new StringBuilder();
    int depth = 0, pendingBlank = 0;
    for (String raw : src.split("\n", -1)) {
      String s = raw.strip();
      if (s.isEmpty()) { pendingBlank = 1; continue; } // collapse blank runs, drop leading
      if (out.length() > 0 && pendingBlank == 1) out.append('\n');
      pendingBlank = 0;
      char c0 = s.charAt(0);
      boolean firstCloser = c0 == '}' || c0 == ')' || c0 == ']';
      int indent = firstCloser ? Math.max(0, depth - 1) : depth;
      out.append("  ".repeat(indent)).append(s).append('\n');
      depth = Math.max(0, depth + braceDelta(s));
    }
    String r = out.toString();
    return r.endsWith("\n") ? r : r + "\n";
  }

  /** Net open-minus-close of {}/()/[] on a line, skipping string literals and // comments. */
  static int braceDelta(String s) {
    int delta = 0;
    boolean inStr = false;
    for (int i = 0; i < s.length(); i++) {
      char c = s.charAt(i);
      if (inStr) {
        if (c == '\\') i++;
        else if (c == '"') inStr = false;
      } else if (c == '"') {
        inStr = true;
      } else if (c == '/' && i + 1 < s.length() && s.charAt(i + 1) == '/') {
        break;
      } else if (c == '{' || c == '(' || c == '[') {
        delta++;
      } else if (c == '}' || c == ')' || c == ']') {
        delta--;
      }
    }
    return delta;
  }

  static void die(String msg) {
    System.err.println(msg);
    System.exit(1);
  }

  static void usage() {
    System.out.println("""
        usage: zc <command> [args]

          zc init [--java|--py] <name>
                                create a new project (default = canonical src/main.zn,
                                --py = legacy Python-shaped .zn, --java = legal-Java .zinc)
          zc build [dir]        transpile .zinc/.zn + compile (rebar3 under the hood)
          zc run [dir|file]     build + run main(); a .zinc/.zn file runs in script mode
          zc release [dir]      self-contained OTP release tarball (ERTS + beam + boot)
          zc test [dir]         run the test suite (EUnit underneath)
          zc clean [dir]        remove build output and generated .erl
          zc check [dir]        opt-in static net: xref + dialyzer over the FFI basement
          zc add <name@ver>     add a hex dependency to zinc.toml
          zc deps [dir]         list dependencies
          zc toolchain install [ver]   install a managed OTP into ~/.zc/otp (rustup model)
          zc toolchain list            list managed toolchains
          zc doctor             check the install: versions, toolchains, resolution
          zc fmt <file|dir>     reindent .zinc/.zn (2-space, by brace depth)
          zc version            show version
        """);
  }
}
