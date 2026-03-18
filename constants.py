PI = 3.14159

MAX_RETRIES = 3

APP_NAME = "zinc-app"

def circle_area(radius: float) -> float:
    return (PI * (radius ** 2))

def circle_circumference(radius: float) -> float:
    return ((2 * PI) * radius)

print(f"App: {APP_NAME}")
print(f"Area of r=5: {circle_area(5.0)}")
print(f"Circumference of r=5: {circle_circumference(5.0)}")
print(f"Max retries: {MAX_RETRIES}")
