# Elixir re-validation: GenServer + Supervisor self-heal (same OTP underneath as the Erlang run).

defmodule Counter do
  use GenServer
  def start_link(_), do: GenServer.start_link(__MODULE__, 0, name: :counter)
  def incr, do: GenServer.cast(:counter, :incr)
  def get, do: GenServer.call(:counter, :get)
  def boom, do: GenServer.cast(:counter, :boom)

  @impl true
  def init(n), do: {:ok, n}
  @impl true
  def handle_call(:get, _from, n), do: {:reply, n, n}
  @impl true
  def handle_cast(:incr, n), do: {:noreply, n + 1}
  def handle_cast(:boom, _n), do: raise("intentional crash")
end

defmodule Sup do
  use Supervisor
  def start_link, do: Supervisor.start_link(__MODULE__, :ok, name: :sup)

  @impl true
  def init(:ok) do
    children = [%{id: :counter, start: {Counter, :start_link, [[]]}, restart: :permanent}]
    Supervisor.init(children, strategy: :one_for_one, max_restarts: 1_000_000, max_seconds: 3600)
  end
end

:logger.set_primary_config(:level, :none)

{:ok, _sup} = Sup.start_link()
p1 = Process.whereis(:counter)
Counter.incr()
Counter.incr()
Counter.incr()
IO.puts("  before crash : pid=#{inspect(p1)}  count=#{Counter.get()}")

settle = fn settle, old ->
  case Process.whereis(:counter) do
    pid when is_pid(pid) and pid != old -> :ok
    _ -> Process.sleep(1); settle.(settle, old)
  end
end

Counter.boom()
settle.(settle, p1)

p2 = Process.whereis(:counter)
IO.puts("  after  crash : pid=#{inspect(p2)}  count=#{Counter.get()}  (auto-restarted; state reset)")
healed = is_pid(p2) and p2 != p1 and Process.alive?(p2)
IO.puts("\n  SELF-HEAL #{if healed, do: "PASS", else: "FAIL"} : new pid, serving again, zero intervention")
Counter.incr()
IO.puts("  post-heal incr -> count=#{Counter.get()}")

# restart-latency benchmark
wait = fn wait, old ->
  case Process.whereis(:counter) do
    pid when is_pid(pid) and pid != old -> :ok
    _ -> wait.(wait, old)
  end
end

one_restart = fn ->
  old = Process.whereis(:counter)
  t0 = System.monotonic_time(:microsecond)
  Counter.boom()
  wait.(wait, old)
  System.monotonic_time(:microsecond) - t0
end

reps = 500
times = for _ <- 1..reps, do: one_restart.()
avg = Enum.sum(times) / reps
IO.puts("\n  supervisor restart latency : avg #{Float.round(avg, 1)} us  (min #{Enum.min(times)} / max #{Enum.max(times)} us) over #{reps} crashes")

{ts, _} = :timer.tc(fn -> Enum.each(1..1_000_000, fn _ -> spawn(fn -> :ok end) end) end)
IO.puts("  raw process spawn : 1000000 procs in #{ts} us -> #{Float.round(ts / 1_000_000, 4)} us/proc")
