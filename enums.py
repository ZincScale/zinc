import enum

class Direction(enum.Enum):
    North = 1
    South = 2
    East = 3
    West = 4

class Color(enum.Enum):
    Red = 1
    Green = 2
    Blue = 3

def describe_direction(d: Direction) -> str:
    match d:
        case Direction.North:
            return "heading north"
        case Direction.South:
            return "heading south"
        case Direction.East:
            return "heading east"
        case Direction.West:
            return "heading west"
    return "unknown"

def move(x: int, y: int, d: Direction):
    match d:
        case Direction.North:
            y = (y + 1)
        case Direction.South:
            y = (y - 1)
        case Direction.East:
            x = (x + 1)
        case Direction.West:
            x = (x - 1)
    print(f"moved {d}: now at ({x}, {y})")

print(describe_direction(Direction.North))
move(0, 0, Direction.East)
move(1, 0, Direction.North)
