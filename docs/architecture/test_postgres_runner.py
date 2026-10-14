import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import check_postgres_withdraw as runner


class PostgresRunnerTests(unittest.TestCase):
    def invoke(self, execute):
        with tempfile.TemporaryDirectory() as folder:
            report = Path(folder) / "report.json"
            with patch.object(runner, "run", side_effect=execute), \
                 patch.object(runner.os.environ, "get", return_value=None), \
                 patch("sys.argv", ["probe", "--report", str(report)]), \
                 contextlib.redirect_stdout(io.StringIO()):
                code = runner.main()
            return code, json.loads(report.read_text(encoding="utf-8"))

    def test_remote_endpoint_rejected_before_creating_resources(self):
        calls = []
        def execute(args):
            calls.append(args)
            return "tcp://remote.example:2375"
        code, report = self.invoke(execute)
        self.assertEqual(code, 1)
        self.assertFalse(report["passed"])
        self.assertEqual(len(calls), 1)

    def test_startup_failure_cleans_created_owned_container_by_name(self):
        calls, marker = [], None
        def execute(args):
            nonlocal marker
            calls.append(args)
            if args[1:3] == ["context", "inspect"]:
                return "npipe:////./pipe/dockerDesktopLinuxEngine"
            if args[1:3] == ["image", "inspect"]:
                return json.dumps([{"Id": "image-id"}])
            if args[1] == "run":
                marker = args[args.index("--label") + 1].split("=", 1)[1]
                raise RuntimeError("created, but daemon failed to start")
            if args[1] == "inspect":
                return json.dumps([{"Config": {"Labels": {"ai.memorys.test": marker}}}])
            if args[1] == "rm":
                self.assertTrue(args[-1].startswith("akos-withdraw-"))
                return args[-1]
            raise AssertionError(args)
        code, report = self.invoke(execute)
        self.assertEqual(code, 1)
        self.assertTrue(report["container_removed"])
        self.assertTrue(any(args[1] == "rm" for args in calls))

    def test_cleanup_refuses_resource_with_wrong_ownership_label(self):
        calls = []
        def execute(args):
            calls.append(args)
            if args[1:3] == ["context", "inspect"]:
                return "unix:///var/run/docker.sock"
            if args[1:3] == ["image", "inspect"]:
                return json.dumps([{"Id": "image-id"}])
            if args[1] == "run":
                raise RuntimeError("startup failed")
            if args[1] == "inspect":
                return json.dumps([{"Config": {"Labels": {"ai.memorys.test": "another-owner"}}}])
            raise AssertionError(args)
        code, report = self.invoke(execute)
        self.assertEqual(code, 1)
        self.assertFalse(report["container_removed"])
        self.assertIn("ownership mismatch", report["cleanup_error"])
        self.assertFalse(any(args[1] == "rm" for args in calls))


if __name__ == "__main__":
    unittest.main()
