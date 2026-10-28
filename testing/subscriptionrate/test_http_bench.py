import http.client
import threading
import unittest
from http.server import ThreadingHTTPServer

from http_bench import Handler, MAX_BYTES


class BenchTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=2)

    def request(self, method, path, body=None, headers=None):
        connection = http.client.HTTPConnection(*self.server.server_address, timeout=2)
        try:
            connection.request(method, path, body=body, headers=headers or {})
            response = connection.getresponse()
            return response.status, response.read()
        finally:
            connection.close()

    def test_download_and_upload(self):
        status, body = self.request("GET", "/download?bytes=131073")
        self.assertEqual((status, body), (200, bytes(131073)))
        self.assertEqual(self.request("POST", "/upload", b"fixture"), (200, b"received=7\n"))

    def test_bad_sizes_and_paths(self):
        for query in ["", "bytes=0", "bytes=-1", "bytes=nan", "bytes=1&bytes=2", f"bytes={MAX_BYTES+1}"]:
            self.assertEqual(self.request("GET", "/download?" + query)[0], 400)
        self.assertEqual(self.request("GET", "/etc/passwd")[0], 404)
        self.assertEqual(self.request("POST", "/upload", b"", {"Content-Length": str(MAX_BYTES+1)})[0], 400)
        self.assertEqual(self.request("POST", "/upload", b"", {"Transfer-Encoding": "chunked"})[0], 400)


if __name__ == "__main__":
    unittest.main()
