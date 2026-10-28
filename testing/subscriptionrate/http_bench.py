"""Loopback-only, bounded HTTP source/sink for manual rate tests; no disk access."""

import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from socketserver import TCPServer
from urllib.parse import parse_qs, urlsplit

MAX_BYTES = 128 * 1024 * 1024
BLOCK = bytes(64 * 1024)


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.0"

    def setup(self):
        super().setup()
        self.connection.settimeout(15)

    def log_message(self, *_args):
        pass

    def reply(self, code, body):
        self.send_response(code)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        url = urlsplit(self.path)
        if url.path != "/download":
            self.reply(404, b"not found\n")
            return
        try:
            values = parse_qs(url.query, strict_parsing=True)
            if set(values) != {"bytes"} or len(values["bytes"]) != 1:
                raise ValueError
            size = int(values["bytes"][0])
            if not 1 <= size <= MAX_BYTES:
                raise ValueError
        except ValueError:
            self.reply(400, b"bytes must be 1..134217728\n")
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Content-Length", str(size))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        try:
            while size:
                part = BLOCK[: min(size, len(BLOCK))]
                self.wfile.write(part)
                size -= len(part)
        except (OSError, TimeoutError):
            return

    def do_POST(self):
        if self.path != "/upload":
            self.reply(404, b"not found\n")
            return
        try:
            lengths = self.headers.get_all("Content-Length", [])
            if self.headers.get("Transfer-Encoding") or len(lengths) != 1:
                raise ValueError
            size = int(lengths[0])
            if not 1 <= size <= MAX_BYTES:
                raise ValueError
        except ValueError:
            self.reply(400, b"one bounded Content-Length required\n")
            return
        remaining = size
        try:
            while remaining:
                chunk = self.rfile.read(min(remaining, len(BLOCK)))
                if not chunk:
                    self.close_connection = True
                    return
                remaining -= len(chunk)
            self.reply(200, f"received={size}\n".encode())
        except (OSError, TimeoutError):
            self.close_connection = True


class LoopbackHTTPServer(ThreadingHTTPServer):
    def server_bind(self):
        # HTTPServer.server_bind calls getfqdn(), which can stall on an offline
        # or misconfigured reverse resolver. This fixture never needs DNS.
        TCPServer.server_bind(self)
        self.server_name = "localhost"
        self.server_port = self.server_address[1]


def serve(port):
    # Never expose this unauthenticated fixture on an external interface.
    with LoopbackHTTPServer(("127.0.0.1", port), Handler) as server:
        server.daemon_threads = True
        print(f"Test endpoint: http://127.0.0.1:{server.server_port}", flush=True)
        server.serve_forever()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=18080)
    serve(parser.parse_args().port)
