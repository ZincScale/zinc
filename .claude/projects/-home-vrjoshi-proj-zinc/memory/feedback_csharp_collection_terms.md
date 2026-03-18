---
name: Use C# collection method names
description: Collection methods should use C# LINQ naming (Where, Select, First, etc.) not custom Zinc names
type: feedback
---

Use C# LINQ method names for collection operations, not custom names.

**Why:** Zinc targets C# AOT as default backend. Using familiar C# terms reduces learning curve for the target audience (enterprise C#/Java developers). Consistency with the target ecosystem.

**How to apply:** Use these names in Zinc:
- `Where` (not `filter`)
- `Select` (not `map`)
- `First` / `FirstOrDefault`
- `Any` / `All`
- `Aggregate` (not `reduce`)
- `OrderBy` / `OrderByDescending`
- `Take` / `Skip`
- `Count`
- `Distinct`
- `Sum` / `Min` / `Max`
- `ToList` / `ToDictionary`
- `Add` (not `add`) — capitalize like C#
- `Contains` (not `contains`)
- `ForEach`

Method names should be PascalCase to match C# conventions.
