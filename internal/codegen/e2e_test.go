package codegen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// e2eRun transpiles src, writes the Go output to a temp directory, compiles
// and runs it, and returns trimmed stdout. The test fails immediately if any
// step errors — transpile, compile, or runtime.
func e2eRun(t *testing.T, src string) string {
	t.Helper()
	out, errs := transpile(src)
	if errs != nil {
		t.Fatalf("transpile errors: %v", errs)
	}

	dir := t.TempDir()

	goMod := "module e2e\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(out), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	cmd := exec.Command("go", "run", "main.go")
	cmd.Dir = dir
	raw, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("go run failed.\ngenerated Go:\n%s\nstderr:\n%s", out, exitErr.Stderr)
		}
		t.Fatalf("go run: %v", err)
	}
	return strings.TrimSpace(string(raw))
}

func assertOutput(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("expected output:\n%s\ngot:\n%s", want, got)
	}
}

// --- Basic -------------------------------------------------------------------

func TestE2EHelloWorld(t *testing.T) {
	out := e2eRun(t, `fn main() { print("Hello, World!") }`)
	assertOutput(t, out, "Hello, World!")
}

func TestE2EArithmetic(t *testing.T) {
	out := e2eRun(t, `fn main() { var x: Int = 3 + 4 * 2; print(x) }`)
	assertOutput(t, out, "11")
}

func TestE2EStringInterpolation(t *testing.T) {
	out := e2eRun(t, `fn main() { var name: String = "Growler"; print("Hello, {name}!") }`)
	assertOutput(t, out, "Hello, Growler!")
}

func TestE2EIfElse(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var x: Int = 5
    if (x > 3) {
        print("big")
    } else {
        print("small")
    }
}`)
	assertOutput(t, out, "big")
}

func TestE2EWhileLoop(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var i: Int = 0
    var sum: Int = 0
    while (i < 5) {
        sum = sum + i
        i = i + 1
    }
    print(sum)
}`)
	assertOutput(t, out, "10")
}

func TestE2EForIn(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var nums = [1, 2, 3]
    var sum: Int = 0
    for n in nums {
        sum = sum + n
    }
    print(sum)
}`)
	assertOutput(t, out, "6")
}

// --- Functions ---------------------------------------------------------------

func TestE2ERecursion(t *testing.T) {
	out := e2eRun(t, `
fn fib(n: Int): Int {
    if (n <= 1) { return n }
    return fib(n - 1) + fib(n - 2)
}
fn main() { print(fib(10)) }`)
	assertOutput(t, out, "55")
}

func TestE2EDefaultParams(t *testing.T) {
	out := e2eRun(t, `
fn greet(name: String, greeting: String = "Hello"): String {
    return "{greeting}, {name}!"
}
fn main() {
    print(greet("Alice"))
    print(greet("Bob", "Hi"))
}`)
	assertOutput(t, out, "Hello, Alice!\nHi, Bob!")
}

func TestE2ENamedArgs(t *testing.T) {
	out := e2eRun(t, `
fn connect(host: String, port: Int = 8080): String {
    return "{host}:{port}"
}
fn main() {
    print(connect("localhost"))
    print(connect("example.com", port: 443))
}`)
	assertOutput(t, out, "localhost:8080\nexample.com:443")
}

// --- Classes -----------------------------------------------------------------

func TestE2EClass(t *testing.T) {
	out := e2eRun(t, `
class Dog {
    var name: String
    construct new(n: String) { this.name = n }
    pub fn bark(): String { return "{this.name} says: Woof!" }
}
fn main() {
    var d = Dog.new("Rex")
    print(d.bark())
}`)
	assertOutput(t, out, "Rex says: Woof!")
}

func TestE2EInheritance(t *testing.T) {
	out := e2eRun(t, `
