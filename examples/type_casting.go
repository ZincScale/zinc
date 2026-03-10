//go:build ignore

package main

import (
	"fmt"
)

//line examples/type_casting.zn:3
func describe(x interface{}) string {
//line examples/type_casting.zn:4
	if func() bool { _, ok := x.(int); return ok }() {
//line examples/type_casting.zn:5
		n := x.(int)
//line examples/type_casting.zn:6
		return fmt.Sprintf("Int: %v", n)
	}
//line examples/type_casting.zn:8
	if func() bool { _, ok := x.(string); return ok }() {
//line examples/type_casting.zn:9
		s := x.(string)
//line examples/type_casting.zn:10
		return fmt.Sprintf("String: %v", s)
	}
//line examples/type_casting.zn:12
	return "Unknown type"
}

//line examples/type_casting.zn:15
func main() {
//line examples/type_casting.zn:16
	var a interface{} = 42
//line examples/type_casting.zn:17
	var b interface{} = "hello"
//line examples/type_casting.zn:19
	fmt.Println(describe(a))
//line examples/type_casting.zn:20
	fmt.Println(describe(b))
//line examples/type_casting.zn:23
	n := a.(int)
//line examples/type_casting.zn:24
	fmt.Println((n + 1))
//line examples/type_casting.zn:27
	fmt.Println(func() bool { _, ok := a.(int); return ok }())
//line examples/type_casting.zn:28
	fmt.Println(func() bool { _, ok := a.(string); return ok }())
}
