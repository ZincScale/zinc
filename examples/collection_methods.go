//go:build ignore

package main

import (
	"fmt"
	"sort"
)

//line examples/collection_methods.zn:4
func main() {
//line examples/collection_methods.zn:7
	nums := []int{5, 3, 8, 1, 9, 2, 7, 4, 6}
//line examples/collection_methods.zn:10
	big := nums[:0:0]
	for _, _v0 := range nums {
		if _v0 > 5 {
			big = append(big, _v0)
		}
	}
//line examples/collection_methods.zn:11
	fmt.Println(big)
//line examples/collection_methods.zn:14
	doubled := nums[:0:0]
	for _, _v1 := range nums {
		_v2 := (_v1 * 2)
		doubled = append(doubled, _v2)
	}
//line examples/collection_methods.zn:15
	fmt.Println(doubled)
//line examples/collection_methods.zn:18
	result := nums[:0:0]
	for _, _v3 := range nums {
		if _v3 > 3 {
			_v4 := (_v3 * 10)
			result = append(result, _v4)
		}
	}
//line examples/collection_methods.zn:19
	fmt.Println(result)
//line examples/collection_methods.zn:22
	first3 := nums[:0:0]
	_take6 := 0
	for _, _v5 := range nums {
		if _take6 >= 3 {
			break
		}
		first3 = append(first3, _v5)
		_take6++
	}
//line examples/collection_methods.zn:23
	fmt.Println(first3)
//line examples/collection_methods.zn:24
	rest := nums[:0:0]
	_skip8 := 0
	for _, _v7 := range nums {
		if _skip8 < 3 {
			_skip8++
			continue
		}
		rest = append(rest, _v7)
	}
//line examples/collection_methods.zn:25
	fmt.Println(rest)
//line examples/collection_methods.zn:28
	hasNeg := false
	for _, _v9 := range nums {
		if _v9 < 0 {
			hasNeg = true
			break
		}
	}
//line examples/collection_methods.zn:29
	fmt.Println(hasNeg)
//line examples/collection_methods.zn:30
	allPos := true
	for _, _v10 := range nums {
		if !(_v10 > 0) {
			allPos = false
			break
		}
	}
//line examples/collection_methods.zn:31
	fmt.Println(allPos)
//line examples/collection_methods.zn:34
	bigCount := 0
	for _, _v11 := range nums {
		if _v11 > 5 {
			bigCount++
		}
	}
//line examples/collection_methods.zn:35
	fmt.Println(bigCount)
//line examples/collection_methods.zn:38
	total := 0
	for _, _v12 := range nums {
		total += _v12
	}
//line examples/collection_methods.zn:39
	fmt.Println(total)
//line examples/collection_methods.zn:42
	prices := []int{10, 25, 15}
//line examples/collection_methods.zn:43
	totalDouble := 0
	for _, _v13 := range prices {
		_v14 := (_v13 * 2)
		totalDouble += _v14
	}
//line examples/collection_methods.zn:44
	fmt.Println(totalDouble)
//line examples/collection_methods.zn:47
	lo := nums[0]
	_first16 := true
	for _, _v15 := range nums {
		if _first16 || _v15 < lo {
			lo = _v15
			_first16 = false
		}
	}
//line examples/collection_methods.zn:48
	hi := nums[0]
	_first18 := true
	for _, _v17 := range nums {
		if _first18 || _v17 > hi {
			hi = _v17
			_first18 = false
		}
	}
//line examples/collection_methods.zn:49
	fmt.Println(lo)
//line examples/collection_methods.zn:50
	fmt.Println(hi)
//line examples/collection_methods.zn:53
	sum := 0
	for _, _v19 := range nums {
		sum = (sum + _v19)
	}
//line examples/collection_methods.zn:54
	fmt.Println(sum)
//line examples/collection_methods.zn:57
	dupes := []int{1, 2, 3, 2, 1, 4, 3}
//line examples/collection_methods.zn:58
	unique := dupes[:0:0]
	_seen21 := make(map[interface{}]bool)
	for _, _v20 := range dupes {
		if _seen21[_v20] {
			continue
		}
		_seen21[_v20] = true
		unique = append(unique, _v20)
	}
//line examples/collection_methods.zn:59
	fmt.Println(unique)
//line examples/collection_methods.zn:62
	var lastBig interface{}
	_found23 := false
	for _, _v22 := range nums {
		if _v22 > 5 {
			lastBig = _v22
			_found23 = true
		}
	}
	if !_found23 {
		panic(fmt.Errorf("no matching element found"))
	}
//line examples/collection_methods.zn:63
	fmt.Println(lastBig)
//line examples/collection_methods.zn:66
	nested := [][]int{[]int{1, 2}, []int{3, 4}, []int{5}}
//line examples/collection_methods.zn:67
	var flat []interface{}
	for _, _v24 := range nested {
		for _, _v25 := range _v24 {
			flat = append(flat, _v25)
		}
	}
//line examples/collection_methods.zn:68
	fmt.Println(flat)
//line examples/collection_methods.zn:71
	sorted := append(nums[:0:0], nums...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})
//line examples/collection_methods.zn:72
	fmt.Println(sorted)
