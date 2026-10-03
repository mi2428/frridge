"""Run with python3 tests/test_mp_delete.py; never calls real Multipass."""

import os
from pathlib import Path
import subprocess
import tempfile


repo = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(dir=os.environ.get("TMPDIR")) as directory:
    directory = Path(directory)
    calls = directory / "calls"
    multipass = directory / "multipass"
    multipass.write_text(
        '#!/bin/sh\nprintf "%s\\n" "$@" >> "$CALLS"\nexit "$FAKE_EXIT"\n'
    )
    multipass.chmod(0o755)
    env = dict(os.environ, PATH=f"{directory}:{os.environ['PATH']}", CALLS=str(calls))
    for exit_code in (0, 42):
        calls.write_text("")
        result = subprocess.run(
            ["make", "--no-print-directory", "mp-delete", "MP_NAME=audit-target"],
            cwd=repo,
            env=dict(env, FAKE_EXIT=str(exit_code)),
            capture_output=True,
            text=True,
        )
        assert calls.read_text().splitlines() == ["delete", "--purge", "audit-target"]
        assert (result.returncode == 0) == (exit_code == 0), result.stderr
print("mp-delete: targeted deletion and failure propagation passed")
