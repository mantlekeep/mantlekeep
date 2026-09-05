"""Drive the REAL door from Python — build the Go binary, run it, submit through it.

This is the test that matters: a mock proves the client agrees with itself; only the running
door proves the client agrees with the CONTRACT. It builds ``mantlekeep``, starts ``serve``
with dev-login identity on a free port, and submits an allow, a policy deny, and a validation
deny — asserting the rich Decision each returns.

Skipped automatically if the Go toolchain is absent, so ``test_decision_parse`` still runs
anywhere; this one needs Go.
"""

import os
import shutil
import socket
import subprocess
import time
import unittest
import urllib.error
import urllib.request

from mantlekeep import DoorConfig, GovernedWorker, Intent, ServiceDoorClient
from mantlekeep.door.errors import DecisionError

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
CONTROL_DIR = os.path.join(REPO_ROOT, "mantlekeep-control")
CALLER_HEADER = "X-Mantlekeep-User"


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


def _wait_until_serving(url: str, deadline_seconds: float = 15.0) -> None:
    end = time.monotonic() + deadline_seconds
    while time.monotonic() < end:
        try:
            urllib.request.urlopen(url, timeout=1.0)
            return
        except urllib.error.HTTPError:
            return  # any HTTP answer means the server is up
        except (urllib.error.URLError, ConnectionError, OSError):
            time.sleep(0.2)
    raise RuntimeError(f"door did not start listening at {url}")


@unittest.skipIf(shutil.which("go") is None, "Go toolchain not available")
class AgainstRunningDoorTest(unittest.TestCase):
    process = None
    binary = None

    @classmethod
    def setUpClass(cls):
        cls.binary = os.path.join(CONTROL_DIR, "mk-serve-test.bin")
        build = subprocess.run(
            ["go", "build", "-o", cls.binary, "./cmd/mantlekeep"],
            cwd=CONTROL_DIR,
            capture_output=True,
            text=True,
        )
        if build.returncode != 0:
            raise RuntimeError(f"go build failed:\n{build.stderr}")

        cls.port = _free_port()
        env = dict(os.environ)
        env["MANTLEKEEP_ADDR"] = f":{cls.port}"
        env["MANTLEKEEP_USER_HEADER"] = CALLER_HEADER
        env["MANTLEKEEP_DEV_LOGIN"] = "true"
        env["MANTLEKEEP_AUDIT_PATH"] = os.path.join(CONTROL_DIR, "audit-test.db")
        cls.process = subprocess.Popen(
            [cls.binary, "serve"],
            cwd=CONTROL_DIR,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
        cls.base_url = f"http://127.0.0.1:{cls.port}"
        _wait_until_serving(cls.base_url + "/api/audit")

    @classmethod
    def tearDownClass(cls):
        if cls.process:
            cls.process.terminate()
            try:
                cls.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                cls.process.kill()
            if cls.process.stdout:
                cls.process.stdout.close()
        for path in (cls.binary, os.path.join(CONTROL_DIR, "audit-test.db")):
            if path and os.path.exists(path):
                os.remove(path)

    def _client(self) -> ServiceDoorClient:
        return ServiceDoorClient(DoorConfig(base_url=self.base_url, caller_header=CALLER_HEADER))

    def test_allow_returns_a_token_and_policy(self):
        # 'root' is the engine-baked superadmin — a clean allow.
        decision = self._client().submit(
            Intent(action="job.run", resource="project/demo", goal="ship it", subject="root")
        )
        self.assertTrue(decision.allowed, decision)
        self.assertTrue(decision.token, "an allow must carry a token")
        self.assertTrue(decision.policy_id, "an allow must name the policy")
        self.assertTrue(decision.expires_at, "an allow must say when it expires")

    def test_policy_deny_is_typed(self):
        # 'dev-alice' holds only a consumer role; the core ships no grant for job.run.
        decision = self._client().submit(
            Intent(action="job.run", resource="project/demo", goal="try it", subject="dev-alice")
        )
        self.assertFalse(decision.allowed)
        self.assertEqual(decision.outcome, "deny")
        self.assertEqual(decision.denial_code, "DENY_ACTION_NOT_ALLOWED", decision)

    def test_missing_goal_is_a_validation_deny(self):
        decision = self._client().submit(
            Intent(action="job.run", resource="project/demo", goal="", subject="root")
        )
        self.assertFalse(decision.allowed)
        self.assertEqual(decision.denial_code, "DENY_VALIDATION", decision)

    def test_governed_worker_does_not_run_work_on_a_deny(self):
        worker = GovernedWorker(self._client())
        ran = {"executed": False}

        def effect(_token):
            ran["executed"] = True
            return "done"

        # Built outside the assertRaises block: only worker.run is under test, so a
        # DecisionError from anywhere else cannot be mistaken for the deny.
        denied = Intent(action="job.run", resource="project/demo", goal="x", subject="dev-alice")

        # A denied action must never execute the work — the whole point of decide-then-dispatch.
        with self.assertRaises(DecisionError):
            worker.run(denied, effect)
        self.assertFalse(ran["executed"], "work ran despite a deny — governance was bypassed")

    def test_governed_worker_runs_work_on_allow(self):
        worker = GovernedWorker(self._client())
        result = worker.run(
            Intent(action="job.run", resource="project/demo", goal="ship", subject="root"),
            lambda token: f"executed under {token}",
        )
        self.assertIn("executed under", result)


if __name__ == "__main__":
    unittest.main()
