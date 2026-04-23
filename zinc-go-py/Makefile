.PHONY: sync test clean

# Create/refresh the venv and install deps. Idempotent.
sync:
	uv sync

# Run the e2e suite (launcher picks up the uv-managed .venv automatically).
test:
	bash run_e2e.sh

clean:
	/bin/rm -rf build/ dist/ *.egg-info zinc-out/ .venv/
	find . -type d -name __pycache__ -exec /bin/rm -rf {} +
