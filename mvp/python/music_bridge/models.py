from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Track:
    name: str
    artist: str
    album: str
    location: Path | None
    duration: float | None = None
    album_artist: str | None = None


@dataclass(frozen=True)
class Playlist:
    name: str
    tracks: tuple[Track, ...]


def load_playlists(data: list[dict]) -> list[Playlist]:
    result = []
    for item in data:
        tracks = []
        raw_tracks = item.get("tracks")
        if raw_tracks is None:
            raw_tracks = [{}] * int(item.get("trackCount", 0))
        for raw in raw_tracks:
            location = raw.get("location") or raw.get("path")
            tracks.append(Track(
                name=str(raw.get("name", "")),
                artist=str(raw.get("artist", "")),
                album=str(raw.get("album", "")),
                location=Path(location).expanduser() if location else None,
                duration=float(raw["duration"]) if raw.get("duration") is not None else None,
                album_artist=str(raw["album_artist"]) if raw.get("album_artist") else None,
            ))
        result.append(Playlist(str(item.get("name", "")), tuple(tracks)))
    return result
