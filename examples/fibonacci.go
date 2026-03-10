//go:build ignore

package main

import (
	"fmt"
)

//line examples/fibonacci.zn:3
func fib(n int) int {
//line examples/fibonacci.zn:4
	if n <= 1 {
//line examples/fibonacci.zn:5
		return n
	}
//line examples/fibonacci.zn:7
	return (fib((n - 1)) + fib((n - 2)))
}

//line examples/fibonacci.zn:10
func fibIterative(n int) int {
//line examples/fibonacci.zn:11
	if n <= 1 {
//line examples/fibonacci.zn:12
		return n
	}
//line examples/fibonacci.zn:14
	a := 0
//line examples/fibonacci.zn:15
	b := 1
//line examples/fibonacci.zn:16
	i := 2
//line examples/fibonacci.zn:17
	for i <= n {
//line examples/fibonacci.zn:18
		tmp := (a + b)
//line examples/fibonacci.zn:19
		a = b
//line examples/fibonacci.zn:20
		b = tmp
//line examples/fibonacci.zn:21
		i += 1
	}
//line examples/fibonacci.zn:23
	return b
}

//line examples/fibonacci.zn:26
func main() {
//line examples/fibonacci.zn:27
	fmt.Println("Fibonacci (recursive):")
//line examples/fibonacci.zn:28
	i := 0
//line examples/fibonacci.zn:29
	for i <= 10 {
//line examples/fibonacci.zn:30
		fmt.Println(fib(i))
//line examples/fibonacci.zn:31
		i += 1
	}
//line examples/fibonacci.zn:34
	fmt.Println("Fibonacci (iterative):")
//line examples/fibonacci.zn:35
	j := 0
//line examples/fibonacci.zn:36
	for j <= 10 {
//line examples/fibonacci.zn:37
		fmt.Println(fibIterative(j))
//line examples/fibonacci.zn:38
		j += 1
	}
}
