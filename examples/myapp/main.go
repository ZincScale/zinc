//go:build ignore

package main

import (
	"fmt"
	"myapp/models"
	"myapp/utils"
)

func main() {
	sum := utils.Add(10, 32)
	product := utils.Multiply(6, 7)
	fmt.Println(sum)
	fmt.Println(product)
	dog := models.NewDog("Rex")
	fmt.Println(dog)
}
