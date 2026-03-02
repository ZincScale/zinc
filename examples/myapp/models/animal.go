package models

import (
	"fmt"
)

type Animal struct {
	Name  string
	Sound string
}

func NewAnimal(name string, sound string) *Animal {
	obj := &Animal{}
	obj.Name = name
	obj.Sound = sound
	return obj
}

func (a *Animal) Speak() string {
	return fmt.Sprintf("%v says %v!", a.Name, a.Sound)
}

func (a *Animal) GetName() string {
	return a.Name
}
