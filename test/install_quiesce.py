"""Exercise the macOS updater against real temporary processes and flock locks."""
import fcntl
import importlib.util
import os
from pathlib import Path
import select
import shutil
import signal
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'tools' / 'install-local-macos.py'
spec = importlib.util.spec_from_file_location('companion_install', SCRIPT)
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)

# Use a native executable so ps shows the same argv as the real Go helper,
# including an installation path containing spaces. Signal handling is native.
HOLDER = '''
#include <fcntl.h>
#include <stdlib.h>
#include <sys/file.h>
#include <unistd.h>
int main(void) {
    int fd = open(getenv("TEST_LOCK"), O_RDWR | O_CREAT, 0600);
    if (fd < 0 || flock(fd, LOCK_EX | LOCK_NB) < 0) return 1;
    if (write(1, "ready\\n", 6) != 6) return 2;
    for (;;) pause();
}
'''


@unittest.skipUnless(sys.platform == 'darwin', 'The updater uses the macOS ps interface')
class QuiesceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.scratch = tempfile.TemporaryDirectory(prefix='companion-process-test-')
        cls.addClassCleanup(cls.scratch.cleanup)
        cls.exe = Path(cls.scratch.name) / 'Looking for Guidance.app/Contents/MacOS/LookingForGuidance'
        cls.exe.parent.mkdir(parents=True)
        subprocess.run(['cc', '-x', 'c', '-', '-o', cls.exe], input=HOLDER, text=True, check=True)
        cls.other = Path(cls.scratch.name) / 'Unrelated Companion'
        shutil.copy2(cls.exe, cls.other)

    def setUp(self):
        area = tempfile.TemporaryDirectory(dir=self.scratch.name)
        self.addCleanup(area.cleanup)
        self.support = Path(area.name)

    def spawn(self, exe, lock, *args):
        proc = subprocess.Popen([exe, *args], env={**os.environ, 'TEST_LOCK': str(self.support / lock)},
                                stdout=subprocess.PIPE)

        def stop():
            if proc.poll() is None:
                proc.kill()
            proc.wait(timeout=2)
            proc.stdout.close()

        self.addCleanup(stop)
        self.assertTrue(select.select([proc.stdout], [], [], 3)[0], 'helper did not start')
        self.assertEqual(proc.stdout.readline(), b'ready\n')
        return proc

    def locked(self, name):
        with (self.support / name).open('a+') as f:
            try:
                fcntl.flock(f, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                return True
            return False

    def test_only_watcher_stops_before_swap(self):
        watch = self.spawn(self.exe, 'watch.lock', '--watch')
        other = self.spawn(self.other, 'other.lock', '--watch')
        mcp = self.spawn(self.exe, 'mcp.lock', '--game-mcp')
        with installer.stopped_companion(self.support, self.exe):
            self.assertEqual(watch.wait(timeout=1), -signal.SIGTERM)
            self.assertTrue(self.locked('watch.lock'))
            self.assertTrue(self.locked('tray.lock'))
            self.assertIsNone(other.poll())
            self.assertIsNone(mcp.poll())
        self.assertFalse(self.locked('watch.lock'))
        self.assertFalse(self.locked('tray.lock'))

    def test_live_tray_aborts_without_signalling_watcher_or_tray(self):
        watch = self.spawn(self.exe, 'watch.lock', '--watch')
        tray = self.spawn(self.exe, 'tray.lock')
        with self.assertRaises(SystemExit):
            with installer.stopped_companion(self.support, self.exe):
                self.fail('entered swap while tray was running')
        self.assertIsNone(watch.poll())
        self.assertIsNone(tray.poll())

    def test_unknown_watcher_lock_holder_is_left_running(self):
        other = self.spawn(self.other, 'watch.lock', '--watch')
        with self.assertRaises(SystemExit):
            with installer.stopped_companion(self.support, self.exe):
                self.fail('entered swap while an unidentified process held watch.lock')
        self.assertIsNone(other.poll())
        self.assertTrue(self.locked('watch.lock'))
        self.assertFalse(self.locked('tray.lock'))

    def test_failed_swap_releases_both_locks(self):
        with self.assertRaises(RuntimeError):
            with installer.stopped_companion(self.support, self.exe):
                raise RuntimeError('simulated failed swap')
        self.assertFalse(self.locked('watch.lock'))
        self.assertFalse(self.locked('tray.lock'))


if __name__ == '__main__':
    unittest.main()
