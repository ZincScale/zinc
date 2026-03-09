//go:build ignore

package main

import (
	"fmt"
)

func main() {
	fmt.Println("--- labeled break ---")
outer:
	for i := 0; i < 5; i += 1 {
		for j := 0; j < 5; j += 1 {
			if (i + j) == 6 {
				fmt.Println(fmt.Sprintf("breaking at i=%v, j=%v", i, j))
				break outer
			}
		}
	}
	fmt.Println("--- labeled continue ---")
search:
	for i := 0; i < 4; i += 1 {
		for j := 0; j < 4; j += 1 {
			if j == 2 {
				continue search
			}
			fmt.Println(fmt.Sprintf("i=%v, j=%v", i, j))
		}
	}
}
