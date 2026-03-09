package main

import (
	"fmt"
)

type Direction int

const (
	DirectionNorth Direction = iota
	DirectionSouth
	DirectionEast
	DirectionWest
)

func describe(d Direction) string {
	switch d {
	case DirectionNorth:
		return "Going North"
	case DirectionSouth:
		return "Going South"
	case DirectionEast:
		return "Going East"
	case DirectionWest:
		return "Going West"
	default:
		return "Unknown direction"
	}
}

func main() {
	dir := Direction(DirectionNorth)
	fmt.Println(describe(dir))
	dir2 := Direction(DirectionEast)
	fmt.Println(describe(dir2))
}
