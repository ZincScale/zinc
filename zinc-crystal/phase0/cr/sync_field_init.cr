# Hand-translated from zinc-crystal/phase0/zn/sync_field_init.zn.
# Phase 0 spike — proves Sync::Mutex / Sync::RWLock as class fields,
# `lock (mu) { }` → `mu.synchronize { }`, and concurrent counter
# increment via the WaitGroup-shape lowering.

require "wait_group"

class Counter
  @mu : Sync::Mutex
  @n : Int32

  def initialize
    @mu = Sync::Mutex.new
    @n = 0
  end

  def inc : Nil
    @mu.synchronize do
      @n = @n + 1
    end
  end

  def get : Int32
    @mu.synchronize do
      return @n
    end
  end
end

class Versioned
  @rw : Sync::RWLock
  @tag : String

  def initialize
    @rw = Sync::RWLock.new
    @tag = "v0"
  end

  def read : String
    @rw.read do
      return @tag
    end
    "" # unreachable; pleases the type checker
  end

  def write(t : String) : Nil
    @rw.write do
      @tag = t
    end
  end
end

def main : Nil
  c = Counter.new
  WaitGroup.wait do |wg|
    i = 0
    while i < 3
      wg.spawn do
        k = 0
        while k < 100
          c.inc
          k = k + 1
        end
      end
      i = i + 1
    end
  end
  puts "counter: " + c.get.to_s

  v = Versioned.new
  puts "initial: " + v.read
  v.write("v1")
  puts "after write: " + v.read

  puts "sync field init OK"
end

main
