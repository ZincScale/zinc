package codegen

import (
	"strings"
	"testing"
)

func TestIntegrationGenericClassImplementsInterface(t *testing.T) {
	src := `
interface Showable {
    fn show(): String
}
class Container<T> : Showable {
    var item: T
    construct new(v: T) {
        this.item = v
    }
    fn show(): String {
        return "Container"
    }
    fn get(): T {
        return this.item
    }
}
fn main() {
    var c = Container.new(42)
    print(c.show())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Container[T any] struct")
	assertContains(t, out, "type Showable interface")
}

func TestIntegrationEnumFieldInClassWithMatch(t *testing.T) {
	src := `
enum Status { Active, Idle, Done }
class Task {
    var status: Status
    construct new(s: Status) {
        this.status = s
    }
    fn describe(): String {
        match this.status {
            case Status.Active => { return "active" }
            case Status.Idle   => { return "idle" }
            case _ => { return "done" }
        }
    }
}
fn main() {
    var t = Task.new(StatusActive)
    print(t.describe())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Status int")
	assertContains(t, out, "StatusActive Status = iota")
	assertContains(t, out, "type Task struct")
	assertContains(t, out, "case StatusActive:")
	assertContains(t, out, "case StatusIdle:")
}

func TestIntegrationNestedTryCatch(t *testing.T) {
	src := `
fn riskyOp(x: Int): Int {
    if (x < 0) {
        throw Error("negative")
    }
    return x * 2
}
fn safeDouble(x: Int): Int {
    try {
        var r = riskyOp(x)
        return r
    } catch(err) {
        try {
            var fallback = riskyOp(0)
            return fallback
        } catch(innerErr) {
            return -1
        }
    }
}
fn main() {
    print(safeDouble(5))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// Each try block generates an error check; there should be multiple nil checks
	count := strings.Count(out, "!= nil")
	if count < 2 {
		t.Errorf("expected at least 2 occurrences of '!= nil', got %d\ngot:\n%s", count, out)
	}
	assertContains(t, out, "if innerErr != nil")
	assertContains(t, out, "return (-1)")
}

func TestIntegrationMultiLevelInheritance(t *testing.T) {
	src := `
class Animal {
    var name: String
    construct new(n: String) {
        this.name = n
    }
    pub fn speak(): String {
        return "..."
    }
}
class Dog : Animal {
    construct new(n: String) {
        super(n)
    }
    pub fn speak(): String {
        return "Woof!"
    }
}
class GoldenRetriever : Dog {
    construct new(n: String) {
        super(n)
    }
    pub fn fetch(): String {
        return "Fetch!"
    }
}
fn main() {
    var g = GoldenRetriever.new("Buddy")
    print(g.speak())
    print(g.fetch())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Animal struct")
	assertContains(t, out, "type Dog struct")
	assertContains(t, out, "type GoldenRetriever struct")
	assertContains(t, out, "func NewGoldenRetriever")
	// Receiver name is first letter of type name
	assertContains(t, out, "func (g *GoldenRetriever) Fetch()")
}

func TestIntegrationStringInterpolationInMethod(t *testing.T) {
	src := `
class Person {
    var name: String
    var age: Int
    construct new(n: String, a: Int) {
        this.name = n
        this.age = a
    }
    fn greeting(): String {
        return "Hello, I am {this.name} and I am {this.age} years old!"
    }
}
fn main() {
    var p = Person.new("Alice", 30)
    print(p.greeting())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "fmt.Sprintf")
	// Receiver name is first letter of class name; fields are capitalized
	assertContains(t, out, "p.Name")
	assertContains(t, out, "p.Age")
	assertContains(t, out, "Hello, I am %v and I am %v years old!")
}

func TestIntegrationOptionalFieldInGenericClass(t *testing.T) {
	src := `
class Wrapper<T> {
    var content: T?
    construct new() {
        this.content = null
    }
    fn set(v: T) {
        this.content = v
    }
    fn hasContent(): Bool {
        return this.content != null
    }
}
fn main() {
    var w = Wrapper.new()
    print(w.hasContent())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Wrapper[T any] struct")
	assertContains(t, out, "Content *T")
	// Constructor uses obj; methods use receiver initial (w for Wrapper)
	assertContains(t, out, "obj.Content = nil")
	assertContains(t, out, "w.Content != nil")
}

func TestIntegrationForInWithBuiltins(t *testing.T) {
	src := `
fn main() {
    var words = ["hello", "world", "growler"]
    for w in words {
        print(strUpper(w))
    }
    print(len(words))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "for _, w := range words")
	assertContains(t, out, "strings.ToUpper(w)")
	assertContains(t, out, "len(words)")
}

func TestIntegrationGoroutineChannel(t *testing.T) {
	// Growler channel syntax: go { ... }, ch.send(val), ch.receive()
	src := `
fn main() {
    var ch: Chan<Int> = Chan.new(1)
    go {
        ch.send(42)
    }
    var result = ch.receive()
    print(result)
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "make(chan int, 1)")
	assertContains(t, out, "ch <-")
	assertContains(t, out, "<-ch")
}

func TestIntegrationClassExtendsClassAndInterface(t *testing.T) {
	src := `
interface Speaker {
    pub fn speak(): String
}
class Animal {
    var name: String
    construct new(n: String) {
        this.name = n
    }
    pub fn getName(): String {
        return this.name
    }
}
class Dog : Animal, Speaker {
    var breed: String
    construct new(n: String, b: String) {
        super(n)
        this.breed = b
    }
    pub fn speak(): String {
        return "Woof!"
    }
}
fn main() {
    var d = Dog.new("Rex", "Lab")
    print(d.speak())
    print(d.getName())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Dog struct")
	// Receiver name is first letter of class name (d for Dog)
	assertContains(t, out, "func (d *Dog) Speak() string")
	assertContains(t, out, "var _ Speaker = (*Dog)(nil)")
}

func TestIntegrationWithAndTryCatch(t *testing.T) {
	src := `
fn main() {
    try {
        with var f = openFile("x") {
            print("ok")
        }
    } catch(err) {
        print("error")
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "f := openFile(\"x\")")
	assertContains(t, out, "if _c, ok := any(f).(io.Closer); ok { defer _c.Close() }")
	assertContains(t, out, "func() error")
	assertContains(t, out, "fmt.Println(\"error\")")
}

func TestIntegrationWithInClassMethod(t *testing.T) {
	src := `
class DataProcessor {
    construct new() {}
    pub fn process() {
        with var handle = openFile("data.txt") {
            print("processing")
        }
    }
}
fn main() {
    var p = DataProcessor.new()
    p.process()
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func (d *DataProcessor) Process()")
	assertContains(t, out, "handle := openFile(\"data.txt\")")
	assertContains(t, out, "if _c, ok := any(handle).(io.Closer); ok { defer _c.Close() }")
}

// --- sync.Locker via with ----------------------------------------------------

func TestIntegrationWithMutexInGoroutine(t *testing.T) {
	src := `
import "sync"
fn main() {
    var mu = sync.Mutex.new()
    go {
        with var lock = mu {
            print("critical section")
        }
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "go func()")
	assertContains(t, out, "lock := mu")
	assertContains(t, out, "if _l, ok := any(&lock).(sync.Locker); ok { _l.Lock(); defer _l.Unlock() } else if _l, ok := any(lock).(sync.Locker); ok { _l.Lock(); defer _l.Unlock() }")
	assertContains(t, out, `"sync"`)
}

// --- goroutine combinations --------------------------------------------------

func TestIntegrationGoRoutineTryCatch(t *testing.T) {
	src := `
fn main() {
    go {
        try {
            throw Error("oops")
        } catch(err) {
            print("caught")
        }
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "go func()")
	assertContains(t, out, "func() error")
	assertContains(t, out, "fmt.Println(\"caught\")")
}

func TestIntegrationGoRoutineWith(t *testing.T) {
	src := `
fn main() {
    go {
        with var f = openFile("x") {
            print("reading")
        }
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "go func()")
	assertContains(t, out, "f := openFile(\"x\")")
	assertContains(t, out, "if _c, ok := any(f).(io.Closer); ok { defer _c.Close() }")
}

func TestIntegrationGoRoutineClosure(t *testing.T) {
	src := `
fn main() {
    var base = 10
    go {
        var addBase = (x: Int): Int => x + base
        print(addBase(5))
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "go func()")
	assertContains(t, out, "addBase := func(x int) int")
	assertContains(t, out, "(x + base)")
}

func TestIntegrationGoRoutineThrowPanics(t *testing.T) {
	// throw directly inside a goroutine (no try/catch) should panic, not return
	src := `
fn main() {
    go {
        throw Error("fatal")
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "go func()")
	assertContains(t, out, "panic(fmt.Errorf(\"fatal\"))")
	assertNotContains(t, out, "return fmt.Errorf")
}

func TestIntegrationGoRoutineThrowInsideThrowingFn(t *testing.T) {
	// throw inside a goroutine that's inside a throwing function — goroutine
	// still gets panic, not return, because it has its own void scope.
	src := `
fn risky() {
    go {
        throw Error("goroutine error")
    }
    throw Error("fn error")
}
fn main() {
    try {
        risky()
    } catch(err) {
        print(err)
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "go func()")
	assertContains(t, out, "panic(fmt.Errorf(\"goroutine error\"))")
	// The fn-level throw is still a return
	assertContains(t, out, "return fmt.Errorf(\"fn error\")")
}
