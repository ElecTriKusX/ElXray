"""Measure the local lab through SOCKS5 without shell-specific curl quoting."""

import argparse
from concurrent.futures import ThreadPoolExecutor
import http.client
import json
import socket
import time

BLOCK = bytes(64 * 1024)
MAX_BYTES = 128 * 1024 * 1024


def receive_exact(sock, size):
    result = bytearray()
    while len(result) < size:
        part = sock.recv(size - len(result))
        if not part:
            raise OSError("SOCKS connection closed during handshake")
        result.extend(part)
    return bytes(result)


class LabConnection(http.client.HTTPConnection):
    def __init__(self, socks_port, mode="socks"):
        super().__init__("198.18.0.1" if mode == "tun" else "127.0.0.1", 18080, timeout=15)
        self.socks_port = socks_port
        self.mode = mode

    def connect(self):
        if self.mode == "tun":
            return super().connect()
        sock = socket.create_connection(("127.0.0.1", self.socks_port), timeout=15)
        try:
            sock.sendall(b"\x05\x01\x00")
            if receive_exact(sock, 2) != b"\x05\x00":
                raise OSError("SOCKS5 proxy did not accept no-auth method")
            # Fixed destination: the HTTP fixture on the SERVER's loopback.
            sock.sendall(b"\x05\x01\x00\x01\x7f\x00\x00\x01" + (18080).to_bytes(2, "big"))
            header = receive_exact(sock, 4)
            if header[:3] != b"\x05\x00\x00":
                raise OSError(f"SOCKS CONNECT rejected: {header.hex()}")
            if header[3] == 1:
                receive_exact(sock, 6)
            elif header[3] == 4:
                receive_exact(sock, 18)
            elif header[3] == 3:
                receive_exact(sock, receive_exact(sock, 1)[0] + 2)
            else:
                raise OSError("Invalid SOCKS5 address type")
            self.sock = sock
        except BaseException:
            sock.close()
            raise


def transfer(index, direction, size, socks_port, mode="socks"):
    connection = LabConnection(socks_port, mode)
    started = time.monotonic()
    try:
        if direction == "download":
            connection.request("GET", f"/download?bytes={size}")
        else:
            connection.putrequest("POST", "/upload")
            connection.putheader("Content-Length", str(size))
            connection.endheaders()
            remaining = size
            while remaining:
                part = BLOCK[: min(remaining, len(BLOCK))]
                connection.send(part)
                remaining -= len(part)
        response = connection.getresponse()
        if response.status != 200:
            detail = response.read(256).decode("utf-8", errors="replace")
            raise OSError(f"HTTP {response.status}: {detail}")
        if direction == "download":
            if response.getheader("Content-Length") != str(size):
                raise OSError("Unexpected HTTP response length")
            received = 0
            while True:
                block = response.read(len(BLOCK))
                if not block:
                    break
                if any(block):
                    raise OSError("Nonzero bytes in generated test payload")
                received += len(block)
            if received != size:
                raise OSError(f"Incomplete download: {received}/{size}")
        elif response.read(128) != f"received={size}\n".encode():
            raise OSError("Server did not confirm the complete upload")
        elapsed = time.monotonic() - started
        return {"connection": index, "direction": direction, "bytes": size, "finished_at": time.monotonic(), "seconds": round(elapsed, 3),
                "Mbps": round(size * 8 / elapsed / 1_000_000, 3)}
    finally:
        connection.close()


def bounded_int(low, high):
    def parse(value):
        number = int(value)
        if not low <= number <= high:
            raise argparse.ArgumentTypeError(f"must be {low}..{high}")
        return number
    return parse


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--connections", type=bounded_int(1, 8), default=1)
    parser.add_argument("--bytes", type=bounded_int(1, MAX_BYTES), default=16 * 1024 * 1024)
    parser.add_argument("--direction", choices=["download", "upload", "both"], default="download")
    parser.add_argument("--socks-port", type=bounded_int(1, 65535), default=1080)
    parser.add_argument("--mode", choices=["socks", "tun"], default="socks")
    args = parser.parse_args()
    started = time.monotonic()
    try:
        directions = ["download", "upload"] if args.direction == "both" else [args.direction]
        with ThreadPoolExecutor(max_workers=args.connections * len(directions)) as pool:
            jobs = [pool.submit(transfer, index + 1, direction, args.bytes, args.socks_port, args.mode)
                    for direction in directions for index in range(args.connections)]
            results = [job.result() for job in jobs]
        windows = {direction: max(item["finished_at"] for item in results if item["direction"] == direction) - started for direction in directions}
        for item in results:
            del item["finished_at"]
        elapsed = time.monotonic() - started
    except (OSError, http.client.HTTPException) as error:
        parser.exit(1, f"Test failed: {error}\n")
    print(json.dumps({"direction": args.direction, "results": results,
                      "aggregate_Mbps": round(sum(item["bytes"] for item in results) * 8 / elapsed / 1_000_000, 3),
                      "aggregate_by_direction_Mbps": {direction: round(sum(item["bytes"] for item in results if item["direction"] == direction) * 8 / windows[direction] / 1_000_000, 3) for direction in directions},
                      "wall_seconds": round(elapsed, 3)}, indent=2))


if __name__ == "__main__":
    main()
