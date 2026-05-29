class Point:
    def __init__(self, x: int, y: int):
        self.x = x
        self.y = y

    def dist_sq(self) -> int:
        return self.x * self.x + self.y * self.y

    def translate(self, dx: int, dy: int) -> int:
        self.x = self.x + dx
        self.y = self.y + dy
        return self.x


class Account:
    def __init__(self, owner: str, balance: int):
        self.owner = owner
        self.balance = balance

    def deposit(self, amount: int) -> int:
        self.balance = self.balance + amount
        return self.balance

    def label(self) -> str:
        return self.owner


p = Point(3, 4)
print(p.x)
print(p.y)
print(p.dist_sq())
print(p.translate(1, 1))
print(p.x)
print(p.dist_sq())

a = Account("alice", 100)
print(a.label())
print(a.deposit(50))
print(a.deposit(25))
print(a.balance)
