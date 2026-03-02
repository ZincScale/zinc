package models

type Dog struct {
	Animal
	Tricks []string
}

func NewDog(name string) *Dog {
	obj := &Dog{
		Animal: Animal{name, "Woof"},
	}
	return obj
}

func (d *Dog) LearnTrick(trick string) {
	d.Tricks = append(d.Tricks, trick)
}

func (d *Dog) TrickCount() int {
	return len(d.Tricks)
}
