# Hand-translated from zinc-crystal/phase0/zn/error_explicit.zn.
# Phase 0 spike — proves the `(T, error)` thrower lowering picks
# Crystal exceptions (PLAN §4.6 Option A): the trailing `error` slot
# disappears from the Crystal signature, returns become raises, and
# `or { }` at the call site lowers to `begin/rescue err : Exception`.

# MISMATCH: zinc's `pub String Error()` method maps to Crystal's
# lowercase-first-char rule. Crystal codegen must lowercase leading
# capitals in method names — `Error()` → `error_string` (rename to
# avoid conflict with Crystal's reserved-feeling identifier `error`).
class BaseError < Exception
  def initialize(msg : String)
    super(msg)
  end

  def error_string : String
    message || ""
  end
end

class ParseError < BaseError
  def initialize(msg : String)
    super(msg)
  end
end

# `pub (Int, error) parseNum(s)` → `: Int32` + raise on error path.
def parse_num(s : String) : Int32
  if s == ""
    raise ParseError.new("empty input")
  end
  42
end

# `pub (Int, String, error) lookup(key)` → `: Tuple(Int32, String)` + raise.
def lookup(key : String) : Tuple(Int32, String)
  if key == ""
    raise ParseError.new("missing key")
  end
  {7, "found"}
end

# `pub error validate(input)` → bare `: Nil` + raise.
def validate(input : String) : Nil
  if input == "bad"
    raise ParseError.new("bad input")
  end
end

def main : Nil
  ok = begin
    parse_num("hello")
  rescue err : Exception
    puts "caught: #{err.message}"
    return
  end
  puts "ok: #{ok}"

  fail = begin
    parse_num("")
  rescue err : Exception
    puts "caught: #{err.message}"
    return
  end
  puts "should not reach: #{fail}"
end

main
