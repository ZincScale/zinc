//go:build ignore

package main

import (
	"fmt"
)

//line examples/enums.zn:1
type Direction int

const (
	DirectionNorth Direction = iota
	DirectionSouth
	DirectionEast
	DirectionWest
)

//line examples/enums.zn:3
func describe(d Direction) string {
//line examples/enums.zn:4
	switch d {
	case DirectionNorth:
//line examples/enums.zn:5
		return "Going North"
	case DirectionSouth:
//line examples/enums.zn:6
		return "Going South"
	case DirectionEast:
//line examples/enums.zn:7
		return "Going East"
	case DirectionWest:
//line examples/enums.zn:8
		return "Going West"
	default:
//line examples/enums.zn:9
		return "Unknown direction"
	}
}

//line examples/enums.zn:13
func main() {
//line examples/enums.zn:14
	dir := Direction(DirectionNorth)
//line examples/enums.zn:15
	fmt.Println(describe(dir))
//line examples/enums.zn:17
	dir2 := Direction(DirectionEast)
//line examples/enums.zn:18
	fmt.Println(describe(dir2))
}
