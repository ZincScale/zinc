//go:build ignore

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
type BoxImpl[T any] struct {
	Value T
}

func (b *BoxImpl[T]) GetValue() T  { return b.Value }
func (b *BoxImpl[T]) SetValue(v T) { b.Value = v }

type Box[T any] interface {
	GetValue() T
	SetValue(T)
	Get() T
	Set(v T)
}

func NewBox[T any](v T) *BoxImpl[T] {
	obj := &BoxImpl[T]{}
//line examples/generics.zn:15
	obj.Value = v
	return obj
}

func (b *BoxImpl[T]) Get() T {
//line examples/generics.zn:19
	return b.Value
}

func (b *BoxImpl[T]) Set(v T) {
//line examples/generics.zn:23
	b.Value = v
}

//line examples/generics.zn:28
type StackImpl[T any] struct {
	Items []T
}

func (s *StackImpl[T]) GetItems() []T  { return s.Items }
func (s *StackImpl[T]) SetItems(v []T) { s.Items = v }

type Stack[T any] interface {
	GetItems() []T
	SetItems([]T)
	Push(item T)
	Count() int
}

func NewStack[T any](initial T) *StackImpl[T] {
	obj := &StackImpl[T]{}
//line examples/generics.zn:32
	obj.Items = []T{}
//line examples/generics.zn:33
	obj.Items = append(obj.Items, initial)
	return obj
}

func (s *StackImpl[T]) Push(item T) {
//line examples/generics.zn:37
	s.Items = append(s.Items, item)
}

func (s *StackImpl[T]) Count() int {
//line examples/generics.zn:41
	return len(s.Items)
}

//line examples/generics.zn:46
func printBox(b Box[string]) {
//line examples/generics.zn:47
	fmt.Println(b.Get())
}

//line examples/generics.zn:50
func main() {
//line examples/generics.zn:52
	n := identity(42)
//line examples/generics.zn:53
	fmt.Println(n)
//line examples/generics.zn:55
	s := identity("Zinc")
//line examples/generics.zn:56
	fmt.Println(s)
//line examples/generics.zn:59
	box := NewBox(100)
//line examples/generics.zn:60
	fmt.Println(box.Get())
//line examples/generics.zn:62
	box.Set(200)
//line examples/generics.zn:63
	fmt.Println(box.Get())
//line examples/generics.zn:66
	stack := NewStack(1)
//line examples/generics.zn:67
	stack.Push(2)
//line examples/generics.zn:68
	stack.Push(3)
//line examples/generics.zn:69
	fmt.Println(stack.Count())
//line examples/generics.zn:72
	greeting := NewBox("Hello from generics!")
//line examples/generics.zn:73
	printBox(greeting)
}