//line examples/collection_methods.zn:74
	sortedDesc := append(nums[:0:0], nums...)
	sort.Slice(sortedDesc, func(i, j int) bool {
		return sortedDesc[i] > sortedDesc[j]
	})
//line examples/collection_methods.zn:75
	fmt.Println(sortedDesc)
//line examples/collection_methods.zn:78
	_seg26 := nums[:0:0]
	for _, _v27 := range nums {
		if _v27 > 2 {
			_seg26 = append(_seg26, _v27)
		}
	}
	sort.Slice(_seg26, func(i, j int) bool {
		return _seg26[i] < _seg26[j]
	})
	top3 := _seg26[:0:0]
	_take29 := 0
	for _, _v28 := range _seg26 {
		_v30 := (_v28 * 10)
		if _take29 >= 3 {
			break
		}
		top3 = append(top3, _v30)
		_take29++
	}
//line examples/collection_methods.zn:79
	fmt.Println(top3)
//line examples/collection_methods.zn:82
	numbers := []int{1, 2, 3, 4, 5, 6}
//line examples/collection_methods.zn:83
	groups := make(map[interface{}][]interface{})
	for _, _v31 := range numbers {
		_k32 := (_v31 % 2)
		groups[_k32] = append(groups[_k32], _v31)
	}
//line examples/collection_methods.zn:84
	fmt.Println(len(groups))
//line examples/collection_methods.zn:87
	items := []int{1, 2, 3}
//line examples/collection_methods.zn:88
	dict := make(map[interface{}]interface{})
	for _, _v33 := range items {
		dict[_v33] = (_v33 * _v33)
	}
//line examples/collection_methods.zn:89
	fmt.Println(dict[2])
//line examples/collection_methods.zn:92
	a := []int{1, 2, 3}
//line examples/collection_methods.zn:93
	b := []int{10, 20, 30}
//line examples/collection_methods.zn:94
	var zipped []interface{}
	for _i34 := 0; _i34 < len(a) && _i34 < len(b); _i34++ {
		zipped = append(zipped, (a[_i34] + b[_i34]))
	}
//line examples/collection_methods.zn:95
	fmt.Println(zipped)
//line examples/collection_methods.zn:98
	fmt.Println("--- ForEach ---")
//line examples/collection_methods.zn:99
	for _, _v35 := range nums {
		if _v35 > 7 {
			x := _v35
			fmt.Println(x)
		}
	}
//line examples/collection_methods.zn:103
	scores := map[string]int{"Alice": 90, "Bob": 60, "Carol": 85}
//line examples/collection_methods.zn:106
	passing := make(map[interface{}]interface{})
	for _k36, _v37 := range scores {
		_ = _k36
		_ = _v37
		if _v37 >= 80 {
			passing[_k36] = _v37
		}
	}
//line examples/collection_methods.zn:107
	fmt.Println(len(passing))
//line examples/collection_methods.zn:110
	curved := make(map[interface{}]interface{})
	for _k38, _v39 := range scores {
		_ = _k38
		_ = _v39
		_v40 := (_v39 + 10)
		curved[_k38] = _v40
	}
//line examples/collection_methods.zn:111
	fmt.Println(curved["Bob"])
//line examples/collection_methods.zn:114
	var names []interface{}
	for _k41, _v42 := range scores {
		_ = _k41
		_ = _v42
		names = append(names, _k41)
	}
//line examples/collection_methods.zn:115
	fmt.Println(len(names))
//line examples/collection_methods.zn:118
	hasHigh := false
	for _k43, _v44 := range scores {
		if _v44 > 85 {
			hasHigh = true
			break
		}
		_ = _k43
	}
//line examples/collection_methods.zn:119
	fmt.Println(hasHigh)
//line examples/collection_methods.zn:120
	allPass := true
	for _k45, _v46 := range scores {
		if !(_v46 >= 60) {
			allPass = false
			break
		}
		_ = _k45
	}
//line examples/collection_methods.zn:121
	fmt.Println(allPass)
//line examples/collection_methods.zn:124
	highCount := 0
	for _k47, _v48 := range scores {
		if _v48 >= 80 {
			highCount++
			_ = _k47
			_ = _v48
		}
	}
//line examples/collection_methods.zn:125
	fmt.Println(highCount)
//line examples/collection_methods.zn:128
	totalScore := 0
	for _k49, _v50 := range scores {
		_ = _k49
		_ = _v50
		totalScore = (totalScore + _v50)
	}
//line examples/collection_methods.zn:129
	fmt.Println(totalScore)
//line examples/collection_methods.zn:132
	fmt.Println("--- Map ForEach ---")
//line examples/collection_methods.zn:133
	for _k51, _v52 := range scores {
		_ = _k51
		_ = _v52
		k := _k51
		_ = k
		v := _v52
		_ = v
		fmt.Println(k)
	}
//line examples/collection_methods.zn:136
	passingTotal := 0
	for _k53, _v54 := range scores {
		_ = _k53
		_ = _v54
		if _v54 >= 80 {
			passingTotal = (passingTotal + _v54)
		}
	}
//line examples/collection_methods.zn:137
	fmt.Println(passingTotal)
}
