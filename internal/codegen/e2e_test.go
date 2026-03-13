// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

	goMod := "module e2e\n\ngo 1.26\n"
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
	out := e2eRun(t, `main() { print("Hello, World!") }`)
	assertOutput(t, out, "Hello, World!")
}

func TestE2EArithmetic(t *testing.T) {
	out := e2eRun(t, `main() { x := 3 + 4 * 2; print(x) }`)
	assertOutput(t, out, "11")
}

func TestE2EStringInterpolation(t *testing.T) {
	out := e2eRun(t, `main() { name := "Zinc"; print("Hello, {name}!") }`)
	assertOutput(t, out, "Hello, Zinc!")
}

func TestE2EIfElse(t *testing.T) {
	out := e2eRun(t, `
main() {
    x := 5
    if x > 3 {
        print("big")
    } else {
        print("small")
    }
}`)
	assertOutput(t, out, "big")
}

func TestE2EWhileLoop(t *testing.T) {
	out := e2eRun(t, `
main() {
    i := 0
    sum := 0
    while i < 5 {
        sum = sum + i
        i = i + 1
    }
    print(sum)
}`)
	assertOutput(t, out, "10")
}

func TestE2EForIn(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3]
    sum := 0
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
fib(n Int) Int {
    if n <= 1 { return n }
    return fib(n - 1) + fib(n - 2)
}
main() { print(fib(10)) }`)
	assertOutput(t, out, "55")
}

func TestE2EDefaultParams(t *testing.T) {
	out := e2eRun(t, `
greet(name String, greeting String = "Hello") String {
    return "{greeting}, {name}!"
}
main() {
    print(greet("Alice"))
    print(greet("Bob", "Hi"))
}`)
	assertOutput(t, out, "Hello, Alice!\nHi, Bob!")
}

func TestE2ENamedArgs(t *testing.T) {
	out := e2eRun(t, `
connect(host String, port Int = 8080) String {
    return "{host}:{port}"
}
main() {
    print(connect("localhost"))
    print(connect("example.com", port: 443))
}`)
	assertOutput(t, out, "localhost:8080\nexample.com:443")
}

// --- Classes -----------------------------------------------------------------

func TestE2EClass(t *testing.T) {
	out := e2eRun(t, `
Dog {
    name String
    new(n String) { this.name = n }
    pub bark() String { return "{this.name} says: Woof!" }
}
main() {
    d := Dog("Rex")
    print(d.bark())
}`)
	assertOutput(t, out, "Rex says: Woof!")
}

func TestE2EInheritance(t *testing.T) {
	out := e2eRun(t, `
Animal {
    name String
    new(n String) { this.name = n }
    pub speak() String { return "{this.name}: ..." }
}
Dog : Animal {
    new(n String) { super(n) }
    pub speak() String { return "{this.name}: Woof!" }
}
main() {
    d := Dog("Buddy")
    print(d.speak())
}`)
	assertOutput(t, out, "Buddy: Woof!")
}

func TestE2EInterface(t *testing.T) {
	out := e2eRun(t, `
interface Greeter {
    pub greet() String
}
English : Greeter {
    pub greet() String { return "Hello" }
}
Spanish : Greeter {
    pub greet() String { return "Hola" }
}
main() {
    e := English()
    s := Spanish()
    print(e.greet())
    print(s.greet())
}`)
	assertOutput(t, out, "Hello\nHola")
}

// --- Error handling ----------------------------------------------------------

func TestE2EReturnErrorAndOrHandler(t *testing.T) {
	out := e2eRun(t, `
divide(a Int, b Int) Int {
    if b == 0 { return Error("division by zero") }
    return a / b
}
main() {
    r := divide(10, 2)
    print(r)
    r2 := divide(10, 0) or {
        print("caught: division by zero")
        exit(0)
    }
    print(r2)
}`)
	assertOutput(t, out, "5\ncaught: division by zero")
}

// --- Closures ----------------------------------------------------------------

func TestE2EClosure(t *testing.T) {
	out := e2eRun(t, `
main() {
    base := 10
    addBase := (x Int) Int => x + base
    print(addBase(5))
    print(addBase(20))
}`)
	assertOutput(t, out, "15\n30")
}

func TestE2EHigherOrder(t *testing.T) {
	// Pass a closure directly without going through Any — Any parameters
	// become interface{} in Go which cannot be called. Use a concrete fn type.
	out := e2eRun(t, `
applyDouble(x Int) Int {
    double := (n Int) Int => n * 2
    return double(x)
}
main() {
    print(applyDouble(7))
}`)
	assertOutput(t, out, "14")
}

// --- Concurrency -------------------------------------------------------------

func TestE2EGoroutineChannel(t *testing.T) {
	out := e2eRun(t, `
main() {
    ch Chan<Int> = Chan.new(1)
    go {
        ch.send(42)
    }
    val := ch.receive()
    print(val)
}`)
	assertOutput(t, out, "42")
}

// --- Collection constructors -------------------------------------------------

func TestE2EListNew(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums List<Int> = List()
    nums.add(1)
    nums.add(2)
    nums.add(3)
    print(nums.size())
}`)
	assertOutput(t, out, "3")
}

func TestE2EMapNew(t *testing.T) {
	out := e2eRun(t, `
main() {
    m Map<String, Int> = Map()
    m["a"] = 1
    m["b"] = 2
    print(m.size())
}`)
	assertOutput(t, out, "2")
}

// --- Enums + match -----------------------------------------------------------

func TestE2EEnumMatch(t *testing.T) {
	out := e2eRun(t, `
enum Direction { North, South, East, West }
describe(d Direction) String {
    match d {
        case Direction.North => { return "north" }
        case Direction.South => { return "south" }
        case _ => { return "other" }
    }
}
main() {
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
main() {
    with (f := os.Stdin) {
        print("resource open")
    }
    print("done")
}`)
	assertOutput(t, out, "resource open\ndone")
}

func TestE2EWithFileOpenClose(t *testing.T) {
	out := e2eRun(t, `
import "os"
main() {
    path := "/tmp/zinc_with_test.txt"
    with (f := os.Create(path)) {
        f.WriteString("hello from zinc")
    }
    content := readFile(path)
    print(content)
    os.Remove(path)
}`)
	assertOutput(t, out, "hello from zinc")
}

func TestE2ETupleDestructureWithErrorPropagation(t *testing.T) {
	// os.Pipe() returns (*File, *File, error) — Zinc strips the error automatically
	// The two names in the tuple capture the non-error return values
	src := `import "os"
main() {
    (r, w) := os.Pipe()
    w.WriteString("pipe works")
    w.Close()
    r.Close()
    print("ok")
}`
	out := e2eRun(t, src)
	assertOutput(t, out, "ok")
}

