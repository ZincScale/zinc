package models

//line models/dog.zn:3
type DogImpl struct {
	AnimalImpl
	Tricks []string
}

func (d *DogImpl) GetTricks() []string  { return d.Tricks }
func (d *DogImpl) SetTricks(v []string) { d.Tricks = v }

type Dog interface {
	Animal
	GetTricks() []string
	SetTricks([]string)
	LearnTrick(trick string)
	TrickCount() int
}

var _ Dog = (*DogImpl)(nil)

func NewDog(name string) *DogImpl {
	obj := &DogImpl{
		AnimalImpl: *NewAnimal(name, "Woof"),
	}
	return obj
}

func (d *DogImpl) LearnTrick(trick string) {
//line models/dog.zn:11
	d.Tricks = append(d.Tricks, trick)
}

func (d *DogImpl) TrickCount() int {
//line models/dog.zn:15
	return len(d.Tricks)
}
