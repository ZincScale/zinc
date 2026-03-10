//go:build ignore

package main

import (
	"fmt"
	"sort"
	"strings"
)

//line examples/collections.zn:3
func main() {
//line examples/collections.zn:5
	nums := []int{10, 20, 30, 40, 50}
//line examples/collections.zn:6
	names := []string{"Alice", "Bob", "Charlie"}
//line examples/collections.zn:9
	nums = append(nums, 60)
//line examples/collections.zn:10
	fmt.Println(len(nums))
//line examples/collections.zn:12
	copy := append(nums[:0:0], nums...)
//line examples/collections.zn:13
	sort.Slice(copy, func(i, j int) bool { return copy[i] < copy[j] })
//line examples/collections.zn:14
	fmt.Println(copy)
//line examples/collections.zn:17
	fmt.Println(strings.Join(names, ", "))
//line examples/collections.zn:20
	fmt.Println(nums[1:3])
//line examples/collections.zn:21
	fmt.Println(nums[2:])
//line examples/collections.zn:22
	fmt.Println(nums[:3])
//line examples/collections.zn:25
	fmt.Println(names[0:2])
//line examples/collections.zn:26
	fmt.Println(names[1:])
//line examples/collections.zn:29
	firstTwo := nums[:2]
//line examples/collections.zn:30
	lastThree := names[1:]
//line examples/collections.zn:31
	fmt.Println(firstTwo)
//line examples/collections.zn:32
	fmt.Println(lastThree)
//line examples/collections.zn:35
	greeting := "Hello, Zinc!"
//line examples/collections.zn:36
	fmt.Println(greeting[0:5])
//line examples/collections.zn:37
	fmt.Println(greeting[7:])
//line examples/collections.zn:38
	word := greeting[7:11]
//line examples/collections.zn:39
	fmt.Println(word)
//line examples/collections.zn:42
	scores := map[string]int{"math": 95, "science": 88, "english": 92}
//line examples/collections.zn:43
	fmt.Println(scores["math"])
//line examples/collections.zn:46
	fmt.Println(func() []interface{} {
		_keys := make([]interface{}, 0, len(scores))
		for _k := range scores {
			_keys = append(_keys, _k)
		}
		return _keys
	}())
//line examples/collections.zn:47
	fmt.Println(func() []interface{} {
		_vals := make([]interface{}, 0, len(scores))
		for _, _v := range scores {
			_vals = append(_vals, _v)
		}
		return _vals
	}())
//line examples/collections.zn:48
	fmt.Println(func() bool { _, _ok := scores["math"]; return _ok }())
//line examples/collections.zn:49
	fmt.Println(func() bool { _, _ok := scores["history"]; return _ok }())
//line examples/collections.zn:50
	delete(scores, "english")
//line examples/collections.zn:51
	fmt.Println(len(scores))
//line examples/collections.zn:54
	for _, name := range names {
//line examples/collections.zn:55
		fmt.Println(fmt.Sprintf("Hello, %v!", name))
	}
//line examples/collections.zn:59
	for i, name := range names {
//line examples/collections.zn:60
		fmt.Println(fmt.Sprintf("%v: %v", i, name))
	}
//line examples/collections.zn:64
	for subject, score := range scores {
//line examples/collections.zn:65
		fmt.Println(fmt.Sprintf("%v = %v", subject, score))
	}
//line examples/collections.zn:69
	emptyList := []int{}
//line examples/collections.zn:70
	emptyMap := map[string]bool{}
//line examples/collections.zn:71
	emptyList = append(emptyList, 1)
//line examples/collections.zn:72
	emptyMap["yes"] = true
//line examples/collections.zn:73
	fmt.Println(len(emptyList))
//line examples/collections.zn:74
	fmt.Println(len(emptyMap))
}
