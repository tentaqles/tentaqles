"""Server-Sent Events broker.

Thread-safe fan-out of JSON-serializable event dicts to any number of
subscriber queues. Each subscriber gets its own ``queue.Queue`` so a slow
or disconnected client cannot block the publisher.
"""

from __future__ import annotations

import queue
import threading
from typing import Any


class SSEBroker:
    """Minimal thread-safe pub/sub broker for SSE clients."""

    def __init__(self) -> None:
        self._subscribers: list[queue.Queue] = []
        self._lock = threading.Lock()

    def subscribe(self) -> queue.Queue:
        """Register a new subscriber and return its queue."""
        q: queue.Queue = queue.Queue(maxsize=100)
        with self._lock:
            self._subscribers.append(q)
        return q

    def unsubscribe(self, q: queue.Queue) -> None:
        """Remove a subscriber queue. Safe to call twice."""
        with self._lock:
            try:
                self._subscribers.remove(q)
            except ValueError:
                pass

    def publish(self, event: dict[str, Any]) -> None:
        """Fan-out an event to every live subscriber.

        Drops the event for subscribers whose queues are full rather than
        blocking the publisher thread.
        """
        with self._lock:
            subs = list(self._subscribers)
        for q in subs:
            try:
                q.put_nowait(event)
            except queue.Full:
                # Slow consumer — skip this event for them
                continue

    def subscriber_count(self) -> int:
        with self._lock:
            return len(self._subscribers)


# Module-level singleton used by the HTTP server and any publisher.
_broker = SSEBroker()


def get_broker() -> SSEBroker:
    """Return the process-wide broker singleton."""
    return _broker
