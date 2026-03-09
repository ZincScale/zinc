package main

import (
	"fmt"
)

const PI = 3.14159

const MAX_RETRIES int = 3

const APP_NAME string = "Growler"

const DEBUG bool = false

func circleArea(radius float64) float64 {
	return ((PI * radius) * radius)
}

func main() {
	fmt.Println(fmt.Sprintf("%v v1.0", APP_NAME))
	fmt.Println(fmt.Sprintf("PI = %v", PI))
	fmt.Println(fmt.Sprintf("Area of circle (r=5): %v", circleArea(5.0)))
	fmt.Println(fmt.Sprintf("Max retries: %v", MAX_RETRIES))
	fmt.Println(fmt.Sprintf("Debug mode: %v", DEBUG))
}
