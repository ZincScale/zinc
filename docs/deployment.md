# Zinc — Deployment Guide

## Overview

Zinc transpiles `.zn` files to `.py` and runs on free-threaded Python (GIL disabled). Every deployment path ensures `PYTHON_GIL=0` for true parallelism.

```
.zn source → zinc transpile → .py file → deploy
```

---

## Quick Reference

| Command | Output | Use case |
|---|---|---|
| `zinc run script.zn` | Runs immediately | Development, scripts |
| `zinc pack script.zn` | PyInstaller binary | CLI tools, desktop apps |
| `zinc pack script.zn --format docker` | Dockerfile + .py | Containers, microservices |
| `zinc pack script.zn --format k8s` | Dockerfile + K8s manifest | Kubernetes deployments |

---

## 1. Run Directly

The simplest deployment — just run the `.zn` file:

```bash
zinc run script.zn
zinc run script.zn -- arg1 arg2
```

**What happens:**
1. Zinc transpiles `script.zn` → temp `.py` file
2. Finds the best Python: `python3.14t` (free-threaded) → `python3`
3. Runs with `PYTHON_GIL=0` for parallel execution
4. Cleans up the temp file

**When to use:** Development, cron jobs, one-off scripts, CI/CD pipelines.

---

## 2. PyInstaller — Single Binary

Bundle your script into a standalone executable with no Python dependency:

```bash
# Step 1: Package
zinc pack script.zn

# Step 2: Install PyInstaller (if needed) using free-threaded Python
python3.14t -m pip install pyinstaller

# Step 3: Build the binary
python3.14t script_pack.py

# Step 4: Run
./dist/script
```

**What `zinc pack` generates:**
- `script.py` — transpiled Python source
- `script_pack.py` — PyInstaller build script pointing at `python3.14t`

**Output:** `dist/script` — single executable (~15-50MB depending on imports)

**Key detail:** PyInstaller bundles the free-threaded Python runtime, so the binary runs with GIL disabled even on machines without Python installed.

**When to use:** CLI tools, desktop utilities, distributing to machines without Python.

---

## 3. Docker — Container Image

Generate a Dockerfile and build a container:

```bash
# Step 1: Generate Dockerfile
zinc pack script.zn --format docker

# Step 2: Build
docker build -t myapp .

# Step 3: Run
docker run myapp
docker run myapp -- arg1 arg2
```

**What `zinc pack --format docker` generates:**

`Dockerfile`:
```dockerfile
FROM python:3.14-slim

# Free-threading enabled — GIL disabled for true parallelism
ENV PYTHON_GIL=0

WORKDIR /app
COPY script.py .

# Install dependencies if requirements.txt exists
RUN if [ -f requirements.txt ]; then pip install --no-cache-dir -r requirements.txt; fi

CMD ["python3", "script.py"]
```

`.dockerignore`:
```
*.zn
*.go
__pycache__
.git
dist
build
```

**Adding dependencies:** Create a `requirements.txt` next to your `.zn` file:
```
polars>=1.0
requests>=2.31
```

The Dockerfile auto-installs them during build.

**When to use:** Microservices, cloud deployments, CI/CD containers.

---

## 4. Kubernetes — Full Deployment

Generate a Docker image + K8s deployment manifest:

```bash
# Step 1: Generate everything
zinc pack script.zn --format k8s

# Step 2: Build and push the Docker image
docker build -t myregistry/myapp:latest .
docker push myregistry/myapp:latest

# Step 3: Update the image in the manifest
# Edit myapp-deployment.yaml: image: myregistry/myapp:latest

# Step 4: Deploy
kubectl apply -f myapp-deployment.yaml

# Step 5: Check status
kubectl get pods -l app=myapp
kubectl logs -l app=myapp
```

**What `zinc pack --format k8s` generates:**

`Dockerfile` (same as above)

`myapp-deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  labels:
    app: myapp
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
  template:
    metadata:
      labels:
        app: myapp
    spec:
      containers:
      - name: myapp
        image: myapp:latest
        env:
        - name: PYTHON_GIL
          value: "0"
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "256Mi"
            cpu: "500m"
```

**Key details:**
- `PYTHON_GIL=0` is set in the pod spec — free-threading in K8s
- Resource limits are set (adjust for your workload)
- Scale with `kubectl scale deployment myapp --replicas=5`

**When to use:** Production services, data pipelines, scheduled jobs in K8s.

---

## 5. Shebang — Executable Scripts

Make `.zn` files directly executable on Unix:

```zinc
#!/usr/bin/env zinc run
print("Hello from zinc!")
```

```bash
chmod +x script.zn
./script.zn
```

**When to use:** Utility scripts, automation, replacing bash scripts.

---

## Free-Threaded Python

Zinc runs on free-threaded Python 3.14+ by default. This means:

- **No GIL** — threads run in true parallel on multiple cores
- **`.map()` auto-parallelizes** — collection chains on 1000+ items use `ThreadPoolExecutor`
- **No fork/pickle issues** — unlike `multiprocessing`, threads share memory directly

### How it works

| Deployment | How GIL is disabled |
|---|---|
| `zinc run` | Finds `python3.14t` binary, or sets `PYTHON_GIL=0` |
| `zinc pack` (PyInstaller) | Bundles `python3.14t` runtime into binary |
| `zinc pack --format docker` | `ENV PYTHON_GIL=0` in Dockerfile |
| `zinc pack --format k8s` | `PYTHON_GIL: "0"` in pod env |

### GIL-dependent library warnings

If you import a library known to have issues with free-threading, Zinc warns at transpile time:

```
$ zinc run pipeline.zn
warning: import "pandas" — pandas has partial free-threading support, consider polars
warning: import "numba" — Numba JIT relies on GIL internals — not yet free-thread safe
```

Safe alternatives:
| Instead of | Use |
|---|---|
| pandas | polars |
| numba | numpy (2.1+) |
| multiprocessing | threading (free-threaded) |
| gevent/eventlet | asyncio or threads |

---

## Project Structure

A typical Zinc project:

```
myapp/
├── main.zn              # entry point
├── utils.zn             # utility functions
├── requirements.txt     # Python dependencies
├── Dockerfile           # generated by zinc pack --format docker
└── myapp-deployment.yaml # generated by zinc pack --format k8s
```

---

## CI/CD Example

GitHub Actions workflow:

```yaml
name: Build and Deploy
on: [push]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v4

    - name: Install Zinc
      run: go install github.com/victorybhg/zinc/cmd/zinc@latest

    - name: Transpile and type check
      run: zinc transpile main.zn

    - name: Build Docker image
      run: |
        zinc pack main.zn --format docker
        docker build -t myapp:${{ github.sha }} .

    - name: Push to registry
      run: docker push myregistry/myapp:${{ github.sha }}

    - name: Deploy to K8s
      run: |
        zinc pack main.zn --format k8s
        kubectl set image deployment/myapp myapp=myregistry/myapp:${{ github.sha }}
```
