#!/usr/bin/env python3
"""Upgrade this Mac's existing Companion bundle; install its matching signed WoW add-on."""
from contextlib import ExitStack, contextmanager
import argparse
import datetime
import fcntl
import hashlib
import json
import os
import plistlib
import re
import signal
import stat
import shutil
import subprocess
import sys
import time
import tempfile
import unicodedata
import zipfile
from pathlib import Path


def cmd(*args):
    subprocess.run(list(map(str, args)), check=True)


@contextmanager
def stopped_companion(support, exe):
    # Hold the same locks as the tray and watcher across the bundle rename. An
    # old watcher must never start a child attributed to the old signing hash.
    with ExitStack() as stack:
        tray = stack.enter_context((support / 'tray.lock').open('a+'))
        try:
            fcntl.flock(tray, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise SystemExit('Закрой Looking for Guidance через его меню перед обновлением.')
        watcher = stack.enter_context((support / 'watch.lock').open('a+'))
        try:
            fcntl.flock(watcher, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            listing = subprocess.check_output(['/bin/ps', '-U', str(os.getuid()), '-o', 'pid=,command='], text=True)
            for line in listing.splitlines():
                fields = line.strip().split(None, 1)
                if len(fields) == 2 and fields[1] == str(exe) + ' --watch':
                    try:
                        os.kill(int(fields[0]), signal.SIGTERM)
                    except ProcessLookupError:
                        pass
            deadline = time.monotonic() + 4
            while True:
                try:
                    fcntl.flock(watcher, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    if time.monotonic() >= deadline:
                        raise SystemExit('Не удалось остановить старый watcher. Обновление отменено до замены приложения.')
                    time.sleep(0.05)
        yield


def open_clients(game, installed):
    rows = subprocess.check_output(['/bin/ps', '-axo', 'command='], text=True).splitlines()
    game_exe = str(game / 'World of Warcraft.app/Contents/MacOS/World of Warcraft')
    tray_exe = str(installed / 'Contents/MacOS/LookingForGuidance')
    return (any(row.strip() == game_exe or row.strip().startswith(game_exe + ' ') for row in rows),
            any(row.strip() == tray_exe for row in rows))


def load_manifest(bundle):
    def load_paths(pairs):
        result, seen = {}, set()
        for name, digest in pairs:
            parts = name.split('/')
            normalized = unicodedata.normalize('NFC', name).casefold()
            if (any(not part or part.startswith('.') for part in parts) or
                any(ch in name for ch in ('\\', ':', '\x00')) or normalized in seen or
                not isinstance(digest, str) or not re.fullmatch('[0-9a-f]{64}', digest)):
                raise ValueError('Недопустимая запись в списке файлов аддона: ' + repr(name))
            result[name] = digest
            seen.add(normalized)
        return result

    path = bundle / 'Contents/Resources/addon-files.json'
    manifest = json.loads(path.read_text(), object_pairs_hook=load_paths)
    if not isinstance(manifest, dict) or 'looking-for-guidance.toc' not in manifest:
        raise ValueError('В списке файлов аддона отсутствует looking-for-guidance.toc.')
    return manifest


def stage_addon(bundle, target, manifest):
    target.mkdir()
    with zipfile.ZipFile(bundle / 'Contents/Resources/addon-payload.zip') as archive:
        entries = archive.infolist()
        if len(entries) != len(manifest) or {item.filename for item in entries} != set(manifest):
            raise ValueError('Файлы ZIP не соответствуют манифесту подписанного приложения.')
        for entry in entries:
            if entry.orig_filename != entry.filename or entry.is_dir() or stat.S_IFMT(entry.external_attr >> 16) not in (0, stat.S_IFREG):
                raise ValueError('ZIP содержит нестандартный тип файла: ' + repr(entry.filename))
            destination = target / entry.filename
            destination.parent.mkdir(parents=True, exist_ok=True)
            with archive.open(entry) as source, destination.open('xb') as output:
                shutil.copyfileobj(source, output)
    verify_addon(target, manifest)


def verify_addon(target, manifest):
    for name, expected in manifest.items():
        path = target / name
        if not path.is_file() or path.is_symlink() or hashlib.sha256(path.read_bytes()).hexdigest() != expected:
            raise ValueError('Контрольная сумма файла аддона не совпадает: ' + name)


def replace_pair(installed, next_app, backup_app, current_addon, new_addon, old_addon, manifest):
    # Each directory rename stays on its own volume. Unwind the completed
    # renames in reverse order on an error, including a failed read-back.
    with ExitStack() as rollback:
        def move(source, destination):
            os.rename(source, destination)
            rollback.callback(os.rename, destination, source)

        move(installed, backup_app)
        move(next_app, installed)
        if current_addon.exists():
            move(current_addon, old_addon)
        move(new_addon, current_addon)
        verify_addon(current_addon, manifest)
        rollback.pop_all()


def archive_failed_update(pending, backups):
    if not pending.exists():
        return
    log = pending / 'install.log'
    lines = log.read_text(errors='replace').splitlines() if log.is_file() else []
    terminal = ('Новое приложение установлено, но файлы WoW не подтверждены.',
                'Локальное обновление не завершилось:')
    if not lines or not lines[-1].startswith(terminal):
        raise SystemExit('Уже подготовлено обновление. Проверь ' + str(log))
    clients = subprocess.check_output(['/bin/ps', '-U', str(os.getuid()), '-o', 'command='], text=True)
    if any('install-local-macos.py --apply-pending' in row for row in clients.splitlines()):
        raise SystemExit('Предыдущий установщик ещё запущен.')
    archive = Path(tempfile.mkdtemp(prefix='previous-pending-', dir=backups)) / 'pending-local-update'
    os.rename(pending, archive)
    print('Сохранён предыдущий пакет и журнал установки:', archive, flush=True)


def quit_idle_tray(game, installed):
    running, tray = open_clients(game, installed)
    if running or not tray:
        return
    # NSRunningApplication.terminate sends a normal quit request to this bundle
    # only. No Apple Events to WoW or process-name based kill.
    script = """ObjC.import('AppKit');
    function run(args) {
        var apps = $.NSRunningApplication.runningApplicationsWithBundleIdentifier('io.github.illia-chaika.looking-for-guidance');
        for (var i = 0; i < apps.count; i++) {
            var app = apps.objectAtIndex(i);
            if (ObjC.unwrap(app.bundleURL.path) === args[0]) app.terminate;
        }
    }"""
    cmd('/usr/bin/osascript', '-l', 'JavaScript', '-e', script, installed)
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        running, tray = open_clients(game, installed)
        if running or not tray:
            return
        time.sleep(.2)
    raise SystemExit('Companion не подтвердил обычное завершение. Закрой приложение через его меню.')


def install(args, support):
    repo = Path(__file__).resolve().parents[1]
    installed = Path.home() / 'Applications/Looking for Guidance.app'
    config = json.loads((support / 'settings.json').read_text())
    game = Path(config['wowDir']).resolve(strict=True)
    if game.name != '_forever_' or 'wowf' not in (game / '.flavor.info').read_text().splitlines():
        raise SystemExit('Путь не указывает на настроенный клиент Forever.')
    pending = support / 'pending-local-update'
    built = (pending if args.apply_pending else repo / 'dist') / installed.name
    backups = support / 'local-build-backups'
    backups.mkdir(mode=0o700, exist_ok=True)
    for bundle in (built, installed):
        with (bundle / 'Contents/Info.plist').open('rb') as source:
            if plistlib.load(source)['CFBundleIdentifier'] != 'io.github.illia-chaika.looking-for-guidance':
                raise SystemExit(f'Неожиданный идентификатор {bundle}.')
    cmd('/usr/bin/codesign', '--verify', '--deep', '--strict', built)
    manifest = load_manifest(built)
    if not args.apply_pending:
        archive_failed_update(pending, backups)
    running, tray = open_clients(game, installed)
    if args.after_game and (running or (tray and not args.quit_idle_tray)):
        # Freeze the signed bundle now; later development cannot change this installation.
        pending.mkdir(mode=0o700)
        with ExitStack() as rollback:
            rollback.callback(shutil.rmtree, pending)
            cmd('/usr/bin/ditto', built, pending / installed.name)
            cmd('/usr/bin/codesign', '--verify', '--deep', '--strict', pending / installed.name)
            with (pending / 'install.log').open('w') as log:
                flags = ['--apply-pending'] + (['--quit-idle-tray'] if args.quit_idle_tray else [])
                child = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), *flags],
                                         stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            rollback.pop_all()
        print('Обновление подготовлено. Установщик дождётся выхода из WoW и Companion.', flush=True)
        print('PID:', child.pid, 'Журнал:', pending / 'install.log', flush=True)
        return
    if args.apply_pending:
        while running or (tray and not args.quit_idle_tray):
            time.sleep(3)
            running, tray = open_clients(game, installed)
    if running:
        raise SystemExit('WoW открыт; закрой игру перед установкой.')
    if tray and not args.quit_idle_tray:
        raise SystemExit('Прежде чем обновить, закрой старый Looking for Guidance через его меню.')

    add_ons = game / 'Interface/AddOns'
    current_addon = add_ons / 'looking-for-guidance'
    if add_ons.resolve(strict=True) != add_ons or current_addon.is_symlink():
        raise SystemExit('Папка аддона перенаправлена; проверь путь к настроенному клиенту.')
    with tempfile.TemporaryDirectory(prefix='.forever-update-', dir=installed.parent) as app_stage, \
         tempfile.TemporaryDirectory(prefix='.forever-update-', dir=add_ons) as addon_stage:
        next_app = Path(app_stage) / installed.name
        new_addon = Path(addon_stage) / 'new-addon'
        cmd('/usr/bin/ditto', built, next_app)
        cmd('/usr/bin/codesign', '--verify', '--deep', '--strict', next_app)
        stage_addon(next_app, new_addon, manifest)
        if args.quit_idle_tray:
            quit_idle_tray(game, installed)
        with stopped_companion(support, str(installed / 'Contents/MacOS/LookingForGuidance')):
            if any(open_clients(game, installed)):
                raise SystemExit('WoW или Companion снова открылся. Повтори установку после выхода.')
            stamp = datetime.datetime.now().strftime('%Y%m%d-%H%M%S')
            backup = Path(tempfile.mkdtemp(prefix=stamp+'-', dir=backups))
            if current_addon.is_dir():
                cmd('/usr/bin/ditto', current_addon, backup / current_addon.name)
            if any(open_clients(game, installed)):
                raise SystemExit('Клиент снова запустился во время подготовки установки.')
            replace_pair(installed, next_app, backup / installed.name, current_addon,
                         new_addon, Path(addon_stage) / 'old-addon', manifest)
    print('Предыдущая сборка и аддон:', backup)
    print('Приложение:', installed)
    print('Сверены все установленные файлы аддона:', len(manifest), flush=True)
    if args.apply_pending:
        shutil.rmtree(built)
        os.rename(pending / 'install.log', backup / 'install.log')
        pending.rmdir()
    cmd('/usr/bin/open', '-g', installed)


def main():
    options = argparse.ArgumentParser()
    options.add_argument('--after-game', action='store_true', help='Install once, after this WoW session closes')
    options.add_argument('--quit-idle-tray', action='store_true', help='Quit only the existing Companion when WoW is closed')
    options.add_argument('--apply-pending', action='store_true', help=argparse.SUPPRESS)
    args = options.parse_args()
    support = Path.home() / 'Library/Application Support/Looking for Guidance'
    with (support / 'local-installer.lock').open('a+') as lock:
        mode = fcntl.LOCK_EX if args.apply_pending else fcntl.LOCK_EX | fcntl.LOCK_NB
        try:
            fcntl.flock(lock, mode)
        except BlockingIOError:
            raise SystemExit('Одноразовый установщик уже запущен.')
        try:
            install(args, support)
        except (Exception, SystemExit) as exc:
            if args.apply_pending:
                print('Локальное обновление не завершилось:', exc, flush=True)
                raise SystemExit(1)
            raise


if __name__ == '__main__':
    main()