func TestE2EWithMutex(t *testing.T) {
	out := e2eRun(t, `
import "sync"
main() {
    mu := sync.Mutex.new()
    x := 0
    with (lock := mu) {
        x = x + 1
    }
    with (lock2 := mu) {
        x = x + 10
    }
    print(x)
}`)
	assertOutput(t, out, "11")
}

// --- Type casting (as / is) --------------------------------------------------

func TestE2EAsCast(t *testing.T) {
	out := e2eRun(t, `
main() {
    x Any = 42
    y := x as Int
    print(y + 1)
}`)
	assertOutput(t, out, "43")
}

func TestE2EIsCheck(t *testing.T) {
	out := e2eRun(t, `
main() {
    x Any = "hello"
    if x is String {
        print("yes")
    } else {
        print("no")
    }
}`)
	assertOutput(t, out, "yes")
}

func TestE2EIsCheckFalse(t *testing.T) {
	out := e2eRun(t, `
main() {
    x Any = 42
    if x is String {
        print("string")
    } else {
        print("not string")
    }
}`)
	assertOutput(t, out, "not string")
}

func TestE2EAsCastClassType(t *testing.T) {
	out := e2eRun(t, `
Animal {
    name String
    new(name String) {
        this.name = name
    }
    speak() String { return this.name }
}

Dog : Animal {
    new(name String) {
        super(name)
    }
    pub bark() String { return this.name + " says woof" }
}

main() {
    a Any = Dog("Rex")
    d := a as Dog
    print(d.bark())
}`)
	assertOutput(t, out, "Rex says woof")
}

// --- .new() on Go types ------------------------------------------------------

func TestE2EGoTypeNew(t *testing.T) {
	out := e2eRun(t, `
import "sync"
main() {
    mu := sync.Mutex.new()
    mu.Lock()
    mu.Unlock()
    print("ok")
}`)
	assertOutput(t, out, "ok")
}

func TestE2EWithMutexNew(t *testing.T) {
	out := e2eRun(t, `
import "sync"
main() {
    x := 0
    with (mu := sync.Mutex.new()) {
        x = x + 1
    }
    print(x)
}`)
	assertOutput(t, out, "1")
}

func TestE2EGoTypeNewBytesBuffer(t *testing.T) {
	out := e2eRun(t, `
import "bytes"
main() {
    buf := bytes.Buffer.new()
    buf.WriteString("hello")
    print(buf.String())
}`)
	assertOutput(t, out, "hello")
}

func TestE2EGoTypeNewWithNamedFields(t *testing.T) {
	out := e2eRun(t, `
import "bytes"
main() {
    buf := bytes.Buffer.new()
    buf.WriteString("hello")
    print(buf.String())
    print(buf.Len())
}`)
	assertOutput(t, out, "hello\n5")
}

func TestE2EGoTypeNewStructFields(t *testing.T) {
	// Use a Go struct where we can set fields via named construction
	out := e2eRun(t, `
import "net/url"
main() {
    u := url.URL.new(Scheme: "https", Host: "example.com", Path: "/api")
    print(u.String())
}`)
	assertOutput(t, out, "https://example.com/api")
}

// --- Labeled break/continue --------------------------------------------------

func TestE2ELabeledBreak(t *testing.T) {
	out := e2eRun(t, `
main() {
    result := ""
    @outer for (i := 0; i < 3; i += 1) {
        for (j := 0; j < 3; j += 1) {
            if j == 1 {
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
main() {
    result := ""
    @outer for (i := 0; i < 3; i += 1) {
        for (j := 0; j < 3; j += 1) {
            if j == 1 {
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
main() {
    i := 0
    result := ""
    @outer while i < 3 {
        j := 0
        while j < 3 {
            if j == 1 {
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
Dog {
    name String
    new(n String) {
        this.name = n
    }
}
main() {
    d Dog? = Dog("Rex")
    result := d?.name
    print(result)
}`)
	assertOutput(t, out, "Rex")
}

// --- Safe navigation ?.  --- field access on nil → returns nil
func TestE2ESafeNavNil(t *testing.T) {
	out := e2eRun(t, `
Dog {
    name String
    new(n String) {
        this.name = n
    }
}
main() {
    d Dog? = null
    result := d?.name
    if result == null {
        print("nil safe")
    }
}`)
	assertOutput(t, out, "nil safe")
}

// --- Safe navigation ?.  --- method call on non-nil pointer
func TestE2ESafeNavMethodCall(t *testing.T) {
	out := e2eRun(t, `
Dog {
    name String
    new(n String) {
        this.name = n
    }
    pub speak() String {
        return "woof"
    }
}
main() {
    d Dog? = Dog("Rex")
    result := d?.speak()
    print(result)
}`)
	assertOutput(t, out, "woof")
}

// --- Safe navigation ?.  --- method call on nil → returns nil, method not called
func TestE2ESafeNavMethodNil(t *testing.T) {
	out := e2eRun(t, `
Dog {
    name String
    new(n String) {
        this.name = n
    }
    pub speak() String {
        return "woof"
    }
}
main() {
    d Dog? = null
    result := d?.speak()
    if result == null {
        print("method not called")
    }
}`)
	assertOutput(t, out, "method not called")
}

// --- Safe navigation ?.  --- as statement (void method) on non-nil
func TestE2ESafeNavVoidMethodNonNil(t *testing.T) {
	out := e2eRun(t, `
Logger {
    lastMsg String
    new() {
        this.lastMsg = ""
    }
    pub log(msg String) {
        this.lastMsg = msg
        print(msg)
    }
}
main() {
    l Logger? = Logger()
    l?.log("hello")
}`)
	assertOutput(t, out, "hello")
}

// --- Safe navigation ?.  --- as statement (void method) on nil — should not crash
func TestE2ESafeNavVoidMethodNil(t *testing.T) {
	out := e2eRun(t, `
Logger {
    lastMsg String
    new() {
        this.lastMsg = ""
    }
    pub log(msg String) {
        this.lastMsg = msg
        print(msg)
    }
}
main() {
    l Logger? = null
    l?.log("should not print")
    print("survived")
}`)
	assertOutput(t, out, "survived")
}

// --- Safe navigation ?.  --- chaining a?.b?.c
func TestE2ESafeNavChaining(t *testing.T) {
	out := e2eRun(t, `
Address {
    city String
    new(c String) {
        this.city = c
    }
}
Person {
    name String
    address Address?
    new(n String, addr Address?) {
        this.name = n
        this.address = addr
    }
}
main() {
    p Person? = Person("Alice", Address("NYC"))
    city := p?.address?.city
    print(city)
}`)
	assertOutput(t, out, "NYC")
}

