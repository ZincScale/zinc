class Stack:
    def __init__(self):
        self.items = []
    def push(self, x: int):
        self.items.append(x)
    def size(self) -> int:
        return len(self.items)
    def total(self) -> int:
        s = 0
        for v in self.items:
            s = s + v
        return s

st = Stack()
st.push(10)
st.push(20)
st.push(30)
print(st.size())
print(st.total())
print(st.items[0])
print(st.items[-1])

class Roster:
    def __init__(self):
        self.names = ["alice", "bob"]
    def add(self, n: str):
        self.names.append(n)

r = Roster()
r.add("carol")
print(len(r.names))
for n in r.names:
    print(n)
