class Resource:
    def __init__(self, name: str):
        self.name = name
    def __enter__(self):
        print("acquire " + self.name)
        return self
    def __exit__(self, exc_type, exc_val, exc_tb):
        print("release " + self.name)
    def use(self):
        print("using " + self.name)

with Resource("db") as r:
    print("inside " + r.name)
    r.use()

print("---")

# with without 'as'
class Quiet:
    def __enter__(self):
        print("enter")
        return self
    def __exit__(self, a, b, c):
        print("exit")

with Quiet():
    print("body")

print("---")

# nested with
with Resource("outer") as o:
    with Resource("inner") as i:
        print("both: " + o.name + " " + i.name)