// --- Safe navigation ?.  --- chaining where middle is nil
func TestE2ESafeNavChainingNilMiddle(t *testing.T) {
	out := e2eRun(t, `
Address {
    city String
    new(c String) {
        this.city = c
    }
}
Person {
    name String
    address Address?
    new(n String, addr Address?) {
        this.name = n
        this.address = addr
    }
}
main() {
    p Person? = Person("Bob", null)
    city := p?.address?.city
    if city == null {
        print("no city")
    }
}`)
	assertOutput(t, out, "no city")
}

// --- with multi-return (auto-detect) -----------------------------------------

func TestE2EWithTryMultiReturn(t *testing.T) {
	out := e2eRun(t, `
import "os"
main() {
    with (f := os.CreateTemp("", "test*.txt")) {
        f.WriteString("hello")
        print("ok")
    }
}`)
	assertOutput(t, out, "ok")
}

// with: write and read back to verify file actually works
func TestE2EWithTryFileWriteRead(t *testing.T) {
	out := e2eRun(t, `
import "os"
main() {
    path := os.TempDir() + "/zinc_with_test.txt"
    with (f := os.Create(path)) {
        f.WriteString("hello from with")
    }
    content := readFile(path)
    print(content)
    os.Remove(path)
}`)
	assertOutput(t, out, "hello from with")
}

// with: error handled by or handler
func TestE2EWithOrHandler(t *testing.T) {
	out := e2eRun(t, `
import "os"
main() {
    with (f := os.Open("/nonexistent/path/that/does/not/exist") or {
        print("caught error")
        exit(0)
    }) {
        print("should not reach")
    }
}`)
	assertOutput(t, out, "caught error")
}

// with: multiple resources — file + mutex
func TestE2EWithMultipleResources(t *testing.T) {
	out := e2eRun(t, `
import "sync"
import "os"
main() {
    x := 0
    with (f := os.Stdin, mu := sync.Mutex.new()) {
        x = x + 1
        print("inside with")
    }
    print(x)
}`)
	assertOutput(t, out, "inside with\n1")
}

// with: multiple resources with auto-detected multi-return
func TestE2EWithMultipleTryResources(t *testing.T) {
	out := e2eRun(t, `
import "os"
main() {
    p1 := os.TempDir() + "/zinc_multi1.txt"
    p2 := os.TempDir() + "/zinc_multi2.txt"
    with (f1 := os.Create(p1), f2 := os.Create(p2)) {
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
main() {
    x := 0
    with (mu1 := sync.Mutex.new()) {
        x = x + 1
        with (mu2 := sync.Mutex.new()) {
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
main() {
    with (x := 42) {
        print(x)
    }
}`)
	assertOutput(t, out, "42")
}

// with: using readFile built-in inside with block
func TestE2EWithTryReadAfterWrite(t *testing.T) {
	out := e2eRun(t, `
import "os"
main() {
    path := os.TempDir() + "/zinc_with_rw.txt"
    with (f := os.Create(path)) {
        f.WriteString("zinc with rocks")
    }
    // File is now closed (defer Close() ran), safe to read
    print(readFile(path))
    os.Remove(path)
}`)
	assertOutput(t, out, "zinc with rocks")
}

// with: RWMutex (implements sync.Locker via RLock/Lock)
func TestE2EWithRWMutex(t *testing.T) {
	out := e2eRun(t, `
import "sync"
main() {
    mu := sync.RWMutex.new()
    x := 0
    with (lock := mu) {
        x = x + 5
    }
    print(x)
}`)
	assertOutput(t, out, "5")
}

// --- New stdlib built-in aliases ---------------------------------------------

func TestE2EJsonEncode(t *testing.T) {
	out := e2eRun(t, `
main() {
    data := "hello"
    encoded := jsonEncode(data)
    print(encoded)
}`)
	assertOutput(t, out, `"hello"`)
}

func TestE2ESprintf(t *testing.T) {
	out := e2eRun(t, `
main() {
    result := sprintf("Hello, %s! You are %d.", "Alice", 30)
    print(result)
}`)
	assertOutput(t, out, "Hello, Alice! You are 30.")
}

func TestE2ETypeOf(t *testing.T) {
	out := e2eRun(t, `
main() {
    x := 42
    print(typeOf(x))
}`)
	assertOutput(t, out, "int")
}

func TestE2ESleep(t *testing.T) {
	out := e2eRun(t, `
main() {
    sleep(1)
    print("done")
}`)
	assertOutput(t, out, "done")
}

func TestE2EReadWriteFile(t *testing.T) {
	out := e2eRun(t, `
import "os"
main() {
    dir := os.TempDir()
    path := dir + "/zinc_test_rw.txt"
    writeFile(path, "hello zinc")
    content := readFile(path)
    print(content)
    os.Remove(path)
}`)
	assertOutput(t, out, "hello zinc")
}

// --- OO collection methods ---------------------------------------------------

func TestE2EListAdd(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums List<Int> = List()
    nums.add(10)
    nums.add(20)
    nums.add(30)
    print(nums.size())
    for n in nums {
        print(n)
    }
}`)
	assertOutput(t, out, "3\n10\n20\n30")
}

func TestE2EMapRemove(t *testing.T) {
	out := e2eRun(t, `
main() {
    m Map<String, Int> = Map()
    m["a"] = 1
    m["b"] = 2
    m["c"] = 3
    m.remove("b")
    print(m.size())
}`)
	assertOutput(t, out, "2")
}

func TestE2EListClone(t *testing.T) {
	out := e2eRun(t, `
main() {
    a := [1, 2, 3]
    b := a.clone()
    b.add(4)
    print(a.size())
    print(b.size())
}`)
	assertOutput(t, out, "3\n4")
}

func TestE2ECollectionSize(t *testing.T) {
	out := e2eRun(t, `
main() {
    list := [1, 2, 3, 4, 5]
    print(list.size())
    m Map<String, Int> = Map()
    m["x"] = 1
    print(m.size())
    s := "hello"
    print(s.size())
}`)
	assertOutput(t, out, "5\n1\n5")
}

// --- OO string methods -------------------------------------------------------

func TestE2EStringUpper(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "hello"
    print(s.upper())
}`)
	assertOutput(t, out, "HELLO")
}

