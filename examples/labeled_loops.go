package main

import (
	"fmt"
)

//line examples/labeled_loops.zn:3
func main() {
//line examples/labeled_loops.zn:5
	fmt.Println("--- labeled break ---")
//line examples/labeled_loops.zn:6
outer:
	for i := 0; i < 5; i += 1 {
//line examples/labeled_loops.zn:7
		for j := 0; j < 5; j += 1 {
//line examples/labeled_loops.zn:8
			if (i + j) == 6 {
//line examples/labeled_loops.zn:9
				fmt.Println(fmt.Sprintf("breaking at i=%v, j=%v", i, j))
				break outer
			}
		}
	}
//line examples/labeled_loops.zn:16
	fmt.Println("--- labeled continue ---")
//line examples/labeled_loops.zn:17
search:
	for i := 0; i < 4; i += 1 {
//line examples/labeled_loops.zn:18
		for j := 0; j < 4; j += 1 {
//line examples/labeled_loops.zn:19
			if j == 2 {
				continue search
			}
//line examples/labeled_loops.zn:22
			fmt.Println(fmt.Sprintf("i=%v, j=%v", i, j))
		}
	}
}
