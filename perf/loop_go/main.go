package main

import (
	"fmt"
	"time"
)

func main() {
	n := 1000000
	data := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		data = append(data, int64(i))
	}

	// Warm caches.
	var warm int64
	for _, x := range data {
		warm += x
	}

	start := time.Now()
	var sum int64
	for _, x := range data {
		if x%2 == 0 {
			sum += x * x
		}
	}
	elapsed := time.Since(start)

	fmt.Printf("go loop: %d ints in %s sum=%d\n", n, elapsed, sum)
}
