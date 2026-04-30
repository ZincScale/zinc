# Hand-translated from zinc-crystal/phase0/zn/sealed.zn
# Phase 0 spike — sealed class lowering (Option A from PLAN §4.3),
# `match` → Crystal `case in`, data-class auto to_s with zinc's format.

abstract class Shape
end

class Shape::Circle < Shape
  getter radius : Float64

  def initialize(@radius : Float64)
  end

  def to_s(io : IO) : Nil
    io << "Circle(radius=" << fmt_num(@radius) << ")"
  end
end

class Shape::Rect < Shape
  getter width : Float64
  getter height : Float64

  def initialize(@width : Float64, @height : Float64)
  end

  def to_s(io : IO) : Nil
    io << "Rect(width=" << fmt_num(@width) << ", height=" << fmt_num(@height) << ")"
  end
end

class Shape::Triangle < Shape
  getter base : Float64
  getter height : Float64

  def initialize(@base : Float64, @height : Float64)
  end

  def to_s(io : IO) : Nil
    io << "Triangle(base=" << fmt_num(@base) << ", height=" << fmt_num(@height) << ")"
  end
end

# Number formatter that mirrors zinc-go's Println behavior:
# integer-valued floats print without ".0", others with full precision.
# This is a MISMATCH point — see phase0/MISMATCH.md.
def fmt_num(n : Float64) : String
  return n.to_i.to_s if n == n.to_i
  n.to_s
end

def area(s : Shape) : Float64
  case s
  in Shape::Circle
    r = s.radius
    3.14159 * r * r
  in Shape::Rect
    w = s.width
    h = s.height
    w * h
  in Shape::Triangle
    b = s.base
    h = s.height
    0.5 * b * h
  in Shape
    raise "unreachable: bare abstract Shape"
  end
end

def describe(s : Shape) : String
  case s
  in Shape::Circle
    r = s.radius
    "circle with radius #{fmt_num(r)}"
  in Shape::Rect
    w = s.width
    h = s.height
    "rect #{fmt_num(w)}x#{fmt_num(h)}"
  in Shape::Triangle
    b = s.base
    h = s.height
    "triangle base=#{fmt_num(b)} height=#{fmt_num(h)}"
  in Shape
    raise "unreachable: bare abstract Shape"
  end
end

c = Shape::Circle.new(5.0)
r = Shape::Rect.new(3.0, 4.0)
t = Shape::Triangle.new(6.0, 3.0)

puts c
puts r
puts t

puts "area circle: #{fmt_num(area(c))}"
puts "area rect: #{fmt_num(area(r))}"
puts "area triangle: #{fmt_num(area(t))}"

puts describe(c)
puts describe(r)
puts describe(t)

shapes : Array(Shape) = [
  Shape::Circle.new(1.0),
  Shape::Rect.new(2.0, 3.0),
  Shape::Triangle.new(4.0, 5.0),
] of Shape
shapes.each do |s|
  puts "  #{describe(s)} → area=#{fmt_num(area(s))}"
end

puts "Sealed OK"