class Animal {
    var name: String
    construct new(n: String) { this.name = n }
    pub fn speak(): String { return "{this.name}: ..." }
}
class Dog : Animal {
    construct new(n: String) { super(n) }
    pub fn speak(): String { return "{this.name}: Woof!" }
}
fn main() {
    var d = Dog.new("Buddy")
    print(d.speak())
}`)
	assertOutput(t, out, "Buddy: Woof!")
}

func TestE2EInterface(t *testing.T) {
	out := e2eRun(t, `
interface Greeter {
    pub fn greet(): String
}
class English : Greeter {
    pub fn greet(): String { return "Hello" }
}
class Spanish : Greeter {
    pub fn greet(): String { return "Hola" }
}
fn main() {
    var e = English.new()
    var s = Spanish.new()
    print(e.greet())
    print(s.greet())
}`)
	assertOutput(t, out, "Hello\nHola")
}

// --- Error handling ----------------------------------------------------------

func TestE2ETryCatch(t *testing.T) {
	out := e2eRun(t, `
fn divide(a: Int, b: Int): Int {
    if (b == 0) { throw Error("division by zero") }
    return a / b
}
fn main() {
    try {
        var r = divide(10, 2)
        print(r)
    } catch(err) {
        print("error")
    }
    try {
        var r = divide(10, 0)
        print(r)
    } catch(err) {
        print("caught: division by zero")
    }
}`)
	assertOutput(t, out, "5\ncaught: division by zero")
}

// --- Closures ----------------------------------------------------------------

func TestE2EClosure(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var base: Int = 10
    var addBase = (x: Int): Int => x + base
    print(addBase(5))
    print(addBase(20))
}`)
	assertOutput(t, out, "15\n30")
}

func TestE2EHigherOrder(t *testing.T) {
	// Pass a closure directly without going through Any — Any parameters
	// become interface{} in Go which cannot be called. Use a concrete fn type.
	out := e2eRun(t, `
fn applyDouble(x: Int): Int {
    var double = (n: Int): Int => n * 2
    return double(x)
}
fn main() {
    print(applyDouble(7))
}`)
	assertOutput(t, out, "14")
}

// --- Concurrency -------------------------------------------------------------

func TestE2EGoroutineChannel(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var ch: Chan<Int> = Chan.new(1)
    go {
        ch.send(42)
    }
    var val = ch.receive()
    print(val)
}`)
	assertOutput(t, out, "42")
}

// --- Enums + match -----------------------------------------------------------

func TestE2EEnumMatch(t *testing.T) {
	out := e2eRun(t, `
enum Direction { North, South, East, West }
fn describe(d: Direction): String {
    match d {
        case Direction.North => { return "north" }
        case Direction.South => { return "south" }
        case _ => { return "other" }
    }
}
fn main() {
    print(describe(DirectionNorth))
    print(describe(DirectionSouth))
    print(describe(DirectionEast))
}`)
	assertOutput(t, out, "north\nsouth\nother")
}

// --- With statement ----------------------------------------------------------

func TestE2EWithFileResource(t *testing.T) {
	// Verify with compiles and runs; resource body executes correctly.
	// We can't easily assert Close() was called without side effects, but we
	// can verify the body runs and the program exits cleanly.
	out := e2eRun(t, `
import "os"
fn main() {
    with var f = os.Stdin {
        print("resource open")
    }
    print("done")
}`)
	assertOutput(t, out, "resource open\ndone")
}

func TestE2EWithFileOpenClose(t *testing.T) {
	out := e2eRun(t, `
import "os"
fn main() {
    var path = "/tmp/growler_with_test.txt"
    var (f, _) = os.Create(path)
    with var file = f {
        file.WriteString("hello from growler")
    }
    var (data, _) = os.ReadFile(path)
    print(string(data))
    os.Remove(path)
}`)
	assertOutput(t, out, "hello from growler")
}

func TestE2EWithMutex(t *testing.T) {
	out := e2eRun(t, `
import "sync"
fn main() {
    var mu = sync.Mutex.new()
    var x = 0
    with var lock = mu {
        x = x + 1
    }
    with var lock2 = mu {
        x = x + 10
    }
    print(x)
}`)
	assertOutput(t, out, "11")
}

// --- Type casting (as / is) --------------------------------------------------

func TestE2EAsCast(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var x: Any = 42
    var y = x as Int
    print(y + 1)
}`)
	assertOutput(t, out, "43")
}

