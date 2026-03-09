package main

import (
	"fmt"
)

type Address struct {
	City string
	Zip  string
}

func NewAddress(city string, zip string) *Address {
	obj := &Address{}
	obj.City = city
	obj.Zip = zip
	return obj
}

func (a *Address) Format() string {
	return fmt.Sprintf("%v %v", a.City, a.Zip)
}

type User struct {
	Name    string
	Address *Address
}

func NewUser(name string, addr *Address) *User {
	obj := &User{}
	obj.Name = name
	obj.Address = addr
	return obj
}

func (u *User) Greet() string {
	return fmt.Sprintf("Hi, I'm %v", u.Name)
}

func main() {
	alice := NewUser("Alice", NewAddress("NYC", "10001"))
	fmt.Println(func() interface{} {
		if alice != nil {
			return alice.Name
		}
		return nil
	}())
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
	fmt.Println(func() interface{} {
		if alice != nil {
			return alice.Greet()
		}
		return nil
	}())
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
	var nobody *User = nil
	fmt.Println(func() interface{} {
		if nobody != nil {
			return nobody.Name
		}
		return nil
	}())
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
	if nobody != nil {
		nobody.Greet()
	}
	bob := NewUser("Bob", nil)
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
	var ghost *User = nil
	if ghost != nil {
		ghost.Greet()
	}
	fmt.Println("done")
}
