import unittest

from stats_window import rates, traffic


class StatsWindowTest(unittest.TestCase):
    def test_exact_identity_and_common_interval(self):
        response = {"stat": [{"name": "user>>>a>>>traffic>>>uplink", "value": "2500000"},
                             {"name": "user>>>a>>>traffic>>>downlink", "value": "5000000"},
                             {"name": "user>>>another>>>traffic>>>uplink", "value": "999"}]}
        values = traffic(response, "a")
        self.assertEqual(rates({"uplink": 0, "downlink": 0}, values, 2), {"uplink": 10, "downlink": 20})

    def test_missing_and_reset_counters_fail(self):
        with self.assertRaises(ValueError):
            traffic({"stat": []}, "a")
        with self.assertRaises(ValueError):
            rates({"uplink": 10}, {"uplink": 0}, 1)


if __name__ == "__main__":
    unittest.main()
