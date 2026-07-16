# Music Bridge

MacのMusic.appで管理しているローカル音源とプレイリストを、Android用microSDXCへ同期するCLIです。

## セットアップ

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -e .
```

## 使い方

```bash
music-bridge playlists
music-bridge sync --target /Volumes/MUSIC_SD
music-bridge sync --target /Volumes/MUSIC_SD --dry-run
```

同期データはボリューム直下の `music-bridge/Music` に配置されます。プレイリストは音源と同じディレクトリに`.m3u`形式で配置し、iSyncr互換の相対パスを使用します。Android側ではボリューム全体ではなく、この `music-bridge/Music` ディレクトリを読み取り先に指定してください。

安全のため、同期先の `music-bridge` に `.music-bridge-target` が必要です。初回は対象ボリュームを明示して `--init-target` を付けてマーカーを作成してください。

```bash
music-bridge sync --target /Volumes/MUSIC_SD --init-target
```

Music.appのAutomation許可が必要です。プレイリスト取得結果をテスト・確認する場合は `--source-json` でJSONファイルを指定できます。
