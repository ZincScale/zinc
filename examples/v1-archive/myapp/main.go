package main

import (
	"fmt"
	"myapp/models"
	"myapp/utils"
)

//line main.zn:4
func main() {
//line main.zn:5
	sum := utils.Add(10, 32)
//line main.zn:6
	product := utils.Multiply(6, 7)
//line main.zn:7
	fmt.Println(sum)
//line main.zn:8
	fmt.Println(product)
//line main.zn:10
	var dog interface{} = models.NewDog("Rex")
//line main.zn:11
	fmt.Println(dog)
}
