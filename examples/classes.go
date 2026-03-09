//go:build ignore

package main

import (
	"fmt"
)

type Speaker interface {
	Speak() string
}

type Animal struct {
	Name string
}

func NewAnimal(name string) *Animal {
	obj := &Animal{}
	obj.Name = name
	return obj
}

func (a *Animal) GetName() string {
	return a.Name
}

type Dog struct {
	Animal
	Breed string
}

var _ Speaker = (*Dog)(nil)

func NewDog(name string, breed string) *Dog {
	obj := &Dog{
		Animal: Animal{Name: name},
	}
	obj.Breed = breed
	return obj
}

func (d *Dog) Speak() string {
	return "Woof!"
}

func (d *Dog) Describe() string {
	return d.GetName()
}

func main() {
	d := NewDog("Rex", "Labrador")
	fmt.Println(d.Speak())
	fmt.Println(d.Describe())
	fmt.Println(d.GetName())
}
