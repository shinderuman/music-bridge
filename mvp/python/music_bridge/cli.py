import argparse
from pathlib import Path

from .source import get_playlists
from .sync import (
    data_root, delete_playlists, existing_size, format_bytes, free_bytes, marker_path, music_root, plan_tracks,
    run_rsync, stale_playlist_files, validate_target, write_playlists,
)
from .ui import select_many, select_one


def command_playlists(args):
    for playlist in get_playlists(args.source_json, summary=True):
        print(f"{playlist.name}\t{len(playlist.tracks)}曲")


def command_sync(args):
    target = Path(args.target).expanduser() if args.target else None
    if target is None:
        volumes = [path for path in Path("/Volumes").glob("*") if path.is_dir()]
        if not volumes:
            raise RuntimeError("/Volumesに同期先候補がありません。--targetを指定してください")
        target = select_one(volumes, "同期先を選択してください", [str(volume) for volume in volumes])
    if not marker_path(target).exists() and not args.init_target:
        answer = input(
            f"{target}にはMusic Bridgeのマーカーがありません。"
            "テスト用同期先として初期化しますか？ [y/N] "
        ).strip().lower()
        if answer != "y":
            raise RuntimeError("同期先の初期化をキャンセルしました")
        args.init_target = True
    validate_target(target, args.init_target)
    summaries = get_playlists(args.source_json, summary=True)
    selected_summaries = select_many(
        summaries,
        "プレイリストを選択してください",
        [f"{playlist.name} ({len(playlist.tracks)}曲)" for playlist in summaries],
    )
    playlists = get_playlists(
        args.source_json,
        playlist_names=[playlist.name for playlist in selected_summaries],
    )
    stale = stale_playlist_files(summaries, selected_summaries, target)
    if stale:
        print("警告: 選択されなかったプレイリストのM3U8を削除します:")
        for path in stale:
            print(f"  削除: {path}")
        if not args.yes and input("削除して続行しますか？ [y/N] ").strip().lower() != "y":
            raise RuntimeError("プレイリスト削除をキャンセルしました")
    plan, missing = plan_tracks(playlists)
    required = existing_size(plan, target)
    free = free_bytes(target)
    print(f"選択プレイリスト: {len(playlists)}件 / 曲: {sum(len(p.tracks) for p in playlists)}曲")
    print(f"新規転送容量: {format_bytes(required)} / 空き容量: {format_bytes(free)}")
    if missing:
        print(f"ローカルファイルなし: {len(missing)}曲")
    if required > free:
        answer = input("容量が不足しています。中断するには abort、空き容量の範囲で続行するには continue: ").strip().lower()
        if answer != "continue": raise RuntimeError("容量不足のため中断しました")
        available, fitting = free, []
        for item in plan:
            if item.size <= available or (music_root(target) / item.relative).is_file():
                fitting.append(item); available -= item.size
        plan = fitting
    elif not args.yes and input("同期を開始しますか？ [y/N] ").strip().lower() != "y":
        raise RuntimeError("ユーザーにより中断しました")
    if args.dry_run:
        print("dry-run: rsyncとプレイリスト生成内容を確認します")
    playlist_names = {}
    for playlist in playlists:
        for track in playlist.tracks:
            if track.location:
                playlist_names.setdefault(track.location.resolve(), playlist.name)
    run_rsync(plan, target, args.dry_run, playlist_names)
    write_playlists(playlists, plan, target, args.dry_run)
    if not args.dry_run:
        delete_playlists(stale)
    print(f"転送完了: {len(plan)}/{len(plan)}曲")
    print(f"同期完了: {len(playlists)}プレイリスト")


def main(argv=None):
    parser = argparse.ArgumentParser(prog="music-bridge")
    sub = parser.add_subparsers(dest="command", required=True)
    for name in ("playlists", "sync"):
        command = sub.add_parser(name)
        command.add_argument("--source-json", type=Path, help="Music.app取得結果のJSON（テスト用）")
        if name == "sync":
            command.add_argument("--target")
            command.add_argument("--init-target", action="store_true")
            command.add_argument("--dry-run", action="store_true")
            command.add_argument("--yes", action="store_true")
            command.set_defaults(func=command_sync)
        else: command.set_defaults(func=command_playlists)
    args = parser.parse_args(argv)
    try: args.func(args)
    except (RuntimeError, IndexError, OSError) as exc:
        parser.error(str(exc))


if __name__ == "__main__":
    main()
