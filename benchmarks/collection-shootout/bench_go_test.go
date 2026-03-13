// Copyright 2026 Zinc Contributors
// Licensed under the Apache License, Version 2.0

package main

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"testing"
)

// ─── Data Setup ─────────────────────────────────────────────────────────────

const N = 1_000_000

var (
	data    []int
	dataF64 []float64
	mapData map[int]int
)

func init() {
	data = make([]int, N)
	dataF64 = make([]float64, N)
	for i := range data {
		data[i] = rand.IntN(1000)
		dataF64[i] = float64(data[i])
	}
	mapData = make(map[int]int, N/10)
	for i := 0; i < N/10; i++ {
		mapData[i] = rand.IntN(1000)
	}
}

// ─── Filter Benchmarks ─────────────────────────────────────────────────────

// Where(x > 500) — Zinc loop fusion style
func BenchmarkWhere(b *testing.B) {
	for b.Loop() {
		result := data[:0:0]
		for _, v := range data {
			if v > 500 {
				result = append(result, v)
			}
		}
		_ = result
	}
}

// Where + Select chain (fused into single loop)
func BenchmarkWhereSelect(b *testing.B) {
	for b.Loop() {
		result := data[:0:0]
		for _, v := range data {
			if v > 500 {
				result = append(result, v*2)
			}
		}
		_ = result
	}
}

// Distinct — remove duplicates preserving order
func BenchmarkDistinct(b *testing.B) {
	for b.Loop() {
		result := data[:0:0]
		seen := make(map[int]bool)
		for _, v := range data {
			if !seen[v] {
				seen[v] = true
				result = append(result, v)
			}
		}
		_ = result
	}
}

// ─── Transform Benchmarks ───────────────────────────────────────────────────

// Select(x * x)
func BenchmarkSelect(b *testing.B) {
	for b.Loop() {
		result := make([]int, len(data))
		for i, v := range data {
			result[i] = v * v
		}
		_ = result
	}
}

// SelectMany — flatten nested lists
func BenchmarkSelectMany(b *testing.B) {
	// Create nested: 100 sublists of 100 elements
	nested := make([][]int, 100)
	for i := range nested {
		nested[i] = data[i*100 : (i+1)*100]
	}
	b.ResetTimer()
	for b.Loop() {
		var result []int
		for _, sub := range nested {
			result = append(result, sub...)
		}
		_ = result
	}
}

// ─── Partition Benchmarks ───────────────────────────────────────────────────

// Take(10)
func BenchmarkTake(b *testing.B) {
	for b.Loop() {
		result := data[:0:0]
		count := 0
		for _, v := range data {
			if count >= 10 {
				break
			}
			result = append(result, v)
			count++
		}
		_ = result
	}
}

// ─── Quantifier Benchmarks ──────────────────────────────────────────────────

// Any(x > 999)
func BenchmarkAny(b *testing.B) {
	for b.Loop() {
		found := false
		for _, v := range data {
			if v > 999 {
				found = true
				break
			}
		}
		_ = found
	}
}

// All(x >= 0)
func BenchmarkAll(b *testing.B) {
	for b.Loop() {
		all := true
		for _, v := range data {
			if !(v >= 0) {
				all = false
				break
			}
		}
		_ = all
	}
}

// Where + Count
func BenchmarkWhereCount(b *testing.B) {
	for b.Loop() {
		count := 0
		for _, v := range data {
			if v > 500 {
				count++
			}
		}
		_ = count
	}
}

// First(x > 990)
func BenchmarkFirst(b *testing.B) {
	for b.Loop() {
		var result int
		for _, v := range data {
			if v > 990 {
				result = v
				break
			}
		}
		_ = result
	}
}

// Last(x > 990)
func BenchmarkLast(b *testing.B) {
	for b.Loop() {
		var result int
		for _, v := range data {
			if v > 990 {
				result = v
			}
		}
		_ = result
	}
}

// ─── Aggregate Benchmarks ───────────────────────────────────────────────────

// Sum
func BenchmarkSum(b *testing.B) {
	for b.Loop() {
		total := 0
		for _, v := range data {
			total += v
		}
		_ = total
	}
}

