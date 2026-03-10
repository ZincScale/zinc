//go:build ignore

package main

import (
	"fmt"
	"io"
	"os"
	"sync"
)

//line examples/with_resources.zn:13
func main() {
//line examples/with_resources.zn:15
	path := (os.TempDir() + "/zinc_example.txt")
//line examples/with_resources.zn:17
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
//line examples/with_resources.zn:18
		if _, _err1 := f.WriteString("Hello from Zinc!\n"); _err1 != nil {
			panic(_err1)
		}
//line examples/with_resources.zn:19
		if _, _err2 := f.WriteString("Resources are managed automatically.\n"); _err2 != nil {
			panic(_err2)
		}
	}
//line examples/with_resources.zn:23
	fmt.Println(func() string {
		b, err := os.ReadFile(path)
		if err != nil {
			panic(err)
		}
		return string(b)
	}())
//line examples/with_resources.zn:26
	counter := 0
//line examples/with_resources.zn:27
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
//line examples/with_resources.zn:28
		counter += 1
//line examples/with_resources.zn:29
		counter += 10
	}
//line examples/with_resources.zn:32
	fmt.Println(counter)
//line examples/with_resources.zn:35
	{
		f2, _err3 := os.Open("/nonexistent/file.txt")
		if _err3 != nil {
			err := _err3.Error()
			_ = err
//line examples/with_resources.zn:36
			fmt.Println(fmt.Sprintf("Caught error: %v", err))
//line examples/with_resources.zn:37
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
//line examples/with_resources.zn:39
		fmt.Println("should not reach here")
	}
//line examples/with_resources.zn:43
	p1 := (os.TempDir() + "/zinc_multi_a.txt")
//line examples/with_resources.zn:44
	p2 := (os.TempDir() + "/zinc_multi_b.txt")
//line examples/with_resources.zn:46
	{
		f1, _err4 := os.Create(p1)
		if _err4 != nil {
			panic(_err4)
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
		f2, _err5 := os.Create(p2)
		if _err5 != nil {
			panic(_err5)
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
//line examples/with_resources.zn:47
		if _, _err6 := f1.WriteString("File A"); _err6 != nil {
			panic(_err6)
		}
//line examples/with_resources.zn:48
		if _, _err7 := f2.WriteString("File B"); _err7 != nil {
			panic(_err7)
		}
	}
//line examples/with_resources.zn:52
	fmt.Println(func() string {
		b, err := os.ReadFile(p1)
		if err != nil {
			panic(err)
		}
		return string(b)
	}())
//line examples/with_resources.zn:53
	fmt.Println(func() string {
		b, err := os.ReadFile(p2)
		if err != nil {
			panic(err)
		}
		return string(b)
	}())
//line examples/with_resources.zn:56
	if _err8 := os.Remove(path); _err8 != nil {
		panic(_err8)
	}
//line examples/with_resources.zn:57
	if _err9 := os.Remove(p1); _err9 != nil {
		panic(_err9)
	}
//line examples/with_resources.zn:58
	if _err10 := os.Remove(p2); _err10 != nil {
		panic(_err10)
	}
}