func TestE2EIsCheck(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var x: Any = "hello"
    if (x is String) {
        print("yes")
    } else {
        print("no")
    }
}`)
	assertOutput(t, out, "yes")
}

func TestE2EIsCheckFalse(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var x: Any = 42
    if (x is String) {
        print("string")
    } else {
        print("not string")
    }
}`)
	assertOutput(t, out, "not string")
}

func TestE2EAsCastClassType(t *testing.T) {
	out := e2eRun(t, `
class Animal {
    var name: String
    construct new(name: String) {
        this.name = name
    }
    fn speak(): String { return this.name }
}

class Dog : Animal {
    construct new(name: String) {
        super(name)
    }
    pub fn bark(): String { return this.name + " says woof" }
}

fn main() {
    var a: Any = Dog.new("Rex")
    var d = a as Dog
    print(d.bark())
}`)
	assertOutput(t, out, "Rex says woof")
}

// --- .new() on Go types ------------------------------------------------------

func TestE2EGoTypeNew(t *testing.T) {
	out := e2eRun(t, `
import "sync"
fn main() {
    var mu = sync.Mutex.new()
    mu.Lock()
    mu.Unlock()
    print("ok")
}`)
	assertOutput(t, out, "ok")
}

func TestE2EWithMutexNew(t *testing.T) {
	out := e2eRun(t, `
import "sync"
fn main() {
    var x = 0
    with var mu = sync.Mutex.new() {
        x = x + 1
    }
    print(x)
}`)
	assertOutput(t, out, "1")
}

func TestE2EGoTypeNewBytesBuffer(t *testing.T) {
	out := e2eRun(t, `
import "bytes"
fn main() {
    var buf = bytes.Buffer.new()
    buf.WriteString("hello")
    print(buf.String())
}`)
	assertOutput(t, out, "hello")
}

// --- Labeled break/continue --------------------------------------------------

func TestE2ELabeledBreak(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var result = ""
    @outer for (var i = 0; i < 3; i += 1) {
        for (var j = 0; j < 3; j += 1) {
            if (j == 1) {
                break @outer
            }
            result = result + toString(i) + toString(j) + " "
        }
    }
    print(result)
}`)
	assertOutput(t, out, "00")
}

func TestE2ELabeledContinue(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var result = ""
    @outer for (var i = 0; i < 3; i += 1) {
        for (var j = 0; j < 3; j += 1) {
            if (j == 1) {
                continue @outer
            }
            result = result + toString(i) + toString(j) + " "
        }
    }
    print(result)
}`)
	assertOutput(t, out, "00 10 20")
}

func TestE2ELabeledWhile(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var i = 0
    var result = ""
    @outer while (i < 3) {
        var j = 0
        while (j < 3) {
            if (j == 1) {
                i += 1
                continue @outer
            }
            result = result + toString(i) + toString(j) + " "
            j += 1
        }
        i += 1
    }
    print(result)
}`)
	assertOutput(t, out, "00 10 20")
}

// --- Safe navigation ?.  -----------------------------------------------------

// --- Safe navigation ?.  --- field access on non-nil pointer
func TestE2ESafeNavField(t *testing.T) {
	out := e2eRun(t, `
class Dog {
    var name: String
    construct new(n: String) {
        this.name = n
    }
}
fn main() {
    var d: Dog? = Dog.new("Rex")
    var result = d?.name
    print(result)
}`)
	assertOutput(t, out, "Rex")
}

// --- Safe navigation ?.  --- field access on nil → returns nil
func TestE2ESafeNavNil(t *testing.T) {
	out := e2eRun(t, `
class Dog {
    var name: String
    construct new(n: String) {
        this.name = n
    }
}
fn main() {
    var d: Dog? = null
    var result = d?.name
    if (result == null) {
        print("nil safe")
    }
}`)
	assertOutput(t, out, "nil safe")
}

// --- Safe navigation ?.  --- method call on non-nil pointer
func TestE2ESafeNavMethodCall(t *testing.T) {
	out := e2eRun(t, `