func TestE2EStringLower(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "HELLO"
    print(s.lower())
}`)
	assertOutput(t, out, "hello")
}

func TestE2EStringContains(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "hello world"
    if s.contains("world") {
        print("yes")
    }
    if !(s.contains("xyz")) {
        print("no")
    }
}`)
	assertOutput(t, out, "yes\nno")
}

func TestE2EStringStartsEndsWith(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "hello world"
    if s.startsWith("hello") { print("starts") }
    if s.endsWith("world") { print("ends") }
}`)
	assertOutput(t, out, "starts\nends")
}

func TestE2EStringTrim(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "  hello  "
    print(s.trim())
}`)
	assertOutput(t, out, "hello")
}

func TestE2EStringSplit(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "a,b,c"
    parts := s.split(",")
    for p in parts {
        print(p)
    }
}`)
	assertOutput(t, out, "a\nb\nc")
}

func TestE2EStringReplace(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "hello world world"
    print(s.replace("world", "zinc"))
}`)
	assertOutput(t, out, "hello zinc zinc")
}

func TestE2EListJoin(t *testing.T) {
	out := e2eRun(t, `
main() {
    parts := ["a", "b", "c"]
    print(parts.join(", "))
}`)
	assertOutput(t, out, "a, b, c")
}

func TestE2EListSort(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [3, 1, 4, 1, 5]
    nums.sort()
    for n in nums {
        print(n)
    }
}`)
	assertOutput(t, out, "1\n1\n3\n4\n5")
}

// --- For (k, v) in map -------------------------------------------------------

func TestE2EForKeyValueInMap(t *testing.T) {
	out := e2eRun(t, `
main() {
    m := {"a": 1}
    for k, v in m {
        print("{k}={v}")
    }
}`)
	assertOutput(t, out, "a=1")
}

// --- Map utility methods -----------------------------------------------------

func TestE2EMapKeys(t *testing.T) {
	out := e2eRun(t, `
main() {
    m := {"x": 1}
    ks := m.keys()
    print(ks.size())
}`)
	assertOutput(t, out, "1")
}

func TestE2EMapValues(t *testing.T) {
	out := e2eRun(t, `
main() {
    m := {"x": 42}
    vs := m.values()
    print(vs.size())
}`)
	assertOutput(t, out, "1")
}

func TestE2EMapContainsKey(t *testing.T) {
	out := e2eRun(t, `
main() {
    m := {"hello": 1, "world": 2}
    print(m.containsKey("hello"))
    print(m.containsKey("nope"))
}`)
	assertOutput(t, out, "true\nfalse")
}

// --- Callable function types (Fn<>) ------------------------------------------

func TestE2EFnTypeParam(t *testing.T) {
	out := e2eRun(t, `
apply(f Fn<(Int), Int>, x Int) Int {
    return f(x)
}

main() {
    double := (x Int) Int => x * 2
    print(apply(double, 7))
}`)
	assertOutput(t, out, "14")
}

func TestE2EFnTypeMultiParam(t *testing.T) {
	out := e2eRun(t, `
combine(f Fn<(Int, Int), Int>, a Int, b Int) Int {
    return f(a, b)
}

main() {
    add := (a Int, b Int) Int => a + b
    print(combine(add, 3, 4))
}`)
	assertOutput(t, out, "7")
}

func TestE2EFnTypeVoid(t *testing.T) {
	out := e2eRun(t, `
run(callback Fn<(), Void>) {
    callback()
}

main() {
    run(() Void => {
        print("called")
    })
}`)
	assertOutput(t, out, "called")
}

func TestE2EFnTypeVar(t *testing.T) {
	out := e2eRun(t, `
main() {
    transform := (s String) Int => s.size()
    print(transform("hello"))
}`)
	assertOutput(t, out, "5")
}

func TestE2EStringMethodChaining(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "  Hello World  "
    print(s.trim().lower())
}`)
	assertOutput(t, out, "hello world")
}

func TestE2EConstructorNewSyntax(t *testing.T) {
	out := e2eRun(t, `
Cat {
    name String

    new(name String) {
        this.name = name
    }

    pub greet() String {
        return "Meow, I'm {this.name}"
    }
}

main() {
    c := Cat("Whiskers")
    print(c.greet())
}`)
	assertOutput(t, out, "Meow, I'm Whiskers")
}

func e2eRunTyped(t *testing.T, src string) string {
	t.Helper()
	out, errs := transpileWithTypes(src)
	if errs != nil {
		t.Fatalf("transpile errors: %v", errs)
	}

	dir := t.TempDir()
	goMod := "module e2e\n\ngo 1.26\n"
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

func TestE2ETypedMapLiteral(t *testing.T) {
	out := e2eRunTyped(t, `
main() {
    m := {"a": 1, "b": 2}
    print(m["a"] + m["b"])
}`)
	assertOutput(t, out, "3")
}

func TestE2ETypedListLiteral(t *testing.T) {
	out := e2eRunTyped(t, `
main() {
    nums := [10, 20, 30]
    print(nums[0] + nums[2])
}`)
	assertOutput(t, out, "40")
}

func TestE2EEmptyMapWithType(t *testing.T) {
	out := e2eRunTyped(t, `
main() {
    m Map<String, Int> = {}
    m["x"] = 42
    print(m["x"])
}`)
	assertOutput(t, out, "42")
}

func TestE2ENestedList(t *testing.T) {
	out := e2eRunTyped(t, `
main() {
    grid := [[1, 2], [3, 4]]
    print(grid[0][0] + grid[1][1])
}`)
	assertOutput(t, out, "5")
}

func TestE2EConstDecl(t *testing.T) {
	out := e2eRun(t, `
const PI = 3.14
const GREETING String = "hello"

main() {
    print(PI)
    print(GREETING)
}`)
	assertOutput(t, out, "3.14\nhello")
}

func TestE2EConstInExpr(t *testing.T) {
	out := e2eRun(t, `
const TAX_RATE = 0.08
main() {
    price := 100.0
    total := price + price * TAX_RATE
    print(total)
}`)
	assertOutput(t, out, "108")
}

// --- Index access and assignment ---------------------------------------------

func TestE2EListIndexAccess(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [10, 20, 30]
    print(nums[0])
    print(nums[1])
    print(nums[2])
}`)
	assertOutput(t, out, "10\n20\n30")
}

func TestE2EListIndexAssignment(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [10, 20, 30]
    nums[1] = 99
    print(nums[0])
    print(nums[1])
    print(nums[2])
}`)
	assertOutput(t, out, "10\n99\n30")
}

func TestE2EMapIndexAccess(t *testing.T) {
	out := e2eRunTyped(t, `
main() {
    m := {"a": 1, "b": 2}
    print(m["a"])
    print(m["b"])
}`)
	assertOutput(t, out, "1\n2")
}

