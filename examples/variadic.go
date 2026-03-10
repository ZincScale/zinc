package main

import (
	"fmt"
	"strings"
)

//line examples/variadic.zn:2
func sum(nums ...int) int {
//line examples/variadic.zn:3
	total := 0
//line examples/variadic.zn:4
	for _, n := range nums {
//line examples/variadic.zn:5
		total += n
	}
//line examples/variadic.zn:7
	return total
}

//line examples/variadic.zn:11
func log(level string, messages ...string) {
//line examples/variadic.zn:12
	for _, msg := range messages {
//line examples/variadic.zn:13
		fmt.Println(fmt.Sprintf("[%v] %v", level, msg))
	}
}

//line examples/variadic.zn:18
type BuilderImpl struct {
	Parts []string
}

func (b *BuilderImpl) GetParts() []string  { return b.Parts }
func (b *BuilderImpl) SetParts(v []string) { b.Parts = v }

type Builder interface {
	GetParts() []string
	SetParts([]string)
	Append(items ...string)
	Build() string
}

var _ Builder = (*BuilderImpl)(nil)

func NewBuilder() *BuilderImpl {
	obj := &BuilderImpl{}
//line examples/variadic.zn:22
	obj.Parts = []string{"placeholder"}
//line examples/variadic.zn:23
	obj.Parts = obj.Parts[0:0]
	return obj
}

func (b *BuilderImpl) Append(items ...string) {
//line examples/variadic.zn:27
	for _, item := range items {
//line examples/variadic.zn:28
		b.Parts = append(b.Parts, item)
	}
}

func (b *BuilderImpl) Build() string {
//line examples/variadic.zn:33
	return strings.Join(b.Parts, ", ")
}

//line examples/variadic.zn:37
func main() {
//line examples/variadic.zn:39
	fmt.Println(sum(1, 2, 3))
//line examples/variadic.zn:40
	fmt.Println(sum(10, 20, 30, 40))
//line examples/variadic.zn:41
	fmt.Println(sum())
//line examples/variadic.zn:44
	nums := []int{5, 10, 15}
//line examples/variadic.zn:45
	fmt.Println(sum(nums...))
//line examples/variadic.zn:48
	log("INFO", "server started", "listening on :8080")
//line examples/variadic.zn:51
	items := []int{1, 2}
//line examples/variadic.zn:52
	items = append(items, 3, 4, 5)
//line examples/variadic.zn:53
	fmt.Println(items)
//line examples/variadic.zn:56
	more := []int{6, 7, 8}
//line examples/variadic.zn:57
	items = append(items, more...)
//line examples/variadic.zn:58
	fmt.Println(items)
//line examples/variadic.zn:61
	b := NewBuilder()
//line examples/variadic.zn:62
	b.Append("alpha", "beta", "gamma")
//line examples/variadic.zn:63
	fmt.Println(b.Build())
//line examples/variadic.zn:66
	who := "Alice"
//line examples/variadic.zn:67
	age := 30
//line examples/variadic.zn:68
	fmt.Println(fmt.Sprintf("Hello %v, age %v", who, age))
}
