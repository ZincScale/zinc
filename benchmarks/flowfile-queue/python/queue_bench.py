#!/usr/bin/env python3
"""
FlowFile Queue Throughput Benchmark — Python
Simulates NiFi-style producer/consumer with FlowFiles shuttled through queues.

Tests threading.Queue, multiprocessing.Queue, and asyncio.Queue
at FlowFile sizes: 1KB, 10KB, 100KB, 1MB
"""

import struct
import time
import threading
import multiprocessing
import asyncio
import os
import sys
import json
from queue import Queue as ThreadQueue
from multiprocessing import Queue as ProcQueue

# --- FlowFile V3 binary format (matches NiFi FlowFilePackagerV3) ---

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


# --- Benchmark helpers ---

def make_flowfile(size: int, index: int) -> bytes:
    """Create a packed FlowFile binary of approximately `size` bytes content."""
    attrs = {
        "filename": f"flowfile_{index}.dat",
        "uuid": f"aaaaaaaa-bbbb-cccc-dddd-{index:012d}",
        "mime.type": "application/octet-stream",
        "path": "/data/input/",
    }
    content = os.urandom(size)
    return package_flowfile(attrs, content)


SENTINEL = None


# --- Threading benchmark (GIL-bound) ---

def thread_producer(q: ThreadQueue, flowfiles: list[bytes], count: int):
    for i in range(count):
        q.put(flowfiles[i % len(flowfiles)])
    q.put(SENTINEL)


def thread_consumer(q: ThreadQueue, results: dict):
    processed = 0
    total_bytes = 0
    while True:
        item = q.get()
        if item is SENTINEL:
            break
        # Unpack to simulate processing
        attrs, content, _ = unpackage_flowfile(item)
        processed += 1
        total_bytes += len(content)
    results["processed"] = processed
    results["total_bytes"] = total_bytes


def bench_threading(flowfiles: list[bytes], count: int, label: str) -> dict:
    q = ThreadQueue(maxsize=1000)
    results = {}

    consumer = threading.Thread(target=thread_consumer, args=(q, results))
    producer = threading.Thread(target=thread_producer, args=(q, flowfiles, count))

    start = time.perf_counter()
    consumer.start()
    producer.start()
    producer.join()
    consumer.join()
    elapsed = time.perf_counter() - start

    msgs_per_sec = results["processed"] / elapsed
    mb_per_sec = results["total_bytes"] / elapsed / (1024 * 1024)
    return {
        "test": f"threading-{label}",
        "count": results["processed"],
        "elapsed_sec": round(elapsed, 3),
        "msgs_per_sec": round(msgs_per_sec),
        "mb_per_sec": round(mb_per_sec, 1),
    }


# --- Multiprocessing benchmark (true parallelism) ---

def mp_producer(q: ProcQueue, flowfiles: list[bytes], count: int):
    for i in range(count):
        q.put(flowfiles[i % len(flowfiles)])
    q.put(SENTINEL)


def mp_consumer(q: ProcQueue, result_q: ProcQueue):
    processed = 0
    total_bytes = 0
    while True:
        item = q.get()
        if item is SENTINEL:
            break
        attrs, content, _ = unpackage_flowfile(item)
        processed += 1
        total_bytes += len(content)
    result_q.put({"processed": processed, "total_bytes": total_bytes})


def bench_multiprocessing(flowfiles: list[bytes], count: int, label: str) -> dict:
    q = ProcQueue(maxsize=1000)
    result_q = ProcQueue()

    consumer = multiprocessing.Process(target=mp_consumer, args=(q, result_q))
    producer = multiprocessing.Process(target=mp_producer, args=(q, flowfiles, count))

    start = time.perf_counter()
    consumer.start()
    producer.start()
    producer.join()
    consumer.join()
    elapsed = time.perf_counter() - start

    results = result_q.get()
    msgs_per_sec = results["processed"] / elapsed
    mb_per_sec = results["total_bytes"] / elapsed / (1024 * 1024)
    return {
        "test": f"multiproc-{label}",
        "count": results["processed"],
        "elapsed_sec": round(elapsed, 3),
        "msgs_per_sec": round(msgs_per_sec),
        "mb_per_sec": round(mb_per_sec, 1),
    }


# --- Asyncio benchmark (single-threaded concurrency) ---

async def async_producer(q: asyncio.Queue, flowfiles: list[bytes], count: int):
    for i in range(count):
        await q.put(flowfiles[i % len(flowfiles)])
    await q.put(SENTINEL)


async def async_consumer(q: asyncio.Queue) -> dict:
    processed = 0
    total_bytes = 0
    while True:
        item = await q.get()
        if item is SENTINEL:
            break
        attrs, content, _ = unpackage_flowfile(item)
        processed += 1
        total_bytes += len(content)
    return {"processed": processed, "total_bytes": total_bytes}


