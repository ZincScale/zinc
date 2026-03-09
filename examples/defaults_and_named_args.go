package main

import (
	"fmt"
)

func greet(name string, greeting string) {
	fmt.Println(fmt.Sprintf("%v, %v!", greeting, name))
}

func connect(host string, port int, tls bool) {
	fmt.Println(fmt.Sprintf("Connecting to %v:%v (tls=%v)", host, port, tls))
}

type Dog struct {
	Name string
	Age  int
}

func NewDog(name string, age int) *Dog {
	obj := &Dog{}
	obj.Name = name
	obj.Age = age
	return obj
}

func (d *Dog) Describe() string {
	return fmt.Sprintf("%v, age %v", d.Name, d.Age)
}

func main() {
	greet("Alice", "Hello")
	greet("Bob", "Hi")
	connect("localhost", 8080, false)
	connect("example.com", 443, true)
	connect("secure.io", 8080, true)
	d1 := NewDog("Rex", 0)
	d2 := NewDog("Buddy", 3)
	d3 := NewDog("Spot", 5)
	fmt.Println(d1.Describe())
	fmt.Println(d2.Describe())
	fmt.Println(d3.Describe())
}
