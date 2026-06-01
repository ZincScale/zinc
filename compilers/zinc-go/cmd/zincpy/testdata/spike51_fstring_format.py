x = 3.14159
n = 42
big = 1234567
neg = -17
pi = 3.14159265

# float precision, width, alignment, fill
print(f"{x:.2f}")
print(f"{x:10.3f}")
print(f"{x:<10.3f}|")
print(f"{x:^12.2f}|")

# integer width, zero-pad, sign, grouping
print(f"{n:05d}")
print(f"{n:+d}")
print(f"{neg:05d}")
print(f"{big:,}")
print(f"{big:,.2f}")

# radix types
print(f"{255:x}")
print(f"{255:#x}")
print(f"{255:08b}")
print(f"{10:o}")

# percent, exponent, general
print(f"{0.5:.0%}")
print(f"{pi:.3e}")
print(f"{1234.5678:,.2f}")
print(f"{3.0:g}")
print(f"{0.0001234:g}")

# string alignment + fill
print(f"{'hi':>8}|")
print(f"{'hi':*^8}|")
print(f"{'left':<8}|")
print(f"{n:>6}")

# !r / !s conversions, with and without a spec
name = "ab"
print(f"{name!r}")
print(f"{name!r:>6}")
print(f"{x!s}")
