# Iterating a string yields each character as a length-1 str (Python semantics),
# not a Go rune — so str methods work on the loop variable.
for c in "Hello":
    print(c, c.upper(), c.lower())

word: str = "abc123XYZ"
letters: int = 0
digits: int = 0
for ch in word:
    if ch.isalpha():
        letters = letters + 1
    elif ch.isdigit():
        digits = digits + 1
print(letters, digits)

# build a string back up from its characters
s: str = "stressed"
rev: str = ""
for c in s:
    rev = c + rev
print(rev)

# membership-style counting with a char
vowels: str = "aeiou"
count: int = 0
for c in "education":
    if c in vowels:
        count = count + 1
print(count)

# each Unicode code point is one character
for c in "café":
    print(c)

# inline one-line form composes with string iteration
total_len: int = 0
for c in "abcde": total_len = total_len + len(c)
print(total_len)
