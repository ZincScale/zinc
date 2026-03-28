"""Zinc runtime support for Python 3.14t target."""

import threading
import queue
import time


class ZincError(Exception):
    """Base exception for Zinc error values.

    Maps to: return Error("msg") in Zinc source.
    """

    def __init__(self, message=None):
        if isinstance(message, Exception):
            super().__init__(str(message))
            self.__cause__ = message
        else:
            super().__init__(message)


class ZincFuture:
    """Wraps a thread + result tracking.

    Maps to: CompletableFuture<Void> on Java target.
    Provides .join(), .isDone(), .isFailed() matching the Zinc API.
    """

    def __init__(self, fn, or_handler=None):
        self._exception = None
        self._done = threading.Event()

        def _run():
            try:
                fn()
            except Exception as e:
                self._exception = e
                if or_handler:
                    try:
                        or_handler()
                    except Exception:
                        pass
            finally:
                self._done.set()

        self._thread = threading.Thread(target=_run, daemon=True)
        self._thread.start()

    def join(self):
        self._thread.join()
        if self._exception:
            raise self._exception

    def isDone(self):
        return self._done.is_set()

    def isFailed(self):
        return self._exception is not None


class ZincChannel:
    """Bounded channel wrapping queue.Queue.

    Maps to: Channel<T> in Zinc, BlockingQueue in Java.
    """

    def __init__(self, maxsize=0):
        self._q = queue.Queue(maxsize=maxsize)

    def send(self, value):
        self._q.put(value)

    def receive(self):
        return self._q.get()


def zinc_sleep(ms):
    """Sleep for ms milliseconds. Zinc uses ms, Python uses seconds."""
    time.sleep(ms / 1000.0)
