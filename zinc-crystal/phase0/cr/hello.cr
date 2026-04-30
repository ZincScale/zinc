# Hand-translated from zinc-crystal/phase0/zn/hello.zn
# Phase 0 spike — proves the simplest mapping (print, top-level statements,
# function with String parameter, string interpolation).

def greet(name : String) : String
  "Hello, #{name}!"
end

def main : Nil
  puts greet("World")
  puts greet("Zinc")
  x : Int32 = 42
  puts "The answer is #{x}"
end

main
