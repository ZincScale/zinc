//go:build ignore

package main

import (
	"fmt"
	"os"
)

//line examples/errors.zn:1
func divide(a int, b int) (int, error) {
//line examples/errors.zn:2
	if b == 0 {
//line examples/errors.zn:3
		return 0, fmt.Errorf("division by zero")
	}
//line examples/errors.zn:5
	return (a / b), nil
}

//line examples/errors.zn:8
func main() {
//line examples/errors.zn:10
	result, _err0 := divide(10, 2)
	if _err0 != nil {
		panic(_err0)
	}
//line examples/errors.zn:11
	fmt.Println(result)
//line examples/errors.zn:14
	result2, _err1 := divide(5, 0)
	if _err1 != nil {
		err := _err1.Error()
		_ = err
//line examples/errors.zn:15
		fmt.Println(fmt.Sprintf("caught: %v", err))
//line examples/errors.zn:16
		os.Exit(0)
	}
//line examples/errors.zn:18
	fmt.Println(result2)
}
