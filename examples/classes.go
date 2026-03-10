//go:build ignore

package main

import (
	"fmt"
)

//line examples/classes.zn:1
type Speaker interface {
	Speak() string
}

//line examples/classes.zn:5
type Animal struct {
	Name string
}

func NewAnimal(name string) *Animal {
	obj := &Animal{}
//line examples/classes.zn:9
	obj.Name = name
	return obj
}

func (a *Animal) GetName() string {
//line examples/classes.zn:13
	return a.Name
}

//line examples/classes.zn:17
type Dog struct {
	Animal
	Breed string
}

var _ Speaker = (*Dog)(nil)

func NewDog(name string, breed string) *Dog {
	obj := &Dog{
		Animal: *NewAnimal(name),
	}
//line examples/classes.zn:22
	obj.Breed = breed
	return obj
}

func (d *Dog) Speak() string {
//line examples/classes.zn:26
	return "Woof!"
}

func (d *Dog) Describe() string {
//line examples/classes.zn:30
	return d.GetName()
}

//line examples/classes.zn:34
func main() {
//line examples/classes.zn:35
	d := NewDog("Rex", "Labrador")
//line examples/classes.zn:36
	fmt.Println(d.Speak())
//line examples/classes.zn:37
	fmt.Println(d.Describe())
//line examples/classes.zn:38
	fmt.Println(d.GetName())
}
