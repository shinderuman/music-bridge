import json
import subprocess
import time
from pathlib import Path

from .models import Playlist, load_playlists


ROOT = Path(__file__).resolve().parent.parent
JXA = ROOT / "scripts" / "export_music_library.js"


def get_playlists(source_json: Path | None = None, *, summary: bool = False, playlist_names: list[str] | None = None) -> list[Playlist]:
    if source_json:
        playlists = load_playlists(json.loads(source_json.read_text(encoding="utf-8")))
        return [p for p in playlists if not playlist_names or p.name in playlist_names]
    try:
        print("Music.appからプレイリストを取得しています（初回はAutomation許可を確認してください）...", flush=True)
        process = subprocess.Popen(
            ["osascript", "-l", "JavaScript", str(JXA)]
            + (["--summary"] if summary else [])
            + sum((["--playlist", name] for name in (playlist_names or [])), []),
            text=True, stdout=subprocess.PIPE, stderr=None,
        )
        started = time.monotonic()
        next_status = started + 10
        while process.poll() is None:
            now = time.monotonic()
            if now >= next_status:
                print(f"  取得中... {int(now - started)}秒経過", flush=True)
                next_status += 10
            time.sleep(1)
        stdout, _ = process.communicate()
        completed = subprocess.CompletedProcess(
            process.args, process.returncode, stdout, ""
        )
    except KeyboardInterrupt:
        process.terminate()
        process.wait()
        raise RuntimeError("Music.appからの取得を中断しました")
    except OSError as exc:
        raise RuntimeError("osascriptを実行できません。macOS上で実行してください。") from exc
    if completed.returncode:
        raise RuntimeError(
            "Music.appからプレイリストを取得できませんでした。"
            "直前に表示されたJXAまたはAutomation権限のエラーを確認してください。"
        )
    try:
        return load_playlists(json.loads(completed.stdout))
    except (json.JSONDecodeError, TypeError) as exc:
        raise RuntimeError(f"Music.appの取得結果がJSONではありません: {completed.stdout[:300]}") from exc
