//go:build ignore

package main

import (
	"fmt"
	"io"
	"os"
	"sync"
)

func main() {
	path := (os.TempDir() + "/zinc_example.txt")
	{
		f, _err0 := os.Create(path)
		if _err0 != nil {
			panic(_err0)
		}
		if _c, ok := any(f).(io.Closer); ok {
			defer _c.Close()
		}
		if _l, ok := any(&f).(sync.Locker); ok {
			_l.Lock()
			defer _l.Unlock()
		} else if _l, ok := any(f).(sync.Locker); ok {
			_l.Lock()
			defer _l.Unlock()
		}
		f.WriteString("Hello from Zinc!\n")
		f.WriteString("Resources are managed automatically.\n")
	}
	fmt.Println(func() string {
		b, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		return string(b)
	}())
	counter := 0
	{
		mu := sync.Mutex{}
		if _c, ok := any(mu).(io.Closer); ok {
			defer _c.Close()
		}
		if _l, ok := any(&mu).(sync.Locker); ok {
			_l.Lock()
			defer _l.Unlock()
		} else if _l, ok := any(mu).(sync.Locker); ok {
			_l.Lock()
			defer _l.Unlock()
		}
		counter += 1
		counter += 10
	}
	fmt.Println(counter)
	{
		f2, _err1 := os.Open("/nonexistent/file.txt")
		if _err1 != nil {
			err := _err1.Error()
			_ = err
			fmt.Println(fmt.Sprintf("Caught error: %v", err))
			os.Exit(0)
		}
		if _c, ok := any(f2).(io.Closer); ok {
			defer _c.Close()
		}
		if _l, ok := any(&f2).(sync.Locker); ok {
			_l.Lock()
			defer _l.Unlock()
		} else if _l, ok := any(f2).(sync.Locker); ok {
			_l.Lock()
			defer _l.Unlock()
		}
		fmt.Println("should not reach here")
	}
	p1 := (os.TempDir() + "/zinc_multi_a.txt")
	p2 := (os.TempDir() + "/zinc_multi_b.txt")
	{
		f1, _err2 := os.Create(p1)
		if _err2 != nil {
			panic(_err2)
		}
		if _c, ok := any(f1).(io.Closer); ok {
			defer _c.Close()
		}
		if _l, ok := any(&f1).(sync.Locker); ok {
			_l.Lock()
			defer _l.Unlock()
		} else if _l, ok := any(f1).(sync.Locker); ok {
			_l.Lock()
			defer _l.Unlock()
		}
		f2, _err3 := os.Create(p2)
		if _err3 != nil {
			panic(_err3)
		}
		if _c, ok := any(f2).(io.Closer); ok {
			defer _c.Close()
		}
		if _l, ok := any(&f2).(sync.Locker); ok {
			_l.Lock()
			defer _l.Unlock()
		} else if _l, ok := any(f2).(sync.Locker); ok {
			_l.Lock()
			defer _l.Unlock()
		}
		f1.WriteString("File A")
		f2.WriteString("File B")
	}
	fmt.Println(func() string {
		b, err := os.ReadFile(p1)
		if err != nil {
			panic(err)
		}
		return string(b)
	}())
	fmt.Println(func() string {
		b, err := os.ReadFile(p2)
		if err != nil {
			panic(err)
		}
		return string(b)
	}())
	os.Remove(path)
	os.Remove(p1)
	os.Remove(p2)
}