// Min + Max
func BenchmarkMinMax(b *testing.B) {
	for b.Loop() {
		lo, hi := data[0], data[0]
		for _, v := range data[1:] {
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
		_, _ = lo, hi
	}
}

// Aggregate(0, (acc, x) => acc + x) — reduce
func BenchmarkAggregate(b *testing.B) {
	for b.Loop() {
		acc := 0
		for _, v := range data {
			acc = acc + v
		}
		_ = acc
	}
}

// Sum(x * x) — sum with selector
func BenchmarkSumSelector(b *testing.B) {
	for b.Loop() {
		total := 0
		for _, v := range data {
			total += v * v
		}
		_ = total
	}
}

// ─── Sort Benchmarks ────────────────────────────────────────────────────────

// OrderBy — sort ascending
func BenchmarkOrderBy(b *testing.B) {
	for b.Loop() {
		sorted := make([]int, len(data))
		copy(sorted, data)
		sort.Ints(sorted)
		_ = sorted
	}
}

// Where + OrderBy + Select + Take (chain with materialization point)
func BenchmarkWhereOrderBySelectTake(b *testing.B) {
	for b.Loop() {
		// Segment 1: Where (fused)
		filtered := data[:0:0]
		for _, v := range data {
			if v > 500 {
				filtered = append(filtered, v)
			}
		}
		// Materialization: OrderBy
		sort.Ints(filtered)
		// Segment 2: Select + Take (fused)
		result := filtered[:0:0]
		count := 0
		for _, v := range filtered {
			if count >= 3 {
				break
			}
			result = append(result, v*10)
			count++
		}
		_ = result
	}
}

// ─── Group Benchmarks ───────────────────────────────────────────────────────

// GroupBy(x % 10)
func BenchmarkGroupBy(b *testing.B) {
	for b.Loop() {
		groups := make(map[int][]int)
		for _, v := range data {
			k := v % 10
			groups[k] = append(groups[k], v)
		}
		_ = groups
	}
}

// ToDictionary(x => x, x => x*x) on first 10000
func BenchmarkToDictionary(b *testing.B) {
	small := data[:10000]
	b.ResetTimer()
	for b.Loop() {
		dict := make(map[int]int)
		for _, v := range small {
			dict[v] = v * v
		}
		_ = dict
	}
}

// ─── Combine Benchmarks ─────────────────────────────────────────────────────

// Zip(a, b, (x, y) => x + y)
func BenchmarkZip(b *testing.B) {
	a := data
	bSlice := make([]int, N)
	for i := range bSlice {
		bSlice[i] = rand.IntN(1000)
	}
	b.ResetTimer()
	for b.Loop() {
		result := make([]int, len(a))
		for i := 0; i < len(a) && i < len(bSlice); i++ {
			result[i] = a[i] + bSlice[i]
		}
		_ = result
	}
}

// ─── Map Benchmarks ─────────────────────────────────────────────────────────

// Map.Where(v > 500)
func BenchmarkMapWhere(b *testing.B) {
	for b.Loop() {
		result := make(map[int]int)
		for k, v := range mapData {
			if v > 500 {
				result[k] = v
			}
		}
		_ = result
	}
}

// Map.Aggregate(0, (acc, k, v) => acc + v)
func BenchmarkMapAggregate(b *testing.B) {
	for b.Loop() {
		acc := 0
		for _, v := range mapData {
			acc += v
		}
		_ = acc
	}
}

// Map.SelectValues(v * 2)
func BenchmarkMapSelectValues(b *testing.B) {
	for b.Loop() {
		result := make(map[int]int)
		for k, v := range mapData {
			result[k] = v * 2
		}
		_ = result
	}
}

// ─── Complex Chain Benchmark ────────────────────────────────────────────────

// Where → OrderBy → Select → Take → Sum (multi-segment fused chain)
func BenchmarkComplexChain(b *testing.B) {
	for b.Loop() {
		// Segment 1: Where
		filtered := data[:0:0]
		for _, v := range data {
			if v > 200 {
				filtered = append(filtered, v)
			}
		}
		// Materialization: OrderBy
		sort.Ints(filtered)
		// Segment 2: Select → Take → Sum (fused terminal)
		total := 0
		count := 0
		for _, v := range filtered {
			if count >= 100 {
				break
			}
			total += v * v
			count++
		}
		_ = total
	}
}

// ─── Print results helper ───────────────────────────────────────────────────

func TestPrintBenchmarkInfo(t *testing.T) {
	fmt.Printf("Go Loop Fusion Benchmark — N = %d elements\n", N)
	fmt.Printf("Run: go test -bench=. -benchmem -count=5\n")
}