func TestE2EMapIndexAssignment(t *testing.T) {
	out := e2eRunTyped(t, `
main() {
    m := {"x": 10}
    m["x"] = 42
    m["y"] = 99
    print(m["x"])
    print(m["y"])
}`)
	assertOutput(t, out, "42\n99")
}

func TestE2EStringIndexAccess(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "hello"
    print(string(s[0]))
    print(string(s[4]))
}`)
	assertOutput(t, out, "h\no")
}

// --- Slicing e2e -------------------------------------------------------------

func TestE2EListSliceBracket(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5]
    a := nums[1:3]
    print(a.size())
    print(a[0])
    print(a[1])
}`)
	assertOutput(t, out, "2\n2\n3")
}

func TestE2EListSliceOpenEnd(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [10, 20, 30, 40]
    a := nums[2:]
    print(a.size())
    print(a[0])
}`)
	assertOutput(t, out, "2\n30")
}

func TestE2EListSliceOpenStart(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [10, 20, 30, 40]
    a := nums[:2]
    print(a.size())
    print(a[1])
}`)
	assertOutput(t, out, "2\n20")
}

func TestE2EStringSliceBracket(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "hello world"
    print(s[0:5])
    print(s[6:])
}`)
	assertOutput(t, out, "hello\nworld")
}

func TestE2ESliceMethod(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5]
    a := nums.slice(1, 4)
    print(a[0])
    print(a[2])
}`)
	assertOutput(t, out, "2\n4")
}

// --- Break and continue (non-labeled) ----------------------------------------

func TestE2EBreak(t *testing.T) {
	out := e2eRun(t, `
main() {
    i := 0
    while true {
        if i == 3 { break }
        i = i + 1
    }
    print(i)
}`)
	assertOutput(t, out, "3")
}

func TestE2EContinue(t *testing.T) {
	out := e2eRun(t, `
main() {
    sum := 0
    for (i := 0; i < 10; i += 1) {
        if i % 2 == 0 { continue }
        sum = sum + i
    }
    print(sum)
}`)
	assertOutput(t, out, "25")
}

// --- Generics e2e ------------------------------------------------------------

func TestE2EGenericFunction(t *testing.T) {
	out := e2eRun(t, `
identity<T>(val T) T {
    return val
}
main() {
    print(identity(42))
    print(identity("hello"))
}`)
	assertOutput(t, out, "42\nhello")
}

func TestE2EGenericClass(t *testing.T) {
	out := e2eRun(t, `
Box<T> {
    value T
    new(v T) { this.value = v }
    pub get() T { return this.value }
}
main() {
    intBox := Box(42)
    strBox := Box("hello")
    print(intBox.get())
    print(strBox.get())
}`)
	assertOutput(t, out, "42\nhello")
}

// --- Method chaining e2e -----------------------------------------------------

func TestE2EMethodChaining(t *testing.T) {
	out := e2eRun(t, `
main() {
    s := "  HELLO WORLD  "
    print(s.trim().lower())
    print("a,b,c".split(",").join(" "))
}`)
	assertOutput(t, out, "hello world\na b c")
}

// --- Variadic Functions ------------------------------------------------------

func TestE2EVariadicSum(t *testing.T) {
	out := e2eRun(t, `
sum(nums ...Int) Int {
    total := 0
    for n in nums {
        total += n
    }
    return total
}
main() {
    print(sum(1, 2, 3))
    print(sum(10, 20))
    print(sum())
}`)
	assertOutput(t, out, "6\n30\n0")
}

func TestE2EVariadicSpread(t *testing.T) {
	out := e2eRun(t, `
sum(nums ...Int) Int {
    total := 0
    for n in nums {
        total += n
    }
    return total
}
main() {
    items := [1, 2, 3, 4]
    print(sum(items...))
}`)
	assertOutput(t, out, "10")
}

func TestE2EListAddMultiple(t *testing.T) {
	out := e2eRun(t, `
main() {
    items := [1, 2]
    items.add(3, 4, 5)
    print(items)
}`)
	assertOutput(t, out, "[1 2 3 4 5]")
}

func TestE2EListAddSpread(t *testing.T) {
	out := e2eRun(t, `
main() {
    items := [1, 2]
    more := [3, 4, 5]
    items.add(more...)
    print(items)
}`)
	assertOutput(t, out, "[1 2 3 4 5]")
}

func TestE2EVariadicWithFixedParams(t *testing.T) {
	out := e2eRun(t, `
log(level String, messages ...String) {
    for msg in messages {
        print("[{level}] {msg}")
    }
}
main() {
    log("INFO", "server started", "listening on :8080")
}`)
	assertOutput(t, out, "[INFO] server started\n[INFO] listening on :8080")
}

func TestE2EVariadicMethodCall(t *testing.T) {
	out := e2eRun(t, `
Logger {
    prefix String

    new(prefix String) {
        this.prefix = prefix
    }

    pub log(messages ...String) {
        for msg in messages {
            print("{this.prefix}: {msg}")
        }
    }
}
main() {
    l := Logger("APP")
    l.log("started", "ready")
}`)
	assertOutput(t, out, "APP: started\nAPP: ready")
}

func TestE2EFmtSprintf(t *testing.T) {
	out := e2eRun(t, `
import "fmt"
main() {
    msg := fmt.Sprintf("Hello %s, age %d", "Alice", 30)
    print(msg)
}`)
	assertOutput(t, out, "Hello Alice, age 30")
}

// --- Go Interop Auto-Detection E2E ------------------------------------------

func TestE2EAutoDetectStrconvAtoi(t *testing.T) {
	out := e2eRun(t, `
import "strconv"

main() {
    n := strconv.Atoi("42") or { print("error"); exit(1) }
    print(n)
}`)
	assertOutput(t, out, "42")
}

func TestE2EAutoDetectStrconvAtoiFail(t *testing.T) {
	out := e2eRun(t, `
import "strconv"

main() {
    n := strconv.Atoi("notanumber") or { print("caught"); exit(0) }
    print(n)
}`)
	assertOutput(t, out, "caught")
}

func TestE2EAutoDetectStrconvParseFloat(t *testing.T) {
	out := e2eRun(t, `
import "strconv"

main() {
    f := strconv.ParseFloat("3.14", 64) or { print("error"); exit(1) }
    print(f)
}`)
	assertOutput(t, out, "3.14")
}

