"""Fire many requests at (as close as possible to) the same instant."""

import threading
from collections import Counter
from collections.abc import Callable
from concurrent.futures import ThreadPoolExecutor

import requests


def fire_concurrently(request: Callable[[int], requests.Response], count: int) -> list[requests.Response]:
    """Call request(i) for i in range(count), all released together.

    A Barrier makes every thread wait until all of them are ready, then lets
    them go at once. Without it, threads start one by one and the first
    requests may finish before the last ones begin, which hides races.
    """
    barrier = threading.Barrier(count)

    def run(i: int) -> requests.Response:
        barrier.wait()
        return request(i)

    with ThreadPoolExecutor(max_workers=count) as pool:
        return list(pool.map(run, range(count)))


def outcome_counts(responses: list[requests.Response]) -> Counter:
    """Summarise results as {201: n, 'INSUFFICIENT_FUNDS': m, ...}."""
    counts = Counter()
    for r in responses:
        counts[r.status_code if r.ok else r.json().get("error", {}).get("code", r.status_code)] += 1
    return counts
