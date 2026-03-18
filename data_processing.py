def create_orders() -> list[dict]:
    return [{"id": "1", "customer": "Alice", "amount": 150.0, "status": "completed"}, {"id": "2", "customer": "Bob", "amount": 50.0, "status": "pending"}, {"id": "3", "customer": "Alice", "amount": 300.0, "status": "completed"}, {"id": "4", "customer": "Charlie", "amount": 75.0, "status": "completed"}, {"id": "5", "customer": "Bob", "amount": 200.0, "status": "cancelled"}]

orders = create_orders()
print(f"Total orders: {len(orders)}")
completed = [o for o in orders if (o["status"] == "completed")]
print(f"Completed: {len(completed)}")
revenue = sum(o["amount"] for o in completed)
print(f"Revenue: ${revenue}")
customers = [o["customer"] for o in completed]
print(f"Customers: {customers}")
