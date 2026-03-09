package main

import (
	"fmt"
)

func main() {
	ch := make(chan string, 2)
	go func() {
		ch <- "hello from goroutine"
		ch <- "second message"
	}()
	msg1 := <-ch
	msg2 := <-ch
	fmt.Println(msg1)
	fmt.Println(msg2)
}
