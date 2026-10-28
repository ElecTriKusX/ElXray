"""Measure one common server counter window for concurrent LAN/TUN tests."""

import argparse
import json
from pathlib import Path
import subprocess
import time


def traffic(response, email):
    prefix = f"user>>>{email}>>>traffic>>>"
    counters = {item["name"]: int(item.get("value", 0)) for item in response.get("stat", [])}
    required = {direction: prefix + direction for direction in ("uplink", "downlink")}
    if any(name not in counters for name in required.values()):
        raise ValueError("Traffic counters missing; start both transfers and enable user stats")
    return {direction: counters[name] for direction, name in required.items()}


def rates(before, after, seconds):
    if seconds <= 0 or any(after[key] < before[key] for key in before):
        raise ValueError("Counters reset/restarted or invalid measurement interval")
    return {key: round((after[key] - before[key]) * 8 / seconds / 1_000_000, 3) for key in before}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--xray", type=Path, required=True)
    parser.add_argument("--email", default="lab@example")
    parser.add_argument("--seconds", type=float, default=30)
    args = parser.parse_args()
    if not 1 <= args.seconds <= 60:
        parser.error("seconds must be 1..60")
    binary = args.xray.resolve()

    def snapshot():
        result = subprocess.run([str(binary), "api", "statsquery", "--server=127.0.0.1:10085",
                                 "-pattern", f"user>>>{args.email}>>>traffic>>>"],
                                capture_output=True, text=True, check=True, timeout=10)
        return traffic(json.loads(result.stdout), args.email)

    try:
        before = snapshot()
        start = time.monotonic()
        time.sleep(args.seconds)
        after = snapshot()
        elapsed = time.monotonic() - start
        print(json.dumps({"seconds": round(elapsed, 3), "Mbps": rates(before, after, elapsed)}, indent=2))
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        parser.exit(1, f"Measurement failed: {error}\n")


if __name__ == "__main__":
    main()