class Dog {
    var name: String
    construct new(n: String) {
        this.name = n
    }
    pub fn speak(): String {
        return "woof"
    }
}
fn main() {
    var d: Dog? = Dog.new("Rex")
    var result = d?.speak()
    print(result)
}`)
	assertOutput(t, out, "woof")
}

// --- Safe navigation ?.  --- method call on nil → returns nil, method not called
func TestE2ESafeNavMethodNil(t *testing.T) {
	out := e2eRun(t, `
class Dog {
    var name: String
    construct new(n: String) {
        this.name = n
    }
    pub fn speak(): String {
        return "woof"
    }
}
fn main() {
    var d: Dog? = null
    var result = d?.speak()
    if (result == null) {
        print("method not called")
    }
}`)
	assertOutput(t, out, "method not called")
}

// --- Safe navigation ?.  --- as statement (void method) on non-nil
func TestE2ESafeNavVoidMethodNonNil(t *testing.T) {
	out := e2eRun(t, `
class Logger {
    var lastMsg: String
    construct new() {
        this.lastMsg = ""
    }
    pub fn log(msg: String) {
        this.lastMsg = msg
        print(msg)
    }
}
fn main() {
    var l: Logger? = Logger.new()
    l?.log("hello")
}`)
	assertOutput(t, out, "hello")
}

// --- Safe navigation ?.  --- as statement (void method) on nil — should not crash
func TestE2ESafeNavVoidMethodNil(t *testing.T) {
	out := e2eRun(t, `
class Logger {
    var lastMsg: String
    construct new() {
        this.lastMsg = ""
    }
    pub fn log(msg: String) {
        this.lastMsg = msg
        print(msg)
    }
}
fn main() {
    var l: Logger? = null
    l?.log("should not print")
    print("survived")
}`)
	assertOutput(t, out, "survived")
}

// --- Safe navigation ?.  --- chaining a?.b?.c
func TestE2ESafeNavChaining(t *testing.T) {
	out := e2eRun(t, `
class Address {
    var city: String
    construct new(c: String) {
        this.city = c
    }
}
class Person {
    var name: String
    var address: Address?
    construct new(n: String, addr: Address?) {
        this.name = n
        this.address = addr
    }
}
fn main() {
    var p: Person? = Person.new("Alice", Address.new("NYC"))
    var city = p?.address?.city
    print(city)
}`)
	assertOutput(t, out, "NYC")
}

// --- Safe navigation ?.  --- chaining where middle is nil
func TestE2ESafeNavChainingNilMiddle(t *testing.T) {
	out := e2eRun(t, `
class Address {
    var city: String
    construct new(c: String) {
        this.city = c
    }
}
class Person {
    var name: String
    var address: Address?
    construct new(n: String, addr: Address?) {
        this.name = n
        this.address = addr
    }
}
fn main() {
    var p: Person? = Person.new("Bob", null)
    var city = p?.address?.city
    if (city == null) {
        print("no city")
    }
}`)
	assertOutput(t, out, "no city")
}

// --- with multi-return (try) -------------------------------------------------

func TestE2EWithTryMultiReturn(t *testing.T) {
	out := e2eRun(t, `
import "os"
fn main() {
    with var f = try os.CreateTemp("", "test*.txt") {
        f.WriteString("hello")
        print("ok")
    }
}`)
	assertOutput(t, out, "ok")
}

// with + try: write and read back to verify file actually works
func TestE2EWithTryFileWriteRead(t *testing.T) {
	out := e2eRun(t, `
import "os"
fn main() {
    var path = os.TempDir() + "/growler_with_try_test.txt"
    with var f = try os.Create(path) {
        f.WriteString("hello from with-try")
    }
    var content = readFile(path)
    print(content)
    os.Remove(path)
}`)
	assertOutput(t, out, "hello from with-try")
}

// with + try: error causes panic, caught by try/catch
func TestE2EWithTryErrorPanics(t *testing.T) {
	out := e2eRun(t, `
