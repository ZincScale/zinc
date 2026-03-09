//go:build ignore

package main

import (
	"fmt"
)

func identity[T any](val T) T {
	return val
}

func mapLen[K comparable, V any](m map[K]V) int {
	return len(m)
}

type Box[T any] struct {
	Value T
}

func NewBox[T any](v T) *Box[T] {
	obj := &Box[T]{}
	obj.Value = v
	return obj
}

func (b *Box[T]) Get() T {
	return b.Value
}

func (b *Box[T]) Set(v T) {
	b.Value = v
}

func main() {
	n := identity(42)
	fmt.Println(n)
	s := identity("Zinc")
	fmt.Println(s)
	box := NewBox(100)
	fmt.Println(box.Get())
	box.Set(200)
	fmt.Println(box.Get())
}
