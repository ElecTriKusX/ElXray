import argparse
import unittest

from bench_client import bounded_int, receive_exact


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

    def test_parameter_bounds(self):
        parse = bounded_int(1, 8)
        self.assertEqual(parse("2"), 2)
        for value in ["0", "9", "-1"]:
            with self.assertRaises(argparse.ArgumentTypeError):
                parse(value)


if __name__ == "__main__":
    unittest.main()