func TestE2EAutoDetectInFailable(t *testing.T) {
	out := e2eRun(t, `
import "strconv"

parseNum(s String) Int {
    if s == "" { return Error("empty") }
    n := strconv.Atoi(s)
    return n
}

main() {
    x := parseNum("99") or { print("error"); exit(1) }
    print(x)
}`)
	assertOutput(t, out, "99")
}

// --- Phase 1: DeferStmt, RawStringLit ----------------------------------------

func TestE2EDefer(t *testing.T) {
	out := e2eRun(t, `
import "fmt"
main() {
    defer fmt.Println("last")
    print("first")
}`)
	assertOutput(t, out, "first\nlast")
}

func TestE2ERawString(t *testing.T) {
	src := "main() { s := `hello\\nworld`; print(s) }"
	out := e2eRun(t, src)
	assertOutput(t, out, "hello\\nworld")
}

func TestE2EMatchFailable(t *testing.T) {
	out := e2eRun(t, `
check(x Int) String {
    match x {
        case 0 => { return Error("zero not allowed") }
        case _ => { return "ok" }
    }
    return "unreachable"
}

main() {
    r := check(0) or { print("caught: {err}"); exit(0) }
    print(r)
}`)
	assertOutput(t, out, "caught: zero not allowed")
}

func TestE2EMethodFailable(t *testing.T) {
	out := e2eRun(t, `
import "os"

main() {
    f := os.Create("/tmp/zinc_test_method_failable.txt") or {
        print("create failed")
        exit(1)
    }
    f.WriteString("hello from zinc") or {
        print("write failed: {err}")
        exit(1)
    }
    f.Close() or {
        print("close failed")
        exit(1)
    }
    content := readFile("/tmp/zinc_test_method_failable.txt") or {
        print("read failed")
        exit(1)
    }
    print(content)
    os.Remove("/tmp/zinc_test_method_failable.txt")
}`)
	assertOutput(t, out, "hello from zinc")
}

func TestE2EClassWithAddMethod(t *testing.T) {
	out := e2eRun(t, `
Counter {
    count Int

    new() {}

    pub add(n Int) {
        this.count = this.count + n
    }

    pub getCount() Int {
        return this.count
    }
}

main() {
    c := Counter()
    c.add(5)
    c.add(3)
    print(c.getCount())
}`)
	assertOutput(t, out, "8")
}

func TestE2EWithMethodFailable(t *testing.T) {
	// with statement: method calls on resources should detect failable (multi-return)
	out := e2eRun(t, `
import "os"

main() {
    path := os.TempDir() + "/zinc_with_method_test.txt"
    with (f := os.Create(path)) {
        f.WriteString("with method failable") or {
            print("write failed")
            exit(1)
        }
    }
    content := readFile(path) or {
        print("read failed")
        exit(1)
    }
    print(content)
    os.Remove(path)
}`)
	assertOutput(t, out, "with method failable")
}

func TestE2EWithVoidMethodFailable(t *testing.T) {
	// with statement: void failable method (e.g. f.Sync() returns only error)
	out := e2eRun(t, `
import "os"

main() {
    path := os.TempDir() + "/zinc_with_sync_test.txt"
    with (f := os.Create(path)) {
        f.WriteString("sync test")
        f.Sync() or {
            print("sync failed")
            exit(1)
        }
    }
    content := readFile(path) or {
        print("read failed")
        exit(1)
    }
    print(content)
    os.Remove(path)
}`)
	assertOutput(t, out, "sync test")
}

func TestE2EWithMultipleResourcesMethodCalls(t *testing.T) {
	// Multiple resources with method calls on each
	out := e2eRun(t, `
import "os"

main() {
    p1 := os.TempDir() + "/zinc_multi_method_a.txt"
    p2 := os.TempDir() + "/zinc_multi_method_b.txt"
    with (f1 := os.Create(p1), f2 := os.Create(p2)) {
        f1.WriteString("AAA") or { print("f1 write failed"); exit(1) }
        f2.WriteString("BBB") or { print("f2 write failed"); exit(1) }
    }
    print(readFile(p1))
    print(readFile(p2))
    os.Remove(p1)
    os.Remove(p2)
}`)
	assertOutput(t, out, "AAA\nBBB")
}

func TestE2EOsRemoveVoidFailable(t *testing.T) {
	out := e2eRun(t, `
import "os"

main() {
    os.Remove("/nonexistent/path/should/fail") or {
        print("caught")
        exit(0)
    }
    print("should not reach")
}`)
	assertOutput(t, out, "caught")
}

func TestE2EPolymorphism(t *testing.T) {
	out := e2eRun(t, `
interface Speaker {
    pub speak() String
}

Animal {
    name String
    new(n String) {
        this.name = n
    }
    pub speak() String {
        return "{this.name} says ..."
    }
}

Dog : Animal, Speaker {
    new(n String) {
        super(n)
    }
    pub speak() String {
        return "{this.name} says Woof!"
    }
}

printSpeak(s Speaker) {
    print(s.speak())
}

main() {
    d := Dog("Rex")
    printSpeak(d)
    print(d.speak())
}`)
	assertOutput(t, out, "Rex says Woof!\nRex says Woof!")
}

func TestE2EPolymorphismFieldAccess(t *testing.T) {
	out := e2eRun(t, `
Person {
    name String
    age Int
    new(n String, a Int) {
        this.name = n
        this.age = a
    }
}

greet(p Person) {
    print("Hello, {p.name}, age {p.age}")
}

main() {
    p := Person("Alice", 30)
    greet(p)
    print(p.name)
}`)
	assertOutput(t, out, "Hello, Alice, age 30\nAlice")
}

// --- Failable methods through interface-typed parameters ---------------------

func TestE2EFailableMethodViaInterface(t *testing.T) {
	out := e2eRun(t, `
AgeValidator {
    age Int
    new(a Int) { this.age = a }
    pub validate() String {
        if this.age < 0 {
            return Error("age cannot be negative")
        }
        return "valid"
    }
}

checkAge(v AgeValidator) {
    result := v.validate() or {
        print("error: {err}")
        return
    }
    print(result)
}

main() {
    checkAge(AgeValidator(25))
    checkAge(AgeValidator(-1))
}`)
	assertOutput(t, out, "valid\nerror: age cannot be negative")
}

func TestE2EVoidFailableMethodViaInterface(t *testing.T) {
	out := e2eRun(t, `
import "os"

Writer {
    prefix String
    new(p String) { this.prefix = p }
    pub process(path String) {
        with (f := os.Create(path)) {
            f.WriteString("{this.prefix}: data") or {}
        }
    }
}

runWriter(w Writer, path String) {
    w.process(path)
}

main() {
    path := "/tmp/zinc_void_failable_e2e.txt"
    runWriter(Writer("LOG"), path)
    content := readFile(path) or {
        print("read error")
        exit(1)
    }
    print(content)
    os.Remove(path) or {}
}`)
	assertOutput(t, out, "LOG: data")
}

