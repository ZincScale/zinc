package main

import (
	"fmt"
)

func apply(f func(int) int, x int) int {
	return f(x)
}

func combine(f func(int, int) int, a int, b int) int {
	return f(a, b)
}

func run(callback func()) {
	callback()
}

func main() {
	double := func(x int) int { return (x * 2) }
	fmt.Println(apply(double, 7))
	add := func(a int, b int) int { return (a + b) }
	fmt.Println(combine(add, 3, 4))
	run(func() {
		fmt.Println("callback executed!")
	})
	transform := func(s string) int { return len(s) }
	fmt.Println(transform("hello"))
	multiplier := func(factor int) func(int) int {
		return func(x int) int { return (x * factor) }
	}
	triple := multiplier(3)
	fmt.Println(triple(10))
}
