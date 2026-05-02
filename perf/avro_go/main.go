// Hand-rolled Avro round-trip — same workload as ../avro_zinc/src/main.zn.
package main

import (
	"fmt"
	"time"

	hambaAvro "github.com/hamba/avro/v2"
)

type Record struct {
	ID    int64   `avro:"id"`
	Name  string  `avro:"name"`
	Value float64 `avro:"value"`
}

func main() {
	schemaJson := `{"type":"record","name":"R","fields":[{"name":"id","type":"long"},{"name":"name","type":"string"},{"name":"value","type":"double"}]}`
	schema, err := hambaAvro.Parse(schemaJson)
	if err != nil {
		fmt.Println("schema parse failed:", err)
		return
	}

	n := 10000
	records := make([]Record, 0, n)
	for i := 0; i < n; i++ {
		records = append(records, Record{ID: int64(i), Name: fmt.Sprintf("name-%d", i), Value: float64(i) * 1.5})
	}

	start := time.Now()
	for _, rec := range records {
		bytes, err := hambaAvro.Marshal(schema, rec)
		if err != nil {
			fmt.Println("marshal failed:", err)
			return
		}
		var got Record
		if err := hambaAvro.Unmarshal(schema, bytes, &got); err != nil {
			fmt.Println("unmarshal failed:", err)
			return
		}
	}
	elapsed := time.Since(start)

	fmt.Printf("go avro round-trip: %d records in %s\n", n, elapsed)
}
