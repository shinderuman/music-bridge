# Music Bridge

MacのMusic.appのローカル音源をAndroid向けストレージへ同期するCLIです。

## 実装

- `mvp/python`: 実機確認済みのPython MVP
- `alpha/go`: Go版α実装

Go版はリポジトリルートから実行します。

```bash
go run ./alpha/go/cmd/music-bridge playlists
go run ./alpha/go/cmd/music-bridge sync --target /Volumes/CodexVault
```
