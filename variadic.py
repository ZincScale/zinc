def sum_all(*args) -> int:
    total = 0
    for n in args:
        total = (total + n)
    return total

def create_user(**kwargs):
    print(f"creating user: {kwargs}")

def log(level: str, *args, **kwargs):
    message = " ".join([str(a) for a in args])
    print(f"[{level}] {message} {kwargs}")

print(f"sum: {sum_all(1, 2, 3, 4, 5)}")
create_user(name="Alice", age=30, role="admin")
log("INFO", "server", "started", port=8080)
log("ERROR", "connection", "failed", host="db.example.com", retries=3)
