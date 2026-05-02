# Zinc → Go codegen perf check

Side-by-side: same workload, run as Zinc-transpiled Go vs hand-rolled Go.
Both use the same Go libraries (e.g. `hamba/avro`). Tests whether Zinc's
codegen adds measurable overhead to the emitted code.

## Results

| Workload | Zinc | Hand-rolled Go | Notes |
|---|---|---|---|
| Avro 10k roundtrip | 2.86–3.03 ms | 2.85–3.09 ms | Within run-to-run variance. |
| Loop 1M sum-of-squares | 0.76–0.95 ms | 0.70–0.71 ms | Zinc ~7-10% slower; due to `[]int64{}` vs `make([]int64, 0, n)` (no pre-alloc on append loop). |

## Codegen quality (loop_zinc/src/main.zn → loop_zinc/zinc-out/main.go)

```go
func main() {
    n := 1000000
    data := []int64{}                  // could be make([]int64, 0, n)
    for i := 0; i < n; i++ {
        data = append(data, int64(i))
    }
    ...
    for _, x := range data {           // idiomatic Go
        if x % 2 == 0 {
            sum = sum + x * x
        }
    }
}
```

Idiomatic Go shape — `[]int64`, `for-range`, no boxing, no interface{}
indirection. The arithmetic and branching go straight through.

## How to run

```sh
bash perf/bench.sh
```