async def bench_asyncio_inner(flowfiles: list[bytes], count: int) -> dict:
    q = asyncio.Queue(maxsize=1000)
    consumer_task = asyncio.create_task(async_consumer(q))
    producer_task = asyncio.create_task(async_producer(q, flowfiles, count))
    await producer_task
    return await consumer_task


def bench_asyncio(flowfiles: list[bytes], count: int, label: str) -> dict:
    start = time.perf_counter()
    results = asyncio.run(bench_asyncio_inner(flowfiles, count))
    elapsed = time.perf_counter() - start

    msgs_per_sec = results["processed"] / elapsed
    mb_per_sec = results["total_bytes"] / elapsed / (1024 * 1024)
    return {
        "test": f"asyncio-{label}",
        "count": results["processed"],
        "elapsed_sec": round(elapsed, 3),
        "msgs_per_sec": round(msgs_per_sec),
        "mb_per_sec": round(mb_per_sec, 1),
    }


# --- Multi-consumer threading benchmark (fan-out) ---

def bench_threading_fanout(flowfiles: list[bytes], count: int, num_consumers: int, label: str) -> dict:
    """Multiple consumers reading from the same queue — simulates processor fan-out."""
    q = ThreadQueue(maxsize=2000)
    all_results = []

    def consumer(results_list, idx):
        processed = 0
        total_bytes = 0
        while True:
            item = q.get()
            if item is SENTINEL:
                q.put(SENTINEL)  # re-post for other consumers
                break
            attrs, content, _ = unpackage_flowfile(item)
            processed += 1
            total_bytes += len(content)
        results_list.append({"processed": processed, "total_bytes": total_bytes})

    consumers = []
    for i in range(num_consumers):
        t = threading.Thread(target=consumer, args=(all_results, i))
        consumers.append(t)

    start = time.perf_counter()
    for t in consumers:
        t.start()
    # Producer inline
    for i in range(count):
        q.put(flowfiles[i % len(flowfiles)])
    q.put(SENTINEL)
    for t in consumers:
        t.join()
    elapsed = time.perf_counter() - start

    total_processed = sum(r["processed"] for r in all_results)
    total_bytes = sum(r["total_bytes"] for r in all_results)
    msgs_per_sec = total_processed / elapsed
    mb_per_sec = total_bytes / elapsed / (1024 * 1024)
    return {
        "test": f"threading-fanout-{num_consumers}c-{label}",
        "count": total_processed,
        "elapsed_sec": round(elapsed, 3),
        "msgs_per_sec": round(msgs_per_sec),
        "mb_per_sec": round(mb_per_sec, 1),
    }


# --- Main ---

def run_benchmarks():
    sizes = {
        "1KB": 1024,
        "10KB": 10 * 1024,
        "100KB": 100 * 1024,
        "1MB": 1024 * 1024,
    }

    # Scale count inversely with size so benchmarks finish in reasonable time
    counts = {
        "1KB": 100_000,
        "10KB": 50_000,
        "100KB": 10_000,
        "1MB": 2_000,
    }

    results = []

    print(f"Python {sys.version}")
    print(f"GIL enabled: {sys._is_gil_enabled() if hasattr(sys, '_is_gil_enabled') else 'N/A (pre-3.13)'}")
    print(f"CPUs: {os.cpu_count()}")
    print("=" * 70)

    for size_label, size_bytes in sizes.items():
        count = counts[size_label]
        print(f"\n--- FlowFile size: {size_label} | count: {count:,} ---")

        # Pre-generate a pool of packed FlowFiles
        pool_size = min(count, 100)
        flowfiles = [make_flowfile(size_bytes, i) for i in range(pool_size)]

        # Threading (GIL-bound)
        r = bench_threading(flowfiles, count, size_label)
        print(f"  threading:       {r['msgs_per_sec']:>10,} msgs/s  {r['mb_per_sec']:>8.1f} MB/s  ({r['elapsed_sec']}s)")
        results.append(r)

        # Asyncio
        r = bench_asyncio(flowfiles, count, size_label)
        print(f"  asyncio:         {r['msgs_per_sec']:>10,} msgs/s  {r['mb_per_sec']:>8.1f} MB/s  ({r['elapsed_sec']}s)")
        results.append(r)

        # Multiprocessing (true parallelism)
        r = bench_multiprocessing(flowfiles, count, size_label)
        print(f"  multiprocessing: {r['msgs_per_sec']:>10,} msgs/s  {r['mb_per_sec']:>8.1f} MB/s  ({r['elapsed_sec']}s)")
        results.append(r)

        # Fan-out (4 consumers)
        r = bench_threading_fanout(flowfiles, count, 4, size_label)
        print(f"  fanout (4 cons): {r['msgs_per_sec']:>10,} msgs/s  {r['mb_per_sec']:>8.1f} MB/s  ({r['elapsed_sec']}s)")
        results.append(r)

    # Write JSON results
    out_path = os.path.join(os.path.dirname(__file__), "results.json")
    with open(out_path, "w") as f:
        json.dump(results, f, indent=2)
    print(f"\nResults written to {out_path}")


if __name__ == "__main__":
    run_benchmarks()
