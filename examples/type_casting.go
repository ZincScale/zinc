//go:build ignore

package main

import (
	"fmt"
)

func describe(x interface{}) string {
	if func() bool { _, ok := x.(int); return ok }() {
		n := x.(int)
		return fmt.Sprintf("Int: %v", n)
	}
	if func() bool { _, ok := x.(string); return ok }() {
		s := x.(string)
		return fmt.Sprintf("String: %v", s)
	}
	return "Unknown type"
}

func main() {
	var a interface{} = 42
	var b interface{} = "hello"
	fmt.Println(describe(a))
	fmt.Println(describe(b))
	n := a.(int)
	fmt.Println((n + 1))
	fmt.Println(func() bool { _, ok := a.(int); return ok }())
	fmt.Println(func() bool { _, ok := a.(string); return ok }())
}
