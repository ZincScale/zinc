package main

import (
	"fmt"
)

func divide(a int, b int) (int, error) {
	if b == 0 {
		return 0, fmt.Errorf("division by zero")
	}
	return (a / b), nil
}

func main() {
	result, err := divide(10, 2)
	if err != nil {
		fmt.Println(fmt.Sprintf("error: %v", err))
	} else {
		fmt.Println(fmt.Sprintf("10 / 2 = %v", result))
	}
	result2, err2 := divide(10, 0)
	if err2 != nil {
		fmt.Println(fmt.Sprintf("caught: %v", err2))
	} else {
		fmt.Println(result2)
	}
	{
		e := func() error {
			answer, _err := divide(100, 4)
			if _err != nil {
				return _err
			}
			fmt.Println(fmt.Sprintf("100 / 4 = %v", answer))
			return nil
		}()
		if e != nil {
			fmt.Println(fmt.Sprintf("error: %v", e))
		}
	}
}
