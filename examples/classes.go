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
type AnimalImpl struct {
	Name string
}

func (a *AnimalImpl) SetName(v string) { a.Name = v }

type Animal interface {
	SetName(string)
	GetName() string
}

var _ Animal = (*AnimalImpl)(nil)

func NewAnimal(name string) *AnimalImpl {
	obj := &AnimalImpl{}
//line examples/classes.zn:9
	obj.Name = name
	return obj
}

func (a *AnimalImpl) GetName() string {
//line examples/classes.zn:13
	return a.Name
}

//line examples/classes.zn:17
type DogImpl struct {
	AnimalImpl
	Breed string
}

func (d *DogImpl) GetBreed() string  { return d.Breed }
func (d *DogImpl) SetBreed(v string) { d.Breed = v }

type Dog interface {
	Animal
	Speaker
	GetBreed() string
	SetBreed(string)
	Speak() string
	Describe() string
}

var _ Dog = (*DogImpl)(nil)
var _ Speaker = (*DogImpl)(nil)

func NewDog(name string, breed string) *DogImpl {
	obj := &DogImpl{
		AnimalImpl: *NewAnimal(name),
	}
//line examples/classes.zn:22
	obj.Breed = breed
	return obj
}

func (d *DogImpl) Speak() string {
//line examples/classes.zn:26
	return "Woof!"
}

func (d *DogImpl) Describe() string {
//line examples/classes.zn:30
	return d.GetName()
}

//line examples/classes.zn:35
func greetAnimal(a Animal) {
//line examples/classes.zn:36
	fmt.Println(fmt.Sprintf("Hello, %v!", a.GetName()))
}

//line examples/classes.zn:39
func makeSpeak(s Speaker) {
//line examples/classes.zn:40
	fmt.Println(s.Speak())
}

//line examples/classes.zn:43
func main() {
//line examples/classes.zn:44
	d := NewDog("Rex", "Labrador")
//line examples/classes.zn:45
	fmt.Println(d.Speak())
//line examples/classes.zn:46
	fmt.Println(d.Describe())
//line examples/classes.zn:47
	greetAnimal(d)
//line examples/classes.zn:48
	makeSpeak(d)
}
