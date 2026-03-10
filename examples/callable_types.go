package main

import (
	"fmt"
)

//line examples/callable_types.zn:3
func apply(f func(int) int, x int) int {
//line examples/callable_types.zn:4
	return f(x)
}

//line examples/callable_types.zn:7
func combine(f func(int, int) int, a int, b int) int {
//line examples/callable_types.zn:8
	return f(a, b)
}

//line examples/callable_types.zn:11
func run(callback func()) {
//line examples/callable_types.zn:12
	callback()
}

//line examples/callable_types.zn:15
func main() {
//line examples/callable_types.zn:17
	double := func(x int) int { return (x * 2) }
//line examples/callable_types.zn:18
	fmt.Println(apply(double, 7))
//line examples/callable_types.zn:20
	add := func(a int, b int) int { return (a + b) }
//line examples/callable_types.zn:21
	fmt.Println(combine(add, 3, 4))
//line examples/callable_types.zn:24
	run(func() {
		fmt.Println("callback executed!")
	})
//line examples/callable_types.zn:27
	transform := func(s string) int { return len(s) }
//line examples/callable_types.zn:28
	fmt.Println(transform("hello"))
//line examples/callable_types.zn:31
	multiplier := func(factor int) func(int) int {
		return func(x int) int { return (x * factor) }
	}
//line examples/callable_types.zn:34
	triple := multiplier(3)
//line examples/callable_types.zn:35
	fmt.Println(triple(10))
}
