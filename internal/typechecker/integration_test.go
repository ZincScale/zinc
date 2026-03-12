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
    describe() String
}
enum Level { Low, Mid, High }
Score<T> : Describable {
    value T
    level Level
    new(v T, l Level) {
        this.value = v
        this.level = l
    }
    describe() String {
        return "Score"
    }
}
getLevel(s Int) Level {
    if s > 80 {
        return Level.High
    }
    return Level.Low
}
main() {
    lvl := getLevel(90)
    sc := Score(42, lvl)
    print(sc.describe())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestTypecheckerFailableAutoPropagate(t *testing.T) {
	src := `
risky(x Int) Int {
    if x < 0 { return Error("bad") }
    return x
}
run() Int {
    a := risky(1)
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
risky(x Int) Int {
    if x < 0 { return Error("bad") }
    return x
}
main() {
    a := risky(1) or {
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
    id Int
    new(i Int) {
        this.id = i
    }
    pub getId() Int {
        return this.id
    }
}
Middle : Base {
    new(i Int) {
        super(i)
    }
    pub doubled() Int {
        return this.id * 2
    }
}
Leaf : Middle {
    new(i Int) {
        super(i)
    }
    pub label() String {
        return "leaf"
    }
}
main() {
    l := Leaf(5)
    print(l.label())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}

func TestTypecheckerReturnTypeMismatchInMethod(t *testing.T) {
	src := `
Calc {
    double(x Int) Int {
        return "oops"
    }
}
main() {
    c := Calc()
}`
	errs := checkSrc(src)
	if !hasError(errs, "return") {
		t.Errorf("expected a return type mismatch error, got: %v", errs)
	}
}

func TestTypecheckerClassPlusInterfaceNoFalsePositives(t *testing.T) {
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
    new(n String) {
        super(n)
    }
    pub speak() String {
        return "Woof!"
    }
}
main() {
    d := Dog("Rex")
    print(d.speak())
    print(d.getName())
}`
	errs := checkSrc(src)
	noErrors(t, errs, src)
}
