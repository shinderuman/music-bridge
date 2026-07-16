import filecmp
import shutil
import subprocess
import time
import unicodedata
from dataclasses import dataclass
from pathlib import Path

from .models import Playlist, Track


MARKER = ".music-bridge-target"
DATA_DIR = "music-bridge"


def data_root(target: Path) -> Path:
    return target / DATA_DIR


def music_root(target: Path) -> Path:
    return data_root(target) / "Music"


def marker_path(target: Path) -> Path:
    return data_root(target) / MARKER


def format_bytes(value: int) -> str:
    amount = float(value)
    for unit in ("B", "KiB", "MiB", "GiB", "TiB"):
        if amount < 1024 or unit == "TiB":
            return f"{amount:.1f} {unit}" if unit != "B" else f"{int(amount)} B"
        amount /= 1024


@dataclass(frozen=True)
class PlannedTrack:
    track: Track
    relative: Path
    size: int


def safe_component(value: str, fallback: str = "Unknown") -> str:
    value = unicodedata.normalize("NFC", value.strip())
    value = "".join(c for c in value if c not in "/\\\0")
    return value or fallback


def plan_tracks(playlists: list[Playlist]) -> tuple[list[PlannedTrack], list[Track]]:
    planned, missing, seen = [], [], set()
    for playlist in playlists:
        for track in playlist.tracks:
            if not track.location or not track.location.is_file():
                missing.append(track)
                continue
            artist = track.album_artist or track.artist
            relative = Path(safe_component(artist)) / safe_component(track.album) / safe_component(track.location.name)
            key = str(track.location.resolve())
            if key not in seen:
                planned.append(PlannedTrack(track, relative, track.location.stat().st_size))
                seen.add(key)
    return planned, missing


def existing_size(plan: list[PlannedTrack], target: Path) -> int:
    total = 0
    for item in plan:
        destination = music_root(target) / item.relative
        if not destination.is_file() or not filecmp.cmp(item.track.location, destination, shallow=False):
            total += item.size
    return total


def validate_target(target: Path, initialize: bool = False) -> None:
    if not target.is_dir():
        raise RuntimeError(f"同期先が見つかりません: {target}")
    marker = marker_path(target)
    if not marker.exists():
        if initialize:
            marker.parent.mkdir(parents=True, exist_ok=True)
            marker.write_text("Music Bridge target\n", encoding="utf-8")
        else:
            raise RuntimeError(f"同期先確認用マーカーがありません: {marker}（初回は --init-target を指定）")


def free_bytes(target: Path) -> int:
    return shutil.disk_usage(target).free


def run_rsync(plan: list[PlannedTrack], target: Path, dry_run: bool, playlist_names: dict[Path, str] | None = None) -> None:
    total = len(plan)
    total_bytes = sum(item.size for item in plan)
    transferred_bytes = 0
    started = time.monotonic()
    for index, item in enumerate(plan, 1):
        playlist_name = (playlist_names or {}).get(item.track.location.resolve(), "")
        percent = index / total * 100 if total else 100
        label = f" | プレイリスト: {playlist_name}" if playlist_name else ""
        print(f"転送中 [{index}/{total}] {percent:5.1f}%{label} | {item.track.location.name}", end="\r", flush=True)
        destination = music_root(target) / item.relative.parent
        destination.mkdir(parents=True, exist_ok=True)
        command = ["rsync", "-ah", "--partial", "--append-verify"]
        if dry_run:
            command.append("--dry-run")
        command += [str(item.track.location), str(destination) + "/"]
        result = subprocess.run(command, text=True, capture_output=True)
        if result.returncode:
            print(flush=True)
            raise RuntimeError(f"rsyncに失敗しました（{item.track.location}）: {result.stderr.strip()}")
        transferred_bytes += item.size
        elapsed = time.monotonic() - started
        rate = transferred_bytes / elapsed if elapsed > 0 else 0
        remaining = (total_bytes - transferred_bytes) / rate if rate > 0 else 0
        minutes, seconds = divmod(int(remaining), 60)
        hours, minutes = divmod(minutes, 60)
        eta = f"{hours:02d}:{minutes:02d}:{seconds:02d}" if hours else f"{minutes:02d}:{seconds:02d}"
        print(
            f"転送中 [{index}/{total}] {percent:5.1f}%{label} | "
            f"{item.track.location.name} | ETA {eta}",
            end="\r", flush=True,
        )
    print(" " * 160, end="\r", flush=True)


def write_playlists(playlists: list[Playlist], plan: list[PlannedTrack], target: Path, dry_run: bool) -> None:
    by_source = {item.track.location.resolve(): item.relative for item in plan if item.track.location}
    for index, playlist in enumerate(playlists, 1):
        print(f"プレイリスト生成中 [{index}/{len(playlists)}] {playlist.name}", flush=True)
        lines = ["#EXTM3U"]
        for track in playlist.tracks:
            if track.location and track.location.exists() and track.location.resolve() in by_source:
                lines.append(by_source[track.location.resolve()].as_posix())
        destination = music_root(target) / f"{safe_component(playlist.name)}.m3u"
        if not dry_run:
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_text("\n".join(lines) + "\n", encoding="utf-8-sig")


def stale_playlist_files(all_playlists: list[Playlist], selected: list[Playlist], target: Path) -> list[Path]:
    selected_names = {safe_component(playlist.name) for playlist in selected}
    known_names = {safe_component(playlist.name) for playlist in all_playlists}
    playlist_dir = music_root(target)
    if not playlist_dir.is_dir():
        return []
    return sorted(
        path for path in playlist_dir.glob("*.m3u")
        if path.suffix == ".m3u" and path.stem in known_names and path.stem not in selected_names
    )


def delete_playlists(paths: list[Path]) -> None:
    for path in paths:
        path.unlink()
