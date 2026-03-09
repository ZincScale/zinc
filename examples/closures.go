package main

import (
	"fmt"
)

func main() {
	double := func(x int) int { return (x * 2) }
	addTen := func(x int) int { return (x + 10) }
	fmt.Println(double(5))
	fmt.Println(addTen(3))
	greet := func() string { return "Hello from lambda!" }
	fmt.Println(greet())
	describe := func(x int) string {
		if x > 0 {
			return "positive"
		}
		return "non-positive"
	}
	fmt.Println(describe(42))
	fmt.Println(describe((-1)))
	makeMsg := func(name string) string { return fmt.Sprintf("Hello, %v!", name) }
	fmt.Println(makeMsg("Zinc"))
	safeDivide := func(a int, b int) (int, error) {
		if b == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return (a / b), nil
	}
	{
		err := func() error {
			result, _err := safeDivide(10, 2)
			if _err != nil {
				return _err
			}
			fmt.Println(result)
			bad, _err := safeDivide(10, 0)
			if _err != nil {
				return _err
			}
			fmt.Println(bad)
			return nil
		}()
		if err != nil {
			fmt.Println(fmt.Sprintf("Caught: %v", err))
		}
	}
}
