package main

import (
	"fmt"
)

//line examples/generics.zn:3
func identity[T any](val T) T {
//line examples/generics.zn:4
	return val
}

//line examples/generics.zn:7
func mapLen[K comparable, V any](m map[K]V) int {
//line examples/generics.zn:8
	return len(m)
}

//line examples/generics.zn:11
type Box[T any] struct {
	Value T
}

func NewBox[T any](v T) *Box[T] {
	obj := &Box[T]{}
//line examples/generics.zn:15
	obj.Value = v
	return obj
}

func (b *Box[T]) Get() T {
//line examples/generics.zn:19
	return b.Value
}

func (b *Box[T]) Set(v T) {
//line examples/generics.zn:23
	b.Value = v
}

//line examples/generics.zn:27
func main() {
//line examples/generics.zn:28
	n := identity(42)
//line examples/generics.zn:29
	fmt.Println(n)
//line examples/generics.zn:31
	s := identity("Zinc")
//line examples/generics.zn:32
	fmt.Println(s)
//line examples/generics.zn:34
	box := NewBox(100)
//line examples/generics.zn:35
	fmt.Println(box.Get())
//line examples/generics.zn:37
	box.Set(200)
//line examples/generics.zn:38
	fmt.Println(box.Get())
}
