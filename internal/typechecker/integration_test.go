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

package typechecker

import (
	"testing"
)

func TestTypecheckerKitchenSink(t *testing.T) {
	src := `
interface Describable {
    String describe()
}
enum Level { Low, Mid, High }
Score<T> : Describable {
    T value
    Level level
    new(T v, Level l) {
        this.value = v
        this.level = l
    }
    String describe() {
        return "Score"
    }
}
Level getLevel(Int s) {
    if s > 80 {
        return Level.High
    }
    return Level.Low
}
main() {
    var lvl = getLevel(90)
    var sc = Score(42, lvl)
    print(sc.describe())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestTypecheckerFailableAutoPropagate(t *testing.T) {
	src := `
Int risky(Int x) {
    if x < 0 { return Error("bad") }
    return x
}
Int run() {
    var a = risky(1)
    return a
}
main() {
    print(run())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestTypecheckerOrHandlerScope(t *testing.T) {
	src := `
Int risky(Int x) {
    if x < 0 { return Error("bad") }
    return x
}
main() {
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
Base {
    Int id
    new(Int i) {
        this.id = i
    }
    pub Int getId() {
        return this.id
    }
}
Middle : Base {
    new(Int i) {
        super(i)
    }
    pub Int doubled() {
        return this.id * 2
    }
}
Leaf : Middle {
    new(Int i) {
        super(i)
    }
    pub String label() {
        return "leaf"
    }
}
main() {
    var l = Leaf(5)
    print(l.label())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestTypecheckerReturnTypeMismatchInMethod(t *testing.T) {
	src := `
Calc {
    Int double(Int x) {
        return "oops"
    }
}
main() {
    var c = Calc()
}`
	errs := checkSrc(src)
	if !hasError(errs, "return") {
		t.Errorf("expected a return type mismatch error, got: %v", errs)
	}
}

func TestTypecheckerClassPlusInterfaceNoFalsePositives(t *testing.T) {
	src := `
interface Speaker {
    pub String speak()
}
Animal {
    String name
    new(String n) {
        this.name = n
    }
    pub String getName() {
        return this.name
    }
}
Dog : Animal, Speaker {
    new(String n) {
        super(n)
    }
    pub String speak() {
        return "Woof!"
    }
}
main() {
    var d = Dog("Rex")
    print(d.speak())
    print(d.getName())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}
