//go:build ignore

package main

import (
	"fmt"
	"sort"
	"strings"
)

func main() {
	nums := []int{10, 20, 30, 40, 50}
	names := []string{"Alice", "Bob", "Charlie"}
	nums = append(nums, 60)
	fmt.Println(len(nums))
	copy := append(nums[:0:0], nums...)
	sort.Slice(copy, func(i, j int) bool { return copy[i] < copy[j] })
	fmt.Println(copy)
	fmt.Println(strings.Join(names, ", "))
	fmt.Println(nums[1:3])
	fmt.Println(nums[2:])
	fmt.Println(nums[:3])
	fmt.Println(names[0:2])
	fmt.Println(names[1:])
	firstTwo := nums[:2]
	lastThree := names[1:]
	fmt.Println(firstTwo)
	fmt.Println(lastThree)
	greeting := "Hello, Zinc!"
	fmt.Println(greeting[0:5])
	fmt.Println(greeting[7:])
	word := greeting[7:11]
	fmt.Println(word)
	scores := map[string]int{"math": 95, "science": 88, "english": 92}
	fmt.Println(scores["math"])
	fmt.Println(func() []interface{} {
		_keys := make([]interface{}, 0, len(scores))
		for _k := range scores {
			_keys = append(_keys, _k)
		}
		return _keys
	}())
	fmt.Println(func() []interface{} {
		_vals := make([]interface{}, 0, len(scores))
		for _, _v := range scores {
			_vals = append(_vals, _v)
		}
		return _vals
	}())
	fmt.Println(func() bool { _, _ok := scores["math"]; return _ok }())
	fmt.Println(func() bool { _, _ok := scores["history"]; return _ok }())
	delete(scores, "english")
	fmt.Println(len(scores))
	for _, name := range names {
		fmt.Println(fmt.Sprintf("Hello, %v!", name))
	}
	for i, name := range names {
		fmt.Println(fmt.Sprintf("%v: %v", i, name))
	}
	for subject, score := range scores {
		fmt.Println(fmt.Sprintf("%v = %v", subject, score))
	}
	emptyList := []int{}
	emptyMap := map[string]bool{}
	emptyList = append(emptyList, 1)
	emptyMap["yes"] = true
	fmt.Println(len(emptyList))
	fmt.Println(len(emptyMap))
}
