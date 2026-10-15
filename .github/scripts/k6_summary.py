"""Render a k6 --summary-export JSON file as Markdown for the GitHub job summary.

Usage: python3 k6_summary.py <summary.json> "<title>"

In the export, each threshold maps to a boolean that is True when the
threshold was BREACHED (so False means it passed).
"""

import json
import re
import sys

STAGE = re.compile(r"^(?P<metric>\w+)\{stage:(?P<stage>[^}]+)\}$")


def ms(value):
    return f"{value:.1f} ms" if value is not None else "–"


def main(path, title):
    try:
        with open(path) as f:
            metrics = json.load(f)["metrics"]
    except (OSError, ValueError, KeyError) as exc:
        print(f"### {title}\n\nNo k6 summary found ({exc}).")
        return

    duration = metrics.get("http_req_duration", {})
    failed = metrics.get("http_req_failed", {})
    reqs = metrics.get("http_reqs", {})
    checks = metrics.get("checks", {})

    print(f"### {title}\n")
    print("| Requests | Throughput | p50 | p95 | p99 | Error rate | Checks |")
    print("|---|---|---|---|---|---|---|")
    print(
        f"| {reqs.get('count', 0):,} | {reqs.get('rate', 0):.0f} req/s | {ms(duration.get('med'))} "
        f"| {ms(duration.get('p(95)'))} | {ms(duration.get('p(99)'))} "
        f"| {failed.get('value', 0):.2%} | {checks.get('value', 0):.2%} |"
    )

    print("\n**Thresholds**\n")
    print("| Metric | Threshold | Result |")
    print("|---|---|---|")
    for name, data in sorted(metrics.items()):
        if STAGE.match(name):
            continue  # shown in the per-stage table below
        for threshold, breached in data.get("thresholds", {}).items():
            print(f"| `{name}` | `{threshold}` | {'❌ breached' if breached else '✅ passed'} |")

    stages = per_stage(metrics)
    if stages:
        print("\n**Per stage**\n")
        print("| Stage | p95 | Error rate | Within SLO |")
        print("|---|---|---|---|")
        breaking_point = None
        for stage, data in stages.items():
            ok = not data["breached"]
            if not ok and breaking_point is None:
                breaking_point = stage
            print(f"| {stage} | {ms(data.get('p95'))} | {data.get('error_rate', 0):.2%} | {'✅' if ok else '❌'} |")
        if breaking_point:
            print(f"\n**First stage outside the SLO: `{breaking_point}`**")
        else:
            print("\n**All stages within the SLO.**")


def per_stage(metrics):
    stages = {}
    for name, data in metrics.items():
        match = STAGE.match(name)
        if not match:
            continue
        stage = stages.setdefault(match["stage"], {"breached": False})
        stage["breached"] |= any(data.get("thresholds", {}).values())
        if match["metric"] == "http_req_duration":
            stage["p95"] = data.get("p(95)")
        elif match["metric"] == "http_req_failed":
            stage["error_rate"] = data.get("value", 0)
    return dict(sorted(stages.items()))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else "k6")
