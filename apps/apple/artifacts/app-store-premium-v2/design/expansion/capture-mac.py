#!/usr/bin/env python3
"""Capture the app's isolated, offline App Store fixtures (no UI tests)."""
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
EXECUTABLE = Path('/Users/infobrim/Library/Developer/Xcode/DerivedData/Mindwire-avbfmteozqdnqvgwlmwxqaxzkcoq/Build/Products/Debug-maccatalyst/Mindwire.app/Contents/MacOS/Mindwire')
SWIFT = '''import Foundation
import CoreGraphics
let pid = Int(CommandLine.arguments[1])!
let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String:Any]] ?? []
let matches = windows.filter { ($0[kCGWindowOwnerPID as String] as? Int) == pid && ($0[kCGWindowLayer as String] as? Int) == 0 }
print(String(data: try! JSONSerialization.data(withJSONObject: matches), encoding: .utf8)!)
'''
helper = Path('/tmp/mindwire-store-window.swift')
helper.write_text(SWIFT)
scenes = [
    ('01-onboarding-light', 'onboarding', 'light'),
    ('02-bots-light', 'assistant-sidebar', 'light'),
    ('03-task-light', 'assistant-task', 'light'),
    ('04-app-builder-light', 'assistant-build', 'light'),
    ('05-schedules-light', 'assistant-schedules', 'light'),
    ('06-accounts-light', 'assistant-account', 'light'),
    ('07-developer-dark', 'chat', 'dark'),
    ('08-review-dark', 'review', 'dark'),
]
(ROOT / 'native/mac').mkdir(parents=True, exist_ok=True)
for name, scene, theme in scenes:
    if len(sys.argv) > 1 and name not in sys.argv[1:]:
        continue
    env = {**os.environ, 'MINDWIRE_STORE_SCENE':scene, 'MINDWIRE_STORE_THEME':theme, 'MINDWIRE_STORE_STORY':'1'}
    with open('/tmp/mindwire-store-mac-preview.log', 'w') as log:
        process = subprocess.Popen([str(EXECUTABLE), '--app-store-preview'], env=env, stdout=log, stderr=log)
        try:
            time.sleep(4)
            script = f'''tell application "System Events"
tell (first process whose unix id is {process.pid})
set frontmost to true
set position of window 1 to {{80,80}}
set size of window 1 to {{1280,852}}
end tell
end tell'''
            subprocess.run(['osascript', '-e', script], check=True, capture_output=True)
            time.sleep(1)
            windows = json.loads(subprocess.check_output(['swift', str(helper), str(process.pid)]))
            window = max(windows, key=lambda w:w['kCGWindowBounds']['Width'] * w['kCGWindowBounds']['Height'])
            target = ROOT / 'native/mac' / (name + '.png')
            subprocess.run(['screencapture', '-x', '-o', '-l', str(window['kCGWindowNumber']), str(target)], check=True)
            print(json.dumps({'file':str(target), 'bounds':window['kCGWindowBounds']}), flush=True)
        finally:
            process.terminate()
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
