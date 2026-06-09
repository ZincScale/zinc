# Elixir re-validation: confirm early-return via throw/catch behaves & costs the same as Erlang.

defmodule CF do
  # early return (multi mid-fn) -> try/throw/catch
  def classify(x) do
    try do
      if x < 0, do: throw({:ret, :neg})
      if x == 0, do: throw({:ret, :zero})
      :pos
    catch
      {:ret, v} -> v
    end
  end

  # mutable accumulator loop -> tail recursion
  def sum_loop(list), do: sum_loop(list, 0)
  defp sum_loop([], acc), do: acc
  defp sum_loop([h | t], acc), do: sum_loop(t, acc + h)

  # early exit search, recursive normal return (baseline)
  def search_match([x | _], x), do: {:found, x}
  def search_match([_ | t], x), do: search_match(t, x)
  def search_match([], _), do: :notfound

  # SAME loop shape, but exits via throw
  def search_throw(list, x) do
    try do: go(list, x), catch: ({:found, v} -> {:found, v})
  end
  defp go([x | _], x), do: throw({:found, x})
  defp go([_ | t], x), do: go(t, x)
  defp go([], _), do: :notfound

  # Enum.each + throw (the closure-per-element shape)
  def search_each(list, x) do
    try do
      Enum.each(list, fn e -> if e == x, do: throw({:found, e}) end)
      :notfound
    catch
      {:found, v} -> {:found, v}
    end
  end
end

tests = [
  {"early classify(-1)", CF.classify(-1), :neg},
  {"early classify(0)", CF.classify(0), :zero},
  {"early classify(7)", CF.classify(7), :pos},
  {"loop sum_loop", CF.sum_loop([1, 2, 3, 4, 5]), 15},
  {"search_match", CF.search_match([1, 2, 3], 3), {:found, 3}},
  {"search_throw", CF.search_throw([1, 2, 3], 3), {:found, 3}}
]

{p, f} =
  Enum.reduce(tests, {0, 0}, fn {name, got, want}, {p, f} ->
    if got == want do
      IO.puts("  PASS  #{name}")
      {p + 1, f}
    else
      IO.puts("  FAIL  #{name}  got=#{inspect(got)} want=#{inspect(want)}")
      {p, f + 1}
    end
  end)

IO.puts("\n  elixir control_flow: #{p} passed, #{f} failed\n")

# throw-cost isolation (mirror of the Erlang bench)
big = Enum.to_list(1..1_000_000)
target = 999_999
reps = 300
rep = fn fun, n -> Enum.each(1..n, fn _ -> fun.() end) end

{t1, _} = :timer.tc(fn -> rep.(fn -> CF.search_match(big, target) end, reps) end)
{t2, _} = :timer.tc(fn -> rep.(fn -> CF.search_throw(big, target) end, reps) end)
{t3, _} = :timer.tc(fn -> rep.(fn -> CF.search_each(big, target) end, reps) end)

IO.puts("Isolating throw cost (1M-elem search x #{reps} reps):")
IO.puts("  (A) recursive, normal return : #{Float.round(t1 / 1000, 1)} ms  (baseline)")
IO.puts("  (B) recursive, EXIT VIA THROW: #{Float.round(t2 / 1000, 1)} ms  #{Float.round(t2 / t1, 2)}x vs A")
IO.puts("  (C) Enum.each + throw        : #{Float.round(t3 / 1000, 1)} ms  #{Float.round(t3 / t1, 2)}x vs A")
