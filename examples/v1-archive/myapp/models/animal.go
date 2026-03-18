package models

import (
	"fmt"
)

//line models/animal.zn:3
type AnimalImpl struct {
	Name  string
	Sound string
}

func (a *AnimalImpl) SetName(v string)  { a.Name = v }
func (a *AnimalImpl) GetSound() string  { return a.Sound }
func (a *AnimalImpl) SetSound(v string) { a.Sound = v }

type Animal interface {
	SetName(string)
	GetSound() string
	SetSound(string)
	Speak() string
	GetName() string
}

var _ Animal = (*AnimalImpl)(nil)

func NewAnimal(name string, sound string) *AnimalImpl {
	obj := &AnimalImpl{}
//line models/animal.zn:8
	obj.Name = name
//line models/animal.zn:9
	obj.Sound = sound
	return obj
}

func (a *AnimalImpl) Speak() string {
//line models/animal.zn:13
	return fmt.Sprintf("%v says %v!", a.Name, a.Sound)
}

func (a *AnimalImpl) GetName() string {
//line models/animal.zn:17
	return a.Name
}
