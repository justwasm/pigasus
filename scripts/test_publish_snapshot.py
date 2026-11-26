"""Hermetic checks for the snapshot publisher's release workflow watcher.

Run with: python3 -m unittest discover -s scripts -p test_publish_snapshot.py
Only the watcher block is evaluated; no checkout, sync, push or API call occurs.
"""

import pathlib
import subprocess
import unittest


SCRIPT = pathlib.Path(__file__).with_name("publish-snapshot.sh")
MOCKS = r'''
WATCH=true; REPO=fixture/repo; TARGET_DIR=/fixture; TAG=v1.5.23
log_info() { :; }; log_ok() { :; }; log_warn() { :; }
log_err() { echo "$*" >&2; }
git() { printf '%s\n' fixture-sha; }
seq() { printf '1\n'; }
sleep() { :; }
gh() {
  if [[ "$1 $2" == "run list" ]]; then
    [[ "$*" == *headSha*fixture-sha* ]] || return 91
    if [[ "$MOCK_MISSING" != true ]]; then printf '%s\n' 123; fi
  elif [[ "$1 $2" == "run watch" ]]; then
    [[ "$*" == *--exit-status* ]] || return 92
    printf 'WATCHED\n'
    return "$MOCK_WATCH_EXIT"
  else
    return 93
  fi
}
'''


class ReleaseWatcherScenarios(unittest.TestCase):
    def test_watcher_outcomes(self):
        watcher = "if $WATCH; then" + SCRIPT.read_text().split("if $WATCH; then", 1)[1]
        scenarios = [
            ("successful_workflow", "true", "false", 0, 0, True),
            ("failed_workflow_propagates", "true", "false", 7, 7, True),
            ("missing_current_sha_fails_closed", "true", "true", 0, 1, False),
            ("unchanged_tag_does_not_watch_old_run", "false", "false", 0, 0, False),
        ]
        for name, pushed, missing, watch_exit, expected, watched in scenarios:
            with self.subTest(name=name):
                setup = (
                    f"TAG_PUSHED={pushed}; MOCK_MISSING={missing}; "
                    f"MOCK_WATCH_EXIT={watch_exit}\n"
                )
                result = subprocess.run(
                    ["bash", "-c", "set -euo pipefail\n" + MOCKS + setup + watcher],
                    capture_output=True,
                    text=True,
                    timeout=10,
                    check=False,
                )
                self.assertEqual(result.returncode, expected, result.stderr)
                self.assertEqual("WATCHED" in result.stdout, watched, result.stdout)


if __name__ == "__main__":
    unittest.main()
