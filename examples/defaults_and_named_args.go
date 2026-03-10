//go:build ignore

package main

import (
	"fmt"
)

//line examples/defaults_and_named_args.zn:3
func greet(name string, greeting string) {
//line examples/defaults_and_named_args.zn:4
	fmt.Println(fmt.Sprintf("%v, %v!", greeting, name))
}

//line examples/defaults_and_named_args.zn:7
func connect(host string, port int, tls bool) {
//line examples/defaults_and_named_args.zn:8
	fmt.Println(fmt.Sprintf("Connecting to %v:%v (tls=%v)", host, port, tls))
}

//line examples/defaults_and_named_args.zn:11
type Dog struct {
	Name string
	Age  int
}

func NewDog(name string, age int) *Dog {
	obj := &Dog{}
//line examples/defaults_and_named_args.zn:16
	obj.Name = name
//line examples/defaults_and_named_args.zn:17
	obj.Age = age
	return obj
}

func (d *Dog) Describe() string {
//line examples/defaults_and_named_args.zn:21
	return fmt.Sprintf("%v, age %v", d.Name, d.Age)
}

//line examples/defaults_and_named_args.zn:25
func main() {
//line examples/defaults_and_named_args.zn:27
	greet("Alice", "Hello")
//line examples/defaults_and_named_args.zn:28
	greet("Bob", "Hi")
//line examples/defaults_and_named_args.zn:31
	connect("localhost", 8080, false)
//line examples/defaults_and_named_args.zn:32
	connect("example.com", 443, true)
//line examples/defaults_and_named_args.zn:33
	connect("secure.io", 8080, true)
//line examples/defaults_and_named_args.zn:36
	d1 := NewDog("Rex", 0)
//line examples/defaults_and_named_args.zn:37
	d2 := NewDog("Buddy", 3)
//line examples/defaults_and_named_args.zn:38
	d3 := NewDog("Spot", 5)
//line examples/defaults_and_named_args.zn:39
	fmt.Println(d1.Describe())
//line examples/defaults_and_named_args.zn:40
	fmt.Println(d2.Describe())
//line examples/defaults_and_named_args.zn:41
	fmt.Println(d3.Describe())
}
