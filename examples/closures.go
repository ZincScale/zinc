package main

import (
	"fmt"
	"os"
)

//line examples/closures.zn:1
func main() {
//line examples/closures.zn:3
	double := func(x int) int { return (x * 2) }
//line examples/closures.zn:4
	addTen := func(x int) int { return (x + 10) }
//line examples/closures.zn:6
	fmt.Println(double(5))
//line examples/closures.zn:7
	fmt.Println(addTen(3))
//line examples/closures.zn:10
	greet := func() string { return "Hello from lambda!" }
//line examples/closures.zn:11
	fmt.Println(greet())
//line examples/closures.zn:14
	describe := func(x int) string {
		if x > 0 {
			return "positive"
		}
		return "non-positive"
	}
//line examples/closures.zn:20
	fmt.Println(describe(42))
//line examples/closures.zn:21
	fmt.Println(describe((-1)))
//line examples/closures.zn:24
	makeMsg := func(name string) string { return fmt.Sprintf("Hello, %v!", name) }
//line examples/closures.zn:25
	fmt.Println(makeMsg("Zinc"))
//line examples/closures.zn:28
	safeDivide := func(a int, b int) (int, error) {
		if b == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return (a / b), nil
	}
//line examples/closures.zn:35
	result, _err0 := safeDivide(10, 2)
	if _err0 != nil {
		panic(_err0)
	}
//line examples/closures.zn:36
	fmt.Println(result)
//line examples/closures.zn:38
	bad, _err1 := safeDivide(10, 0)
	if _err1 != nil {
		err := _err1.Error()
		_ = err
//line examples/closures.zn:39
		fmt.Println(fmt.Sprintf("Caught: %v", err))
//line examples/closures.zn:40
		os.Exit(0)
	}
//line examples/closures.zn:42
	fmt.Println(bad)
}
