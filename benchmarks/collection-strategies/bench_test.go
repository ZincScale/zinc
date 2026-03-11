package bench

import (
	"iter"
	"slices"
	"testing"
)

// ============================================================================
// Test data
// ============================================================================

func makeData(n int) []int {
	data := make([]int, n)
	for i := range data {
		data[i] = i
	}
	return data
}

var sizes = []struct {
	name string
	n    int
}{
	{"100", 100},
	{"10K", 10_000},
	{"1M", 1_000_000},
}

// ============================================================================
// Chain 1: Filter + Map  (Where + Select)
// items.Where(x => x % 3 == 0).Select(x => x * 2).ToList()
// ============================================================================

// Strategy A: Loop fusion — single fused loop
func filterMapFused(data []int) []int {
	var result []int
	for _, x := range data {
		if x%3 == 0 {
			result = append(result, x*2)
		}
	}
	return result
}

// Strategy B: Range-over-func iterators
func iterWhere(s []int, pred func(int) bool) iter.Seq[int] {
	return func(yield func(int) bool) {
		for _, v := range s {
			if pred(v) {
				if !yield(v) {
					return
				}
			}
		}
	}
}

func iterSelect(seq iter.Seq[int], f func(int) int) iter.Seq[int] {
	return func(yield func(int) bool) {
		seq(func(v int) bool {
			return yield(f(v))
		})
	}
}

func iterToList(seq iter.Seq[int]) []int {
	return slices.Collect(seq)
}

func filterMapIter(data []int) []int {
	seq := iterWhere(data, func(x int) bool { return x%3 == 0 })
	seq = iterSelect(seq, func(x int) int { return x * 2 })
	return iterToList(seq)
}

// Strategy C: Naive intermediate slices
func naiveWhere(data []int, pred func(int) bool) []int {
	var result []int
	for _, x := range data {
		if pred(x) {
			result = append(result, x)
		}
	}
	return result
}

func naiveSelect(data []int, f func(int) int) []int {
	result := make([]int, len(data))
	for i, x := range data {
		result[i] = f(x)
	}
	return result
}

func filterMapNaive(data []int) []int {
	filtered := naiveWhere(data, func(x int) bool { return x%3 == 0 })
	return naiveSelect(filtered, func(x int) int { return x * 2 })
}

// ============================================================================
// Chain 2: Filter + First (short-circuit)
// items.Where(x => x > n/2).First()
// ============================================================================

func filterFirstFused(data []int) (int, bool) {
	half := len(data) / 2
	for _, x := range data {
		if x > half {
			return x, true
		}
	}
	return 0, false
}

func iterFirst(seq iter.Seq[int]) (int, bool) {
	for v := range seq {
		return v, true
	}
	return 0, false
}

func filterFirstIter(data []int) (int, bool) {
	half := len(data) / 2
	seq := iterWhere(data, func(x int) bool { return x > half })
	return iterFirst(seq)
}

func filterFirstNaive(data []int) (int, bool) {
	half := len(data) / 2
	filtered := naiveWhere(data, func(x int) bool { return x > half })
	if len(filtered) > 0 {
		return filtered[0], true
	}
	return 0, false
}

// ============================================================================
// Chain 3: Filter + Map + Reduce (Where + Select + Aggregate)
// items.Where(x => x % 2 == 0).Select(x => x * x).Aggregate(0, (acc, x) => acc + x)
// ============================================================================

func filterMapReduceFused(data []int) int {
	acc := 0
	for _, x := range data {
		if x%2 == 0 {
			acc += x * x
		}
	}
	return acc
}

func iterAggregate(seq iter.Seq[int], init int, f func(int, int) int) int {
	acc := init
	seq(func(v int) bool {
		acc = f(acc, v)
		return true
	})
	return acc
}

func filterMapReduceIter(data []int) int {
	seq := iterWhere(data, func(x int) bool { return x%2 == 0 })
	seq = iterSelect(seq, func(x int) int { return x * x })
	return iterAggregate(seq, 0, func(acc, x int) int { return acc + x })
}

func naiveAggregate(data []int, init int, f func(int, int) int) int {
	acc := init
	for _, x := range data {
		acc = f(acc, x)
	}
	return acc
}

func filterMapReduceNaive(data []int) int {
	filtered := naiveWhere(data, func(x int) bool { return x%2 == 0 })
	mapped := naiveSelect(filtered, func(x int) int { return x * x })
	return naiveAggregate(mapped, 0, func(acc, x int) int { return acc + x })
}

// ============================================================================
// Chain 4: Take (bounded output)
// items.Where(x => x % 7 == 0).Take(10).ToList()
// ============================================================================

func filterTakeFused(data []int, n int) []int {
	var result []int
	for _, x := range data {
		if x%7 == 0 {
			result = append(result, x)
			if len(result) >= n {
				break
			}
		}
	}
	return result
}

func iterTake(seq iter.Seq[int], n int) iter.Seq[int] {
	return func(yield func(int) bool) {
		count := 0
		seq(func(v int) bool {
			if count >= n {
				return false
			}
			count++
			return yield(v)
		})
	}
}

func filterTakeIter(data []int, n int) []int {
	seq := iterWhere(data, func(x int) bool { return x%7 == 0 })
	seq = iterTake(seq, n)
	return iterToList(seq)
}

func filterTakeNaive(data []int, n int) []int {
	filtered := naiveWhere(data, func(x int) bool { return x%7 == 0 })
	if len(filtered) > n {
		filtered = filtered[:n]
	}
	return filtered
}

// ============================================================================
// Benchmarks
// ============================================================================

// --- Chain 1: Filter + Map ---

func BenchmarkFilterMap(b *testing.B) {
	for _, size := range sizes {
		data := makeData(size.n)
		b.Run("Fused/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterMapFused(data)
			}
		})
		b.Run("Iterator/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterMapIter(data)
			}
		})
		b.Run("Naive/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterMapNaive(data)
			}
		})
	}
}

// --- Chain 2: Filter + First (short-circuit) ---

func BenchmarkFilterFirst(b *testing.B) {
	for _, size := range sizes {
		data := makeData(size.n)
		b.Run("Fused/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterFirstFused(data)
			}
		})
		b.Run("Iterator/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterFirstIter(data)
			}
		})
		b.Run("Naive/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterFirstNaive(data)
			}
		})
	}
}

// --- Chain 3: Filter + Map + Reduce ---

func BenchmarkFilterMapReduce(b *testing.B) {
	for _, size := range sizes {
		data := makeData(size.n)
		b.Run("Fused/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterMapReduceFused(data)
			}
		})
		b.Run("Iterator/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterMapReduceIter(data)
			}
		})
		b.Run("Naive/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterMapReduceNaive(data)
			}
		})
	}
}

// --- Chain 4: Filter + Take ---

func BenchmarkFilterTake(b *testing.B) {
	for _, size := range sizes {
		data := makeData(size.n)
		b.Run("Fused/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterTakeFused(data, 10)
			}
		})
		b.Run("Iterator/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterTakeIter(data, 10)
			}
		})
		b.Run("Naive/"+size.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				filterTakeNaive(data, 10)
			}
		})
	}
}
