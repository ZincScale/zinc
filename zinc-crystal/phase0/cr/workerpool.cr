# Hand-translated from zinc-crystal/phase0/zn/workerpool.zn.
# Phase 0 spike — proves the structured-concurrency lowering works:
# `concurrent { }` → `WaitGroup.wait do |wg| ... end`,
# nested `spawn { }` → `wg.spawn do ... end`.

require "wait_group"

def main : Nil
  jobs = Channel(String).new(10)
  results = Channel(String).new(10)

  tasks : Array(String) = ["fetch", "parse", "transform", "validate", "store"]
  tasks.each do |task|
    jobs.send(task)
  end

  WaitGroup.wait do |wg|
    (0...3).each do |w|
      wg.spawn do
        loop do
          job = jobs.receive
          if job == "STOP"
            break
          end
          results.send("done: #{job}")
        end
      end
    end

    (0...3).each do |i|
      jobs.send("STOP")
    end

    wg.spawn do
      (0...5).each do |i|
        puts results.receive
      end
    end
  end

  puts "Worker Pool OK"
end

main