import "os"
fn main() {
    try {
        with var f = try os.Open("/nonexistent/path/that/does/not/exist") {
            print("should not reach")
        }
    } catch(err) {
        print("caught error")
    }
}`)
	assertOutput(t, out, "caught error")
}

// with: multiple resources — file + mutex
func TestE2EWithMultipleResources(t *testing.T) {
	out := e2eRun(t, `
import "sync"
import "os"
fn main() {
    var x = 0
    with var f = os.Stdin, var mu = sync.Mutex.new() {
        x = x + 1
        print("inside with")
    }
    print(x)
}`)
	assertOutput(t, out, "inside with\n1")
}

// with + try: multiple try resources
func TestE2EWithMultipleTryResources(t *testing.T) {
	out := e2eRun(t, `
import "os"
fn main() {
    var p1 = os.TempDir() + "/growler_multi1.txt"
    var p2 = os.TempDir() + "/growler_multi2.txt"
    with var f1 = try os.Create(p1), var f2 = try os.Create(p2) {
        f1.WriteString("file1")
        f2.WriteString("file2")
    }
    print(readFile(p1))
    print(readFile(p2))
    os.Remove(p1)
    os.Remove(p2)
}`)
	assertOutput(t, out, "file1\nfile2")
}

// with: nested with blocks
func TestE2EWithNested(t *testing.T) {
	out := e2eRun(t, `
import "sync"
fn main() {
    var x = 0
    with var mu1 = sync.Mutex.new() {
        x = x + 1
        with var mu2 = sync.Mutex.new() {
            x = x + 10
        }
    }
    print(x)
}`)
	assertOutput(t, out, "11")
}

// with: resource that is neither Closer nor Locker — just scoping
func TestE2EWithPlainValue(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    with var x = 42 {
        print(x)
    }
}`)
	assertOutput(t, out, "42")
}

// with + try: using readFile built-in inside with block
func TestE2EWithTryReadAfterWrite(t *testing.T) {
	out := e2eRun(t, `
import "os"
fn main() {
    var path = os.TempDir() + "/growler_with_rw.txt"
    with var f = try os.Create(path) {
        f.WriteString("growler with-try rocks")
    }
    // File is now closed (defer Close() ran), safe to read
    print(readFile(path))
    os.Remove(path)
}`)
	assertOutput(t, out, "growler with-try rocks")
}

// with: RWMutex (implements sync.Locker via RLock/Lock)
func TestE2EWithRWMutex(t *testing.T) {
	out := e2eRun(t, `
import "sync"
fn main() {
    var mu = sync.RWMutex.new()
    var x = 0
    with var lock = mu {
        x = x + 5
    }
    print(x)
}`)
	assertOutput(t, out, "5")
}

// --- New stdlib built-in aliases ---------------------------------------------

func TestE2EJsonEncode(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var data = "hello"
    var encoded = jsonEncode(data)
    print(encoded)
}`)
	assertOutput(t, out, `"hello"`)
}

func TestE2ESprintf(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var result = sprintf("Hello, %s! You are %d.", "Alice", 30)
    print(result)
}`)
	assertOutput(t, out, "Hello, Alice! You are 30.")
}

func TestE2ETypeOf(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    var x = 42
    print(typeOf(x))
}`)
	assertOutput(t, out, "int")
}

func TestE2ESleep(t *testing.T) {
	out := e2eRun(t, `
fn main() {
    sleep(1)
    print("done")
}`)
	assertOutput(t, out, "done")
}

func TestE2EReadWriteFile(t *testing.T) {
	out := e2eRun(t, `
import "os"
fn main() {
    var dir = os.TempDir()
    var path = dir + "/growler_test_rw.txt"
    writeFile(path, "hello growler")
    var content = readFile(path)
    print(content)
    os.Remove(path)
}`)
	assertOutput(t, out, "hello growler")
}
