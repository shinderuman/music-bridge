from pathlib import Path

from music_bridge.models import Playlist, Track
from music_bridge.sync import existing_size, plan_tracks, safe_component, write_playlists


def test_plan_deduplicates_same_file_and_reports_missing(tmp_path):
    song = tmp_path / "song.mp3"
    song.write_bytes(b"abc")
    track = Track("Song", "Artist", "Album", song)
    missing = Track("Missing", "Artist", "Album", tmp_path / "missing.mp3")
    plan, missing_tracks = plan_tracks([Playlist("one", (track,)), Playlist("two", (track, missing))])
    assert len(plan) == 1
    assert missing_tracks == [missing]
    assert plan[0].relative == Path("Artist/Album/song.mp3")


def test_existing_size_excludes_matching_file(tmp_path):
    song = tmp_path / "song.mp3"
    song.write_bytes(b"abc")
    plan, _ = plan_tracks([Playlist("one", (Track("Song", "Artist", "Album", song),))])
    (tmp_path / "Music/Artist/Album").mkdir(parents=True)
    (tmp_path / "Music/Artist/Album/song.mp3").write_bytes(b"abc")
    assert existing_size(plan, tmp_path) == 0


def test_playlist_uses_android_relative_paths(tmp_path):
    song = tmp_path / "song.mp3"
    song.write_bytes(b"abc")
    playlist = Playlist("My List", (Track("Song", "Artist", "Album", song),))
    plan, _ = plan_tracks([playlist])
    write_playlists([playlist], plan, tmp_path, False)
    assert (tmp_path / "Playlists/My List.m3u8").read_text() == "#EXTM3U\n../Music/Artist/Album/song.mp3\n"


def test_safe_component_removes_path_separators():
    assert safe_component(" A/B ") == "AB"
