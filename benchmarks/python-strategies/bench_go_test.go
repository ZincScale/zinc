// Copyright 2026 victorybhg
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package python_strategies

import (
	"fmt"
	"testing"
)

// These benchmarks mirror the Python benchmarks exactly, using Go code
// equivalent to what Zinc's Go codegen produces (fused loops).

func makeData(n int) []int {
	data := make([]int, n)
	for i := range data {
		data[i] = i + 1
	}
	return data
}

// Where+Select: nums.Where(x => x > n/2).Select(x => x * 2).ToList()
func BenchmarkGoWhereSelect_1K(b *testing.B)   { benchGoWhereSelect(b, 1_000) }
func BenchmarkGoWhereSelect_10K(b *testing.B)  { benchGoWhereSelect(b, 10_000) }
func BenchmarkGoWhereSelect_100K(b *testing.B) { benchGoWhereSelect(b, 100_000) }
func BenchmarkGoWhereSelect_1M(b *testing.B)   { benchGoWhereSelect(b, 1_000_000) }
func BenchmarkGoWhereSelect_10M(b *testing.B)  { benchGoWhereSelect(b, 10_000_000) }

func benchGoWhereSelect(b *testing.B, n int) {
	data := makeData(n)
	threshold := n / 2
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result []int
		for _, x := range data {
			if x > threshold {
				result = append(result, x*2)
			}
		}
		_ = result
	}
}

// Where+First: nums.Where(x => x > threshold).First()
func BenchmarkGoFirst_1K(b *testing.B)   { benchGoFirst(b, 1_000) }
func BenchmarkGoFirst_10K(b *testing.B)  { benchGoFirst(b, 10_000) }
func BenchmarkGoFirst_100K(b *testing.B) { benchGoFirst(b, 100_000) }
func BenchmarkGoFirst_1M(b *testing.B)   { benchGoFirst(b, 1_000_000) }
func BenchmarkGoFirst_10M(b *testing.B)  { benchGoFirst(b, 10_000_000) }

func benchGoFirst(b *testing.B, n int) {
	data := makeData(n)
	threshold := n - 10
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var first int
		for _, x := range data {
			if x > threshold {
				first = x
				break
			}
		}
		_ = first
	}
}

// Aggregate: nums.Aggregate(0, (acc, x) => acc + x)
func BenchmarkGoAggregate_1K(b *testing.B)   { benchGoAggregate(b, 1_000) }
func BenchmarkGoAggregate_10K(b *testing.B)  { benchGoAggregate(b, 10_000) }
func BenchmarkGoAggregate_100K(b *testing.B) { benchGoAggregate(b, 100_000) }
func BenchmarkGoAggregate_1M(b *testing.B)   { benchGoAggregate(b, 1_000_000) }
func BenchmarkGoAggregate_10M(b *testing.B)  { benchGoAggregate(b, 10_000_000) }

func benchGoAggregate(b *testing.B, n int) {
	data := makeData(n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acc := 0
		for _, x := range data {
			acc = acc + x
		}
		_ = acc
	}
}

// Take: nums.Where(x => x > 5).Select(x => x * 2).Take(10).ToList()
func BenchmarkGoTake_1K(b *testing.B)   { benchGoTake(b, 1_000) }
func BenchmarkGoTake_10K(b *testing.B)  { benchGoTake(b, 10_000) }
func BenchmarkGoTake_100K(b *testing.B) { benchGoTake(b, 100_000) }
func BenchmarkGoTake_1M(b *testing.B)   { benchGoTake(b, 1_000_000) }
func BenchmarkGoTake_10M(b *testing.B)  { benchGoTake(b, 10_000_000) }

func benchGoTake(b *testing.B, n int) {
	data := makeData(n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result []int
		taken := 0
		for _, x := range data {
			if taken >= 10 {
				break
			}
			if x > 5 {
				result = append(result, x*2)
				taken++
			}
		}
		_ = result
	}
}

// Complex: nums.Where(x => x%2==0).Select(x => x*x).Aggregate(0, (a,b) => a+b)
func BenchmarkGoComplex_1K(b *testing.B)   { benchGoComplex(b, 1_000) }
func BenchmarkGoComplex_10K(b *testing.B)  { benchGoComplex(b, 10_000) }
func BenchmarkGoComplex_100K(b *testing.B) { benchGoComplex(b, 100_000) }
func BenchmarkGoComplex_1M(b *testing.B)   { benchGoComplex(b, 1_000_000) }
func BenchmarkGoComplex_10M(b *testing.B)  { benchGoComplex(b, 10_000_000) }

func benchGoComplex(b *testing.B, n int) {
	data := makeData(n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		acc := 0
		for _, x := range data {
			if x%2 == 0 {
				acc = acc + x*x
			}
		}
		_ = acc
	}
}

func TestPrintGoResults(t *testing.T) {
	fmt.Println("Go benchmark results are in the Benchmark* functions above.")
	fmt.Println("Run with: go test -bench=. -benchtime=3s")
}
