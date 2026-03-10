package main

import (
	"fmt"
	"strings"
)

//line examples/variadic.zn:4
func sum(nums ...int) int {
//line examples/variadic.zn:5
	total := 0
//line examples/variadic.zn:6
	for _, n := range nums {
//line examples/variadic.zn:7
		total += n
	}
//line examples/variadic.zn:9
	return total
}

//line examples/variadic.zn:13
func log(level string, messages ...string) {
//line examples/variadic.zn:14
	for _, msg := range messages {
//line examples/variadic.zn:15
		fmt.Println(fmt.Sprintf("[%v] %v", level, msg))
	}
}

//line examples/variadic.zn:20
type Builder struct {
	Parts []string
}

func NewBuilder() *Builder {
	obj := &Builder{}
//line examples/variadic.zn:24
	obj.Parts = []string{"placeholder"}
//line examples/variadic.zn:25
	obj.Parts = obj.Parts[0:0]
	return obj
}

func (b *Builder) Append(items ...string) {
//line examples/variadic.zn:29
	for _, item := range items {
//line examples/variadic.zn:30
		b.Parts = append(b.Parts, item)
	}
}

func (b *Builder) Build() string {
//line examples/variadic.zn:35
	return strings.Join(b.Parts, ", ")
}

//line examples/variadic.zn:39
func main() {
//line examples/variadic.zn:41
	fmt.Println(sum(1, 2, 3))
//line examples/variadic.zn:42
	fmt.Println(sum(10, 20, 30, 40))
//line examples/variadic.zn:43
	fmt.Println(sum())
//line examples/variadic.zn:46
	nums := []int{5, 10, 15}
//line examples/variadic.zn:47
	fmt.Println(sum(nums...))
//line examples/variadic.zn:50
	log("INFO", "server started", "listening on :8080")
//line examples/variadic.zn:53
	items := []int{1, 2}
//line examples/variadic.zn:54
	items = append(items, 3, 4, 5)
//line examples/variadic.zn:55
	fmt.Println(items)
//line examples/variadic.zn:58
	more := []int{6, 7, 8}
//line examples/variadic.zn:59
	items = append(items, more...)
//line examples/variadic.zn:60
	fmt.Println(items)
//line examples/variadic.zn:63
	b := NewBuilder()
//line examples/variadic.zn:64
	b.Append("alpha", "beta", "gamma")
//line examples/variadic.zn:65
	fmt.Println(b.Build())
//line examples/variadic.zn:68
	msg := fmt.Sprintf("Hello %s, age %d", "Alice", 30)
//line examples/variadic.zn:69
	fmt.Println(msg)
}
