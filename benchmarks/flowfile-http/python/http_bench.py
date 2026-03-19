#!/usr/bin/env python3
"""
FlowFile HTTP API Benchmark — Python
Simulates NiFi-style REST ingress/egress for FlowFiles.

Server runs in a subprocess (aiohttp). Client hammers it from main process.
"""

import asyncio
import multiprocessing
import os
import signal
import struct
import sys
import time
import json

import aiohttp
from aiohttp import web

# --- FlowFile V3 format ---

MAGIC = b"NiFiFF3"
MAX_VALUE_2_BYTES = 65535


def write_field_length(value: int) -> bytes:
    if value < MAX_VALUE_2_BYTES:
        return struct.pack(">H", value)
    return b"\xff\xff" + struct.pack(">I", value)


def read_field_length(data: bytes, offset: int) -> tuple[int, int]:
    val = struct.unpack(">H", data[offset:offset + 2])[0]
    if val < MAX_VALUE_2_BYTES:
        return val, offset + 2
    big = struct.unpack(">I", data[offset + 2:offset + 6])[0]
    return big, offset + 6


def package_flowfile(attributes: dict, content: bytes) -> bytes:
    buf = bytearray()
    buf.extend(MAGIC)
    buf.extend(write_field_length(len(attributes)))
    for key, value in attributes.items():
        key_bytes = key.encode("utf-8")
        val_bytes = str(value).encode("utf-8") if value is not None else b""
        buf.extend(write_field_length(len(key_bytes)))
        buf.extend(key_bytes)
        buf.extend(write_field_length(len(val_bytes)))
        buf.extend(val_bytes)
    buf.extend(struct.pack(">q", len(content)))
    buf.extend(content)
    return bytes(buf)


def unpackage_flowfile(data: bytes, offset: int = 0) -> tuple[dict, bytes, int]:
    assert data[offset:offset + 7] == MAGIC
    pos = offset + 7
    count, pos = read_field_length(data, pos)
    attributes = {}
    for _ in range(count):
        key_len, pos = read_field_length(data, pos)
        key = data[pos:pos + key_len].decode("utf-8")
        pos += key_len
        val_len, pos = read_field_length(data, pos)
        val = data[pos:pos + val_len].decode("utf-8")
        pos += val_len
        attributes[key] = val
    content_len = struct.unpack(">q", data[pos:pos + 8])[0]
    pos += 8
    content = data[pos:pos + content_len]
    pos += content_len
    return attributes, content, pos


def make_flowfile(size: int, index: int) -> bytes:
    attrs = {
        "filename": f"flowfile_{index}.dat",
        "uuid": f"aaaaaaaa-bbbb-cccc-dddd-{index:012d}",
        "mime.type": "application/octet-stream",
        "path": "/data/input/",
    }
    content = os.urandom(size)
    return package_flowfile(attrs, content)


# --- Server (runs in subprocess) ---

def run_server(host: str, port: int, ready_event):
    """Run aiohttp server in a separate process."""
    queue = asyncio.Queue()
    received = {"count": 0, "bytes": 0}

    async def handle_post(request):
        data = await request.read()
        received["count"] += 1
        received["bytes"] += len(data)
        await queue.put(data)
        return web.Response(status=200, text="OK")

    async def handle_get(request):
        try:
            data = queue.get_nowait()
            return web.Response(status=200, body=data, content_type="application/flowfile-v3")
        except asyncio.QueueEmpty:
            return web.Response(status=204, text="No content")

    async def handle_stats(request):
        return web.json_response({
            "queue_size": queue.qsize(),
            "received_count": received["count"],
            "received_bytes": received["bytes"],
        })

    async def handle_reset(request):
        """Drain the queue and reset counters."""
        dropped = 0
        while not queue.empty():
            try:
                queue.get_nowait()
                dropped += 1
            except asyncio.QueueEmpty:
                break
        received["count"] = 0
        received["bytes"] = 0
        return web.json_response({"dropped": dropped})

    async def start():
        app = web.Application(client_max_size=10 * 1024 * 1024)  # 10MB max body
        app.router.add_post("/flowfile", handle_post)
        app.router.add_get("/flowfile", handle_get)
        app.router.add_get("/stats", handle_stats)
        app.router.add_post("/reset", handle_reset)

        runner = web.AppRunner(app)
        await runner.setup()
        site = web.TCPSite(runner, host, port, reuse_address=True)
        await site.start()
        ready_event.set()

        # Run until killed
        try:
            while True:
                await asyncio.sleep(3600)
        except asyncio.CancelledError:
            pass
        finally:
            await runner.cleanup()

    asyncio.run(start())


# --- Client benchmarks ---

async def bench_post(url: str, flowfiles: list[bytes], count: int, concurrency: int, label: str) -> dict:
    """POST FlowFiles to server with worker pool."""
    posted = 0
    total_bytes = 0

    timeout = aiohttp.ClientTimeout(total=300)
    conn = aiohttp.TCPConnector(limit=concurrency)
    work_queue: asyncio.Queue[bytes | None] = asyncio.Queue(maxsize=concurrency * 2)

    async with aiohttp.ClientSession(timeout=timeout, connector=conn) as session:

        async def worker():
            nonlocal posted, total_bytes
            while True:
                ff = await work_queue.get()
                if ff is None:
                    break
                async with session.post(url, data=ff) as resp:
                    assert resp.status == 200
                    posted += 1
                    total_bytes += len(ff)

        start = time.perf_counter()
        workers = [asyncio.create_task(worker()) for _ in range(concurrency)]

        for i in range(count):
            await work_queue.put(flowfiles[i % len(flowfiles)])
        for _ in range(concurrency):
            await work_queue.put(None)

        await asyncio.gather(*workers)
        elapsed = time.perf_counter() - start

    msgs_per_sec = posted / elapsed
    mb_per_sec = total_bytes / elapsed / (1024 * 1024)
    return {
        "test": f"http-post-c{concurrency}-{label}",
        "count": posted,
        "elapsed_sec": round(elapsed, 3),
        "msgs_per_sec": round(msgs_per_sec),
        "mb_per_sec": round(mb_per_sec, 1),
    }


