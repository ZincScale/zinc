package main

import (
	"fmt"
)

//line examples/concurrency.zn:1
func main() {
//line examples/concurrency.zn:2
	ch := make(chan string, 2)
//line examples/concurrency.zn:4
	go func() {
//line examples/concurrency.zn:5
		ch <- "hello from goroutine"
//line examples/concurrency.zn:6
		ch <- "second message"
	}()
//line examples/concurrency.zn:9
	msg1 := <-ch
//line examples/concurrency.zn:10
	msg2 := <-ch
//line examples/concurrency.zn:11
	fmt.Println(msg1)
//line examples/concurrency.zn:12
	fmt.Println(msg2)
}
