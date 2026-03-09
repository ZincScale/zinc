//go:build ignore

package main

import (
	"fmt"
)

func fib(n int) int {
	if n <= 1 {
		return n
	}
	return (fib((n - 1)) + fib((n - 2)))
}

func fibIterative(n int) int {
	if n <= 1 {
		return n
	}
	a := 0
	b := 1
	i := 2
	for i <= n {
		tmp := (a + b)
		a = b
		b = tmp
		i += 1
	}
	return b
}

func main() {
	fmt.Println("Fibonacci (recursive):")
	i := 0
	for i <= 10 {
		fmt.Println(fib(i))
		i += 1
	}
	fmt.Println("Fibonacci (iterative):")
	j := 0
	for j <= 10 {
		fmt.Println(fibIterative(j))
		j += 1
	}
}