func TestE2EErrorPropagationChain(t *testing.T) {
	out := e2eRun(t, `
risky(x Int) Int {
    if x < 0 { return Error("negative") }
    return x * 2
}

middle(x Int) Int {
    r := risky(x)
    return r + 1
}

main() {
    a := middle(5) or {
        print("err")
        exit(1)
    }
    print(a)

    b := middle(-1) or {
        print("caught: {err}")
        exit(0)
    }
    print(b)
}`)
	assertOutput(t, out, "11\ncaught: negative")
}

func TestE2EMultipleOrHandlers(t *testing.T) {
	out := e2eRun(t, `
risky(x Int) Int {
    if x == 0 { return Error("zero") }
    return 100 / x
}

main() {
    a := risky(5) or { print("err"); exit(1) }
    print(a)
    b := risky(0) or {
        print("caught: {err}")
        exit(0)
    }
    print(b)
}`)
	assertOutput(t, out, "20\ncaught: zero")
}

func TestE2EPolymorphismMultipleShapes(t *testing.T) {
	out := e2eRun(t, `
interface Shape {
    pub area() Float
    pub name() String
}

Circle : Shape {
    radius Float
    new(r Float) { this.radius = r }
    pub area() Float { return 3.14 * this.radius * this.radius }
    pub name() String { return "Circle" }
}

Square : Shape {
    side Float
    new(s Float) { this.side = s }
    pub area() Float { return this.side * this.side }
    pub name() String { return "Square" }
}

describe(s Shape) {
    print("{s.name()}: {s.area()}")
}

main() {
    describe(Circle(1.0))
    describe(Square(2.0))
}`)
	assertOutput(t, out, "Circle: 3.14\nSquare: 4")
}

func TestE2EMultipleDefers(t *testing.T) {
	out := e2eRun(t, `
import "fmt"
main() {
    defer fmt.Println("first")
    defer fmt.Println("second")
    defer fmt.Println("third")
    print("body")
}`)
	// Go defers execute in LIFO order
	assertOutput(t, out, "body\nthird\nsecond\nfirst")
}

func TestE2EGetterCollisionWithExplicitMethod(t *testing.T) {
	out := e2eRun(t, `
Config {
    value Int
    new(v Int) { this.value = v }
    pub getValue() Int { return this.value * 2 }
}

main() {
    c := Config(21)
    print(c.getValue())
}`)
	assertOutput(t, out, "42")
}

func TestE2EWithNestedResources(t *testing.T) {
	out := e2eRun(t, `
import "os"

main() {
    path := "/tmp/zinc_nested_with_e2e.txt"
    with (f := os.Create(path)) {
        f.WriteString("hello") or {}
        with (f2 := os.Open(path)) {
            print("nested ok")
        }
    }
    os.Remove(path) or {}
    print("done")
}`)
	assertOutput(t, out, "nested ok\ndone")
}

func TestE2EGoInteropStrconvChain(t *testing.T) {
	out := e2eRun(t, `
import "strconv"

main() {
    n := strconv.Atoi("42") or {
        print("parse error")
        exit(1)
    }
    print(n * 2)
    f := strconv.ParseFloat("3.14", 64) or {
        print("parse error")
        exit(1)
    }
    print(f)
}`)
	assertOutput(t, out, "84\n3.14")
}

func TestE2EGenericClassThroughInterface(t *testing.T) {
	out := e2eRun(t, `
Box<T> {
    value T

    new(value T) {
        this.value = value
    }

    pub getValue() T {
        return this.value
    }
}

printBox(b Box<Int>) {
    print(b.getValue())
}

main() {
    b := Box(42)
    printBox(b)
    print(b.getValue())
}`)
	assertOutput(t, out, "42\n42")
}

func TestE2EGenericClassFieldAccessThroughInterface(t *testing.T) {
	out := e2eRun(t, `
Pair<K, V> {
    key K
    val V

    new(key K, val V) {
        this.key = key
        this.val = val
    }
}

printPairKey(p Pair<String, Int>) {
    print(p.key)
}

main() {
    p := Pair("hello", 42)
    printPairKey(p)
    print(p.key)
}`)
	assertOutput(t, out, "hello\nhello")
}

func TestE2EGenericEmptyListLiteralInference(t *testing.T) {
	out := e2eRun(t, `
Stack<T> {
    items List<T>

    new(initial T) {
        this.items = []
        this.items.add(initial)
    }

    pub push(item T) {
        this.items.add(item)
    }

    pub count() Int {
        return this.items.size()
    }
}

main() {
    s := Stack(1)
    s.push(2)
    s.push(3)
    print(s.count())
}`)
	assertOutput(t, out, "3")
}

// --- Collection Methods E2E ---

func TestE2EWhereSelect(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Where(x => x > 5).Select(x => x * 2)
    print(result)
}`)
	assertOutput(t, out, "[12 14 16 18 20]")
}

func TestE2EWhereAggregate(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5]
    sum := nums.Where(x => x % 2 == 0).Aggregate(0, (acc, x) => acc + x)
    print(sum)
}`)
	assertOutput(t, out, "6")
}

func TestE2EAny(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 3, 5, 7, 8, 9]
    hasEven := nums.Any(x => x % 2 == 0)
    print(hasEven)
}`)
	assertOutput(t, out, "true")
}

func TestE2EAll(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [2, 4, 6, 8]
    allEven := nums.All(x => x % 2 == 0)
    print(allEven)
}`)
	assertOutput(t, out, "true")
}

func TestE2ETake(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [10, 20, 30, 40, 50]
    first3 := nums.Take(3)
    print(first3)
}`)
	assertOutput(t, out, "[10 20 30]")
}

func TestE2ECount(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5, 6]
    evenCount := nums.Where(x => x % 2 == 0).Count()
    print(evenCount)
}`)
	assertOutput(t, out, "3")
}

func TestE2EWhereTake(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Where(x => x % 2 == 0).Take(3)
    print(result)
}`)
	assertOutput(t, out, "[2 4 6]")
}

func TestE2ELambdaShorthand(t *testing.T) {
	out := e2eRun(t, `
main() {
    double := (x Int) Int => x * 2
    print(double(5))
}`)
	assertOutput(t, out, "10")
}

func TestE2ESelectAlone(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3]
    doubled := nums.Select(x => x * 2)
    for n in doubled { print(n) }
}`)
	assertOutput(t, out, "2\n4\n6")
}

