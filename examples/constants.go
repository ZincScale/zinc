package main

import (
	"fmt"
)

//line examples/constants.zn:3
const PI = 3.14159

//line examples/constants.zn:4
const MAX_RETRIES int = 3

//line examples/constants.zn:5
const APP_NAME string = "Zinc"

//line examples/constants.zn:6
const DEBUG bool = false

//line examples/constants.zn:8
func circleArea(radius float64) float64 {
//line examples/constants.zn:9
	return ((PI * radius) * radius)
}

//line examples/constants.zn:12
func main() {
//line examples/constants.zn:13
	fmt.Println(fmt.Sprintf("%v v1.0", APP_NAME))
//line examples/constants.zn:14
	fmt.Println(fmt.Sprintf("PI = %v", PI))
//line examples/constants.zn:15
	fmt.Println(fmt.Sprintf("Area of circle (r=5): %v", circleArea(5.0)))
//line examples/constants.zn:16
	fmt.Println(fmt.Sprintf("Max retries: %v", MAX_RETRIES))
//line examples/constants.zn:17
	fmt.Println(fmt.Sprintf("Debug mode: %v", DEBUG))
}