async def bench_roundtrip(post_url: str, get_url: str, flowfiles: list[bytes], count: int, concurrency: int, label: str) -> dict:
    """POST FlowFiles, then GET them back — full round-trip."""
    completed = 0
    total_bytes = 0

    timeout = aiohttp.ClientTimeout(total=300)
    conn = aiohttp.TCPConnector(limit=concurrency * 2)
    async with aiohttp.ClientSession(timeout=timeout, connector=conn) as session:
        # Fill the queue
        fill_q: asyncio.Queue[bytes | None] = asyncio.Queue(maxsize=concurrency * 2)

        async def fill_worker():
            while True:
                ff = await fill_q.get()
                if ff is None:
                    break
                async with session.post(post_url, data=ff) as resp:
                    pass

        fill_workers = [asyncio.create_task(fill_worker()) for _ in range(concurrency)]
        for i in range(count):
            await fill_q.put(flowfiles[i % len(flowfiles)])
        for _ in range(concurrency):
            await fill_q.put(None)
        await asyncio.gather(*fill_workers)

        # GET them back
        get_q: asyncio.Queue[bool | None] = asyncio.Queue(maxsize=concurrency * 2)

        async def get_worker():
            nonlocal completed, total_bytes
            while True:
                item = await get_q.get()
                if item is None:
                    break
                async with session.get(get_url) as resp:
                    if resp.status == 200:
                        data = await resp.read()
                        attrs, content, _ = unpackage_flowfile(data)
                        completed += 1
                        total_bytes += len(content)

        start = time.perf_counter()
        get_workers = [asyncio.create_task(get_worker()) for _ in range(concurrency)]
        for _ in range(count):
            await get_q.put(True)
        for _ in range(concurrency):
            await get_q.put(None)
        await asyncio.gather(*get_workers)
        elapsed = time.perf_counter() - start

    msgs_per_sec = completed / elapsed if elapsed > 0 else 0
    mb_per_sec = total_bytes / elapsed / (1024 * 1024) if elapsed > 0 else 0
    return {
        "test": f"http-roundtrip-c{concurrency}-{label}",
        "count": completed,
        "elapsed_sec": round(elapsed, 3),
        "msgs_per_sec": round(msgs_per_sec),
        "mb_per_sec": round(mb_per_sec, 1),
    }


async def run_benchmarks():
    host = "127.0.0.1"
    port = 18080
    base_url = f"http://{host}:{port}"

    # Start server in subprocess
    ready = multiprocessing.Event()
    server_proc = multiprocessing.Process(target=run_server, args=(host, port, ready), daemon=True)
    server_proc.start()
    ready.wait(timeout=10)

    try:
        sizes = {
            "1KB": 1024,
            "10KB": 10 * 1024,
            "100KB": 100 * 1024,
            "1MB": 1024 * 1024,
        }

        counts = {
            "1KB": 20_000,
            "10KB": 10_000,
            "100KB": 2_000,
            "1MB": 500,
        }

        concurrencies = [10, 50]
        results = []

        print(f"Python {sys.version}", flush=True)
        print(f"Server: aiohttp @ {base_url} (subprocess)", flush=True)
        print(f"CPUs: {os.cpu_count()}", flush=True)
        print("=" * 70, flush=True)

        for size_label, size_bytes in sizes.items():
            count = counts[size_label]
            pool_size = min(count, 50)
            flowfiles = [make_flowfile(size_bytes, i) for i in range(pool_size)]

            print(f"\n--- FlowFile size: {size_label} | count: {count:,} ---", flush=True)

            for conc in concurrencies:
                # Reset server queue before each test
                async with aiohttp.ClientSession() as s:
                    await s.post(f"{base_url}/reset")

                # POST benchmark
                r = await bench_post(f"{base_url}/flowfile", flowfiles, count, conc, size_label)
                print(f"  POST c={conc:>2}: {r['msgs_per_sec']:>10,} msgs/s  {r['mb_per_sec']:>8.1f} MB/s  ({r['elapsed_sec']}s)", flush=True)
                results.append(r)

                # Reset before round-trip so GET only pulls items from this test
                async with aiohttp.ClientSession() as s:
                    await s.post(f"{base_url}/reset")

                # Round-trip (POST then GET back)
                r = await bench_roundtrip(f"{base_url}/flowfile", f"{base_url}/flowfile", flowfiles, count, conc, size_label)
                print(f"  RT   c={conc:>2}: {r['msgs_per_sec']:>10,} msgs/s  {r['mb_per_sec']:>8.1f} MB/s  ({r['elapsed_sec']}s)", flush=True)
                results.append(r)

        # Write JSON results
        out_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "results.json")
        with open(out_path, "w") as f:
            json.dump(results, f, indent=2)
        print(f"\nResults written to {out_path}", flush=True)

    finally:
        server_proc.kill()
        server_proc.join(timeout=5)


if __name__ == "__main__":
    asyncio.run(run_benchmarks())