func TestE2ESkip(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5]
    rest := nums.Skip(2)
    for n in rest { print(n) }
}`)
	assertOutput(t, out, "3\n4\n5")
}

func TestE2EFirst(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5]
    first := nums.First(x => x > 3)
    print(first)
}`)
	assertOutput(t, out, "4")
}

func TestE2EFirstOrDefault(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3]
    first := nums.FirstOrDefault(x => x > 10)
    print(first)
}`)
	assertOutput(t, out, "<nil>")
}

func TestE2EWhereSelectAggregate(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5]
    sum := nums.Where(x => x > 2).Select(x => x * 10).Aggregate(0, (acc, x) => acc + x)
    print(sum)
}`)
	assertOutput(t, out, "120")
}

func TestE2EWhereSelectTake(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]
    result := nums.Where(x => x > 3).Select(x => x * 2).Take(3)
    for n in result { print(n) }
}`)
	assertOutput(t, out, "8\n10\n12")
}

func TestE2ESelectAny(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3]
    hasNeg := nums.Select(x => x - 2).Any(x => x < 0)
    print(hasNeg)
}`)
	assertOutput(t, out, "true")
}

func TestE2EEmptyList(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3]
    result := nums.Where(x => x > 10)
    print(result)
}`)
	assertOutput(t, out, "[]")
}

func TestE2EAllFalse(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5]
    allBig := nums.All(x => x > 3)
    print(allBig)
}`)
	assertOutput(t, out, "false")
}

func TestE2EForEachStmt(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [10, 20, 30]
    doubled := nums.Select(x => x * 2)
    for n in doubled { print(n) }
}`)
	assertOutput(t, out, "20\n40\n60")
}

func TestE2EWhereCount(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [1, 2, 3, 4, 5]
    c := nums.Where(x => x > 2).Count()
    print(c)
}`)
	assertOutput(t, out, "3")
}

func TestE2ESingleElement(t *testing.T) {
	out := e2eRun(t, `
main() {
    nums := [42]
    result := nums.Where(x => x > 10)
    for n in result { print(n) }
}`)
	assertOutput(t, out, "42")
}

// --- Failable lambdas in collection chains ---

func TestE2ESelectWithFailableFunction(t *testing.T) {
	out := e2eRun(t, `
safeDivide(x Int) Int {
    if x == 0 { return Error("zero") }
    return 100 / x
}
main() {
    nums := [2, 5, 10]
    result := nums.Select(x => safeDivide(x))
    for n in result { print(n) }
}`)
	assertOutput(t, out, "50\n20\n10")
}

func TestE2EWhereWithFailableFunction(t *testing.T) {
	out := e2eRun(t, `
isValid(x Int) Bool {
    if x < 0 { return Error("negative") }
    return x > 3
}
main() {
    nums := [1, 4, 5, 2]
    result := nums.Where(x => isValid(x))
    for n in result { print(n) }
}`)
	assertOutput(t, out, "4\n5")
}

func TestE2EAggregateWithFailableFunction(t *testing.T) {
	out := e2eRun(t, `
safeAdd(a Int, b Int) Int {
    if a + b > 1000 { return Error("overflow") }
    return a + b
}
main() {
    nums := [10, 20, 30]
    sum := nums.Aggregate(0, (acc, x) => safeAdd(acc, x))
    print(sum)
}`)
	assertOutput(t, out, "60")
}

func TestE2ESelectFailableErrorPropagation(t *testing.T) {
	// When a failable lambda errors, it should panic in main
	src := `
safeDivide(x Int) Int {
    if x == 0 { return Error("division by zero") }
    return 100 / x
}
main() {
    nums := [2, 0, 5]
    result := nums.Select(x => safeDivide(x))
    print(result)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatalf("transpile errors: %v", errs)
	}
	// The generated code should compile and panic at runtime
	assertContains(t, out, "_err")
	assertContains(t, out, "!= nil")
}

func TestE2EAnyWithFailableFunction(t *testing.T) {
	src := `
isPositive(x Int) Bool {
    if x == 0 { return Error("zero not allowed") }
    return x > 0
}
main() {
    nums := [1, 2, 3]
    hasPositive := nums.Any(x => isPositive(x))
    print(hasPositive)
}`
	got := e2eRun(t, src)
	assertOutput(t, got, "true")
}

func TestE2EAllWithFailableFunction(t *testing.T) {
	src := `
isPositive(x Int) Bool {
    if x == 0 { return Error("zero not allowed") }
    return x > 0
}
main() {
    nums := [1, 2, 3]
    allPositive := nums.All(x => isPositive(x))
    print(allPositive)
}`
	got := e2eRun(t, src)
	assertOutput(t, got, "true")
}

func TestE2EFirstWithFailableFunction(t *testing.T) {
	src := `
isEven(x Int) Bool {
    if x < 0 { return Error("negative") }
    return x % 2 == 0
}
main() {
    nums := [1, 3, 4, 6]
    first := nums.First(x => isEven(x))
    print(first)
}`
	got := e2eRun(t, src)
	assertOutput(t, got, "4")
}

func TestE2EWhereSelectWithFailable(t *testing.T) {
	src := `
double(x Int) Int {
    if x > 100 { return Error("too large") }
    return x * 2
}
main() {
    nums := [1, 2, 3, 4, 5]
    result := nums.Where(x => x > 2).Select(x => double(x))
    for n in result { print(n) }
}`
	got := e2eRun(t, src)
	assertOutput(t, got, "6\n8\n10")
}

func TestE2EFailableErrorPanicsInMain(t *testing.T) {
	src := `
safeDivide(x Int) Int {
    if x == 0 { return Error("division by zero") }
    return 100 / x
}
main() {
    nums := [2, 0, 5]
    result := nums.Select(x => safeDivide(x))
    print(result)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatalf("transpile errors: %v", errs)
	}

	dir := t.TempDir()
	goMod := "module e2e\n\ngo 1.26\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(out), 0644)

	cmd := exec.Command("go", "run", "main.go")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected runtime panic but command succeeded")
	}
	if !strings.Contains(string(output), "division by zero") {
		t.Errorf("expected 'division by zero' in output, got:\n%s", string(output))
	}
}

func TestE2ECountWithFailableWhere(t *testing.T) {
	src := `
isValid(x Int) Bool {
    if x < 0 { return Error("negative") }
    return x > 3
}
main() {
    nums := [1, 4, 5, 2, 6]
    c := nums.Where(x => isValid(x)).Count()
    print(c)
}`
	got := e2eRun(t, src)
	assertOutput(t, got, "3")
}
