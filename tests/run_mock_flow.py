"""Run real repository plugins through the SDK runtime, WS server and mock lwsapi."""
import argparse
import os
from pathlib import Path
import subprocess
import sys

repo = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("--sdk", type=Path, default=repo.parent / "lw-sdk")
args = parser.parse_args()
sdk = args.sdk.resolve()
assert (sdk / "tests/plugin_business.rs").is_file(), "SDK checkout is missing integration tests"
env = os.environ.copy()
env["LW_PLUGINS_REPO"] = str(repo)
env["LW_MOCK_PYTHON"] = sys.executable
env["PYTHONPATH"] = str(sdk / "sdk/python")
subprocess.run(["cargo", "test", "-p", "lw-sdk", "--test", "plugin_business",
                "--no-default-features", "--", "--nocapture"], cwd=sdk, env=env, check=True)
