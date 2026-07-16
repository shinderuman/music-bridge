// Music.appのプレイリストをJSONとして標準出力へ出力するJXAスクリプト。
ObjC.import("Foundation");

function progress(message) {
  const data = $(message + "\n").dataUsingEncoding($.NSUTF8StringEncoding);
  $.NSFileHandle.fileHandleWithStandardError.writeData(data);
}

function nfc(value) {
  if (value === null || value === undefined) return "";
  return ObjC.unwrap($(value).precomposedStringWithCanonicalMapping);
}

function run(argv) {
  argv = argv || [];
  const summary = argv.indexOf("--summary") !== -1;
  const requested = [];
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--playlist" && i + 1 < argv.length) requested.push(argv[++i]);
  }
  const app = Application("Music");
  app.includeStandardAdditions = true;
  const result = [];
  const playlists = app.userPlaylists().filter((playlist) =>
    requested.length === 0 || requested.indexOf(playlist.name()) !== -1
  );
  progress(`プレイリスト ${playlists.length}件を検出しました`);
  playlists.forEach((playlist, index) => {
    if (summary) {
      const trackCount = playlist.tracks().length;
      result.push({name: playlist.name(), trackCount});
      progress(`  ${index + 1}/${playlists.length}: ${playlist.name()}（${trackCount}曲）`);
      return;
    }
    progress(`  ${index + 1}/${playlists.length}: ${playlist.name()}の曲一覧を取得中...`);
    const sourceTracks = playlist.tracks();
    progress(`  ${index + 1}/${playlists.length}: ${sourceTracks.length}曲を処理中...`);
    const tracks = [];
    sourceTracks.forEach((track, trackIndex) => {
      if (trackIndex % 100 === 0) {
        progress(`    ${playlist.name()}: ${trackIndex + 1}/${sourceTracks.length}曲`);
      }
      tracks.push({
        name: nfc(track.name()),
        artist: nfc(track.artist()),
        album_artist: nfc(track.albumArtist()),
        album: nfc(track.album()),
        location: track.location() ? nfc(track.location().toString()) : null,
        duration: track.duration()
      });
    });
    result.push({name: playlist.name(), tracks});
    progress(`  ${index + 1}/${playlists.length}: ${playlist.name()}（${tracks.length}曲）`);
  });
  return JSON.stringify(result);
}
