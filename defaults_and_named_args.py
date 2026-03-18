def greet(name: str, greeting: str = "Hello", punctuation: str = "!"):
    print(f"{greeting}, {name}{punctuation}")

def connect(host: str, port: int = 5432, ssl: bool = True, timeout: int = 30):
    print(f"Connecting to {host}:{port} ssl={ssl} timeout={timeout}")

greet("Alice")
greet("Bob", "Hi")
greet("Charlie", "Hey", ".")
greet("Dave", punctuation="?")
greet("Eve", greeting="Howdy", punctuation="!!")
connect("db.example.com")
connect("db.example.com", port=3306, ssl=False)
connect("localhost", timeout=5)
