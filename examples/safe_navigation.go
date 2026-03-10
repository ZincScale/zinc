//go:build ignore

package main

import (
	"fmt"
)

//line examples/safe_navigation.zn:5
type Address struct {
	City string
	Zip  string
}

func NewAddress(city string, zip string) *Address {
	obj := &Address{}
//line examples/safe_navigation.zn:10
	obj.City = city
//line examples/safe_navigation.zn:11
	obj.Zip = zip
	return obj
}

func (a *Address) Format() string {
//line examples/safe_navigation.zn:15
	return fmt.Sprintf("%v %v", a.City, a.Zip)
}

//line examples/safe_navigation.zn:19
type User struct {
	Name    string
	Address *Address
}

func NewUser(name string, addr *Address) *User {
	obj := &User{}
//line examples/safe_navigation.zn:24
	obj.Name = name
//line examples/safe_navigation.zn:25
	obj.Address = addr
	return obj
}

func (u *User) Greet() string {
//line examples/safe_navigation.zn:29
	return fmt.Sprintf("Hi, I'm %v", u.Name)
}

//line examples/safe_navigation.zn:33
func main() {
//line examples/safe_navigation.zn:35
	alice := NewUser("Alice", NewAddress("NYC", "10001"))
//line examples/safe_navigation.zn:36
	fmt.Println(func() interface{} {
		if alice != nil {
			return alice.Name
		}
		return nil
	}())
//line examples/safe_navigation.zn:39
	fmt.Println(func() interface{} {
		_s0 := alice
		if _s0 == nil {
			return nil
		}
		_s1 := _s0.Address
		if _s1 == nil {
			return nil
		}
		return _s1.City
	}())
//line examples/safe_navigation.zn:42
	fmt.Println(func() interface{} {
		if alice != nil {
			return alice.Greet()
		}
		return nil
	}())
//line examples/safe_navigation.zn:43
	fmt.Println(func() interface{} {
		_s0 := alice
		if _s0 == nil {
			return nil
		}
		_s1 := _s0.Address
		if _s1 == nil {
			return nil
		}
		return _s1.Format()
	}())
//line examples/safe_navigation.zn:46
	var nobody *User = nil
//line examples/safe_navigation.zn:47
	fmt.Println(func() interface{} {
		if nobody != nil {
			return nobody.Name
		}
		return nil
	}())
//line examples/safe_navigation.zn:48
	fmt.Println(func() interface{} {
		_s0 := nobody
		if _s0 == nil {
			return nil
		}
		_s1 := _s0.Address
		if _s1 == nil {
			return nil
		}
		return _s1.City
	}())
//line examples/safe_navigation.zn:49
	if nobody != nil {
		nobody.Greet()
	}
//line examples/safe_navigation.zn:52
	bob := NewUser("Bob", nil)
//line examples/safe_navigation.zn:53
	fmt.Println(func() interface{} {
		_s0 := bob
		if _s0 == nil {
			return nil
		}
		_s1 := _s0.Address
		if _s1 == nil {
			return nil
		}
		return _s1.City
	}())
//line examples/safe_navigation.zn:56
	var ghost *User = nil
//line examples/safe_navigation.zn:57
	if ghost != nil {
		ghost.Greet()
	}
//line examples/safe_navigation.zn:58
	fmt.Println("done")
}
