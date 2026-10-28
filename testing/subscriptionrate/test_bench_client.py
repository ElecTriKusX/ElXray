import argparse
import unittest
from unittest.mock import patch

from bench_client import LabConnection, bounded_int, receive_exact


class PartialSocket:
    def __init__(self, chunks):
        self.chunks = iter(chunks)

    def recv(self, size):
        return next(self.chunks, b"")


class ClientTest(unittest.TestCase):
    def test_partial_and_closed_handshake(self):
        self.assertEqual(receive_exact(PartialSocket([b"\x05", b"\x00"]), 2), b"\x05\x00")
        with self.assertRaises(OSError):
            receive_exact(PartialSocket([b"\x05"]), 2)

    def test_tun_has_no_socks_or_direct_fallback(self):
        with patch("socket.create_connection", side_effect=OSError("route unavailable")) as connect:
            with self.assertRaises(OSError):
                LabConnection(1080, "tun").connect()
            self.assertEqual(connect.call_count, 1)
            self.assertEqual(connect.call_args.args[0], ("198.18.0.1", 18080))

    def test_parameter_bounds(self):
        parse = bounded_int(1, 8)
        self.assertEqual(parse("2"), 2)
        for value in ["0", "9", "-1"]:
            with self.assertRaises(argparse.ArgumentTypeError):
                parse(value)


if __name__ == "__main__":
    unittest.main()
