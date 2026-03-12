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
	"testing"
)

func TestIntegrationGenericClassImplementsInterface(t *testing.T) {
	src := `
interface Showable {
    show() String
}
Container<T> : Showable {
    item T
    new(v T) {
        this.item = v
    }
    show() String {
        return "Container"
    }
    get() T {
        return this.item
    }
}
main() {
    c := Container(42)
    print(c.show())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type ContainerImpl[T any] struct")
	assertContains(t, out, "type Showable interface")
}

func TestIntegrationEnumFieldInClassWithMatch(t *testing.T) {
	src := `
enum Status { Active, Idle, Done }
Task {
    status Status
    new(s Status) {
        this.status = s
    }
    describe() String {
        match this.status {
            case Status.Active => { return "active" }
            case Status.Idle   => { return "idle" }
            case _ => { return "done" }
        }
    }
}
main() {
    t := Task(StatusActive)
    print(t.describe())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type Status int")
	assertContains(t, out, "StatusActive Status = iota")
	assertContains(t, out, "type TaskImpl struct")
	assertContains(t, out, "case StatusActive:")
	assertContains(t, out, "case StatusIdle:")
}

func TestIntegrationAutoErrorPropagation(t *testing.T) {
	src := `
riskyOp(x Int) Int {
    if x < 0 {
        return Error("negative")
    }
    return x * 2
}
safeDouble(x Int) Int {
    r := riskyOp(x)
    return r
}
main() {
    print(safeDouble(5))
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// riskyOp is failable, so safeDouble should have error propagation
	assertContains(t, out, "!= nil")
	assertContains(t, out, "fmt.Errorf")
}

func TestIntegrationMultiLevelInheritance(t *testing.T) {
	src := `
Animal {
    name String
    new(n String) {
        this.name = n
    }
    pub speak() String {
        return "..."
    }
}
Dog : Animal {
    new(n String) {
        super(n)
    }
    pub speak() String {
        return "Woof!"
    }
}
GoldenRetriever : Dog {
    new(n String) {
        super(n)
    }
    pub fetch() String {
        return "Fetch!"
    }
}
main() {
    g := GoldenRetriever("Buddy")
    print(g.speak())
    print(g.fetch())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type AnimalImpl struct")
	assertContains(t, out, "type DogImpl struct")
	assertContains(t, out, "type GoldenRetrieverImpl struct")
	assertContains(t, out, "func NewGoldenRetriever")
	// Receiver name is first letter of type name
	assertContains(t, out, "func (g *GoldenRetrieverImpl) Fetch()")
}

func TestIntegrationStringInterpolationInMethod(t *testing.T) {
	src := `
Person {
    name String
    age Int
    new(n String, a Int) {
        this.name = n
        this.age = a
    }
    greeting() String {
        return "Hello, I am {this.name} and I am {this.age} years old!"
    }
}
main() {
    p := Person("Alice", 30)
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
Wrapper<T> {
    content T?
    new() {
        this.content = null
    }
    set(v T) {
        this.content = v
    }
    hasContent() Bool {
        return this.content != null
    }
}
main() {
    w := Wrapper()
    print(w.hasContent())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type WrapperImpl[T any] struct")
	assertContains(t, out, "Content *T")
	// Constructor uses obj; methods use receiver initial (w for Wrapper)
	assertContains(t, out, "obj.Content = nil")
	assertContains(t, out, "w.Content != nil")
}

func TestIntegrationForInWithBuiltins(t *testing.T) {
	src := `
main() {
    words := ["hello", "world", "zinc"]
    for w in words {
        print(w.upper())
    }
    print(words.size())
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
	// Zinc channel syntax: go { ... }, ch.send(val), ch.receive()
	src := `
main() {
    var ch Chan<Int> = Chan.new(1)
    go {
        ch.send(42)
    }
    result := ch.receive()
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
    pub speak() String
}
Animal {
    name String
    new(n String) {
        this.name = n
    }
    pub getName() String {
        return this.name
    }
}
Dog : Animal, Speaker {
    breed String
    new(n String, b String) {
        super(n)
        this.breed = b
    }
    pub speak() String {
        return "Woof!"
    }
}
main() {
    d := Dog("Rex", "Lab")
    print(d.speak())
    print(d.getName())
}`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "type DogImpl struct")
	// Receiver name is first letter of class name (d for Dog)
	assertContains(t, out, "func (d *DogImpl) Speak() string")
	assertContains(t, out, "var _ Speaker = (*DogImpl)(nil)")
}

func TestIntegrationWithPlainResource(t *testing.T) {
	src := `
main() {
    with (f := openFile("x")) {
        print("ok")
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "f := openFile(\"x\")")
	assertContains(t, out, "if _c, ok := any(f).(io.Closer); ok { defer _c.Close() }")
}

func TestIntegrationWithInClassMethod(t *testing.T) {
	src := `
DataProcessor {
    new() {}
    pub process() {
        with (handle := openFile("data.txt")) {
            print("processing")
        }
    }
}
main() {
    p := DataProcessor()
    p.process()
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "func (d *DataProcessorImpl) Process()")
	assertContains(t, out, "handle := openFile(\"data.txt\")")
	assertContains(t, out, "if _c, ok := any(handle).(io.Closer); ok { defer _c.Close() }")
}

// --- sync.Locker via with ----------------------------------------------------

func TestIntegrationWithMutexInGoroutine(t *testing.T) {
	src := `
import "sync"
main() {
    mu := sync.Mutex.new()
    go {
        with (lock := mu) {
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

func TestIntegrationGoRoutineReturnError(t *testing.T) {
	src := `
risky() Int {
    return Error("oops")
}
main() {
    go {
        r := risky()
        print(r)
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "go func()")
	// In goroutine, failable calls auto-propagate as panic
	assertContains(t, out, "!= nil")
}

func TestIntegrationGoRoutineWith(t *testing.T) {
	src := `
main() {
    go {
        with (f := openFile("x")) {
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
main() {
    base := 10
    go {
        addBase := (x Int) Int => x + base
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

func TestIntegrationGoRoutineReturnErrorPanics(t *testing.T) {
	// return Error directly inside a goroutine should panic (not return) since
	// goroutines have their own void scope
	src := `
risky() Int {
    return Error("fatal")
}
main() {
    go {
        x := risky()
        print(x)
    }
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	assertContains(t, out, "go func()")
	// In goroutine, failable calls panic on error
	assertContains(t, out, "panic(")
}

func TestIntegrationReturnErrorInsideFailable(t *testing.T) {
	// return Error inside a failable function emits return zero, fmt.Errorf
	src := `
risky() Int {
    return Error("fn error")
}
caller() Int {
    r := risky()
    return r
}
main() {
    x := caller()
    print(x)
}
`
	out, errs := transpile(src)
	if errs != nil {
		t.Fatal(errs)
	}
	// risky emits return 0, fmt.Errorf("fn error")
	assertContains(t, out, "return 0, fmt.Errorf(\"fn error\")")
	// caller is transitively failable — auto-propagation
	assertContains(t, out, "!= nil")
}
