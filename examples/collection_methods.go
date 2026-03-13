//go:build ignore

package main

import (
	"fmt"
	"sort"
)

//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:4
func main() {
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:7
	nums := []int{5, 3, 8, 1, 9, 2, 7, 4, 6}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:10
	big := nums[:0:0]
	for _, _v0 := range nums {
		if _v0 > 5 {
			big = append(big, _v0)
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:11
	fmt.Println(big)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:14
	doubled := nums[:0:0]
	for _, _v1 := range nums {
		_v2 := (_v1 * 2)
		doubled = append(doubled, _v2)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:15
	fmt.Println(doubled)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:18
	result := nums[:0:0]
	for _, _v3 := range nums {
		if _v3 > 3 {
			_v4 := (_v3 * 10)
			result = append(result, _v4)
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:19
	fmt.Println(result)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:22
	first3 := nums[:0:0]
	_take6 := 0
	for _, _v5 := range nums {
		if _take6 >= 3 {
			break
		}
		first3 = append(first3, _v5)
		_take6++
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:23
	fmt.Println(first3)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:24
	rest := nums[:0:0]
	_skip8 := 0
	for _, _v7 := range nums {
		if _skip8 < 3 {
			_skip8++
			continue
		}
		rest = append(rest, _v7)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:25
	fmt.Println(rest)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:28
	ascending := []int{1, 2, 3, 5, 4, 3}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:29
	taken := ascending[:0:0]
	for _, _v9 := range ascending {
		if !(_v9 <= 3) {
			break
		}
		taken = append(taken, _v9)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:30
	fmt.Println(taken)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:31
	skipped := ascending[:0:0]
	_skipping11 := true
	for _, _v10 := range ascending {
		if _skipping11 {
			if _v10 <= 3 {
				continue
			}
			_skipping11 = false
		}
		skipped = append(skipped, _v10)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:32
	fmt.Println(skipped)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:35
	hasNeg := false
	for _, _v12 := range nums {
		if _v12 < 0 {
			hasNeg = true
			break
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:36
	fmt.Println(hasNeg)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:37
	allPos := true
	for _, _v13 := range nums {
		if !(_v13 > 0) {
			allPos = false
			break
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:38
	fmt.Println(allPos)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:41
	bigCount := 0
	for _, _v14 := range nums {
		if _v14 > 5 {
			bigCount++
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:42
	fmt.Println(bigCount)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:45
	total := 0
	for _, _v15 := range nums {
		total += _v15
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:46
	fmt.Println(total)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:49
	prices := []int{10, 25, 15}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:50
	totalDouble := 0
	for _, _v16 := range prices {
		_v17 := (_v16 * 2)
		totalDouble += _v17
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:51
	fmt.Println(totalDouble)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:54
	lo := nums[0]
	_first19 := true
	for _, _v18 := range nums {
		if _first19 || _v18 < lo {
			lo = _v18
			_first19 = false
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:55
	hi := nums[0]
	_first21 := true
	for _, _v20 := range nums {
		if _first21 || _v20 > hi {
			hi = _v20
			_first21 = false
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:56
	fmt.Println(lo)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:57
	fmt.Println(hi)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:60
	sum := 0
	for _, _v22 := range nums {
		sum = (sum + _v22)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:61
	fmt.Println(sum)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:64
	dupes := []int{1, 2, 3, 2, 1, 4, 3}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:65
	unique := dupes[:0:0]
	_seen24 := make(map[interface{}]bool)
	for _, _v23 := range dupes {
		if _seen24[_v23] {
			continue
		}
		_seen24[_v23] = true
		unique = append(unique, _v23)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:66
	fmt.Println(unique)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:69
	var lastBig interface{}
	_found26 := false
	for _, _v25 := range nums {
		if _v25 > 5 {
			lastBig = _v25
			_found26 = true
		}
	}
	if !_found26 {
		panic(fmt.Errorf("no matching element found"))
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:70
	fmt.Println(lastBig)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:73
	nested := [][]int{[]int{1, 2}, []int{3, 4}, []int{5}}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:74
	var flat []interface{}
	for _, _v27 := range nested {
		for _, _v28 := range _v27 {
			flat = append(flat, _v28)
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:75
	fmt.Println(flat)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:78
	sorted := append(nums[:0:0], nums...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:79
	fmt.Println(sorted)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:81
	sortedDesc := append(nums[:0:0], nums...)
	sort.Slice(sortedDesc, func(i, j int) bool {
		return sortedDesc[i] > sortedDesc[j]
	})
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:82
	fmt.Println(sortedDesc)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:85
	_seg29 := nums[:0:0]
	for _, _v30 := range nums {
		if _v30 > 2 {
			_seg29 = append(_seg29, _v30)
		}
	}
	sort.Slice(_seg29, func(i, j int) bool {
		return _seg29[i] < _seg29[j]
	})
	top3 := _seg29[:0:0]
	_take32 := 0
	for _, _v31 := range _seg29 {
		_v33 := (_v31 * 10)
		if _take32 >= 3 {
			break
		}
		top3 = append(top3, _v33)
		_take32++
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:86
	fmt.Println(top3)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:89
	numbers := []int{1, 2, 3, 4, 5, 6}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:90
	groups := make(map[interface{}][]interface{})
	for _, _v34 := range numbers {
		_k35 := (_v34 % 2)
		groups[_k35] = append(groups[_k35], _v34)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:91
	fmt.Println(len(groups))
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:94
	items := []int{1, 2, 3}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:95
	dict := make(map[interface{}]interface{})
	for _, _v36 := range items {
		dict[_v36] = (_v36 * _v36)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:96
	fmt.Println(dict[2])
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:99
	a := []int{1, 2, 3}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:100
	b := []int{10, 20, 30}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:101
	var zipped []interface{}
	for _i37 := 0; _i37 < len(a) && _i37 < len(b); _i37++ {
		zipped = append(zipped, (a[_i37] + b[_i37]))
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:102
	fmt.Println(zipped)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:105
	fmt.Println("--- ForEach ---")
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:106
	for _, _v38 := range nums {
		if _v38 > 7 {
			x := _v38
			fmt.Println(x)
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:110
	scores := map[string]int{"Alice": 90, "Bob": 60, "Carol": 85}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:113
	passing := make(map[interface{}]interface{})
	for _k39, _v40 := range scores {
		_ = _k39
		_ = _v40
		if _v40 >= 80 {
			passing[_k39] = _v40
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:114
	fmt.Println(len(passing))
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:117
	curved := make(map[interface{}]interface{})
	for _k41, _v42 := range scores {
		_ = _k41
		_ = _v42
		_v43 := (_v42 + 10)
		curved[_k41] = _v43
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:118
	fmt.Println(curved["Bob"])
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:121
	var names []interface{}
	for _k44, _v45 := range scores {
		_ = _k44
		_ = _v45
		names = append(names, _k44)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:122
	fmt.Println(len(names))
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:125
	hasHigh := false
	for _k46, _v47 := range scores {
		if _v47 > 85 {
			hasHigh = true
			break
		}
		_ = _k46
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:126
	fmt.Println(hasHigh)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:127
	allPass := true
	for _k48, _v49 := range scores {
		if !(_v49 >= 60) {
			allPass = false
			break
		}
		_ = _k48
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:128
	fmt.Println(allPass)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:131
	highCount := 0
	for _k50, _v51 := range scores {
		if _v51 >= 80 {
			highCount++
			_ = _k50
			_ = _v51
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:132
	fmt.Println(highCount)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:135
	totalScore := 0
	for _k52, _v53 := range scores {
		_ = _k52
		_ = _v53
		totalScore = (totalScore + _v53)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:136
	fmt.Println(totalScore)
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:139
	fmt.Println("--- Map ForEach ---")
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:140
	for _k54, _v55 := range scores {
		_ = _k54
		_ = _v55
		k := _k54
		_ = k
		v := _v55
		_ = v
		fmt.Println(k)
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:143
	passingTotal := 0
	for _k56, _v57 := range scores {
		_ = _k56
		_ = _v57
		if _v57 >= 80 {
			passingTotal = (passingTotal + _v57)
		}
	}
//line /home/vrjoshi/proj/zinc/examples/collection_methods.zn:144
	fmt.Println(passingTotal)
}
