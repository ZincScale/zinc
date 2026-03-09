package typechecker

import (
	"testing"
)

func TestTypecheckerKitchenSink(t *testing.T) {
	src := `
interface Describable {
    fn describe(): String
}
enum Level { Low, Mid, High }
class Score<T> : Describable {
    var value: T
    var level: Level
    construct new(v: T, l: Level) {
        this.value = v
        this.level = l
    }
    fn describe(): String {
        return "Score"
    }
}
fn getLevel(s: Int): Level {
    if (s > 80) {
        return Level.High
    }
    return Level.Low
}
fn main() {
    var lvl = getLevel(90)
    var sc = Score.new(42, lvl)
    print(sc.describe())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestTypecheckerFailableAutoPropagate(t *testing.T) {
	src := `
fn risky(x: Int): Int {
    if (x < 0) { return Error("bad") }
    return x
}
fn run(): Int {
    var a = risky(1)
    return a
}
fn main() {
    print(run())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestTypecheckerOrHandlerScope(t *testing.T) {
	src := `
fn risky(x: Int): Int {
    if (x < 0) { return Error("bad") }
    return x
}
fn main() {
    var a = risky(1) or {
        print(err)
        exit(1)
    }
    print(a)
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestTypecheckerMultiLevelInheritance(t *testing.T) {
	src := `
class Base {
    var id: Int
    construct new(i: Int) {
        this.id = i
    }
    pub fn getId(): Int {
        return this.id
    }
}
class Middle : Base {
    construct new(i: Int) {
        super(i)
    }
    pub fn doubled(): Int {
        return this.id * 2
    }
}
class Leaf : Middle {
    construct new(i: Int) {
        super(i)
    }
    pub fn label(): String {
        return "leaf"
    }
}
fn main() {
    var l = Leaf.new(5)
    print(l.label())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestTypecheckerReturnTypeMismatchInMethod(t *testing.T) {
	src := `
class Calc {
    fn double(x: Int): Int {
        return "oops"
    }
}
fn main() {
    var c = Calc.new()
}`
	errs := checkSrc(src)
	if !hasError(errs, "return") {
		t.Errorf("expected a return type mismatch error, got: %v", errs)
	}
}

func TestTypecheckerClassPlusInterfaceNoFalsePositives(t *testing.T) {
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
    construct new(n: String) {
        super(n)
    }
    pub fn speak(): String {
        return "Woof!"
    }
}
fn main() {
    var d = Dog.new("Rex")
    print(d.speak())
    print(d.getName())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}
