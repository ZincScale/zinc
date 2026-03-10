//go:build ignore

package main

import (
	"fmt"
	"os"
)

//line examples/tuple_unpacking.zn:3
func divide(a int, b int) (int, error) {
//line examples/tuple_unpacking.zn:4
	if b == 0 {
//line examples/tuple_unpacking.zn:5
		return 0, fmt.Errorf("division by zero")
	}
//line examples/tuple_unpacking.zn:7
	return (a / b), nil
}

//line examples/tuple_unpacking.zn:10
func main() {
//line examples/tuple_unpacking.zn:12
	result, _err0 := divide(10, 2)
	if _err0 != nil {
		panic(_err0)
	}
//line examples/tuple_unpacking.zn:13
	fmt.Println(fmt.Sprintf("10 / 2 = %v", result))
//line examples/tuple_unpacking.zn:16
	result2, _err1 := divide(10, 0)
	if _err1 != nil {
		err := _err1.Error()
		_ = err
//line examples/tuple_unpacking.zn:17
		fmt.Println(fmt.Sprintf("caught: %v", err))
//line examples/tuple_unpacking.zn:18
		os.Exit(0)
	}
//line examples/tuple_unpacking.zn:20
	fmt.Println(result2)
}
