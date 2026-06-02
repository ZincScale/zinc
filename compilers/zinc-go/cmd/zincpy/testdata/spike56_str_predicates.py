# str predicate methods: isalpha / isdigit / isalnum / isspace / isupper /
# islower / istitle. Empty string is always False; tests run per code point.
cases: list[str] = [
    "abc", "ABC", "Abc", "abc123", "123", "12.3", "  ", "",
    "Hello World", "hello world", "HELLO WORLD", "Title Case Here",
    "mixedUP", "a1b2", "  ss ", "x",
]
for s in cases:
    print(s, "|",
          s.isalpha(), s.isdigit(), s.isalnum(), s.isspace(),
          s.isupper(), s.islower(), s.istitle())

# used in a boolean context / guard
def only_digits(s: str) -> bool:
    return s.isdigit()

print(only_digits("4096"))
print(only_digits("40x6"))

words: list[str] = ["Hello", "world42", "  ", "MIX"]
alpha_words: int = 0
for w in words:
    if w.isalpha():
        alpha_words = alpha_words + 1
print(alpha_words)
