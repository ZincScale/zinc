//go:build ignore

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
	{
		err := func() error {
			result, _err := divide(10, 2)
			if _err != nil {
				return _err
			}
			fmt.Println(result)
			return nil
		}()
		if err != nil {
			fmt.Println("caught error")
		}
	}
	{
		err := func() error {
			result, _err := divide(5, 0)
			if _err != nil {
				return _err
			}
			fmt.Println(result)
			return nil
		}()
		if err != nil {
			fmt.Println("caught division by zero")
		}
	}
}
