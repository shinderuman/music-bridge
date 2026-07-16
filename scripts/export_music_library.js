// Music.appのプレイリストをJSONとして標準出力へ出力するJXAスクリプト。
ObjC.import("Foundation");

function progress(message, newline) {
  const data = $(message + (newline ? "\n" : "\r")).dataUsingEncoding($.NSUTF8StringEncoding);
  $.NSFileHandle.fileHandleWithStandardError.writeData(data);
}

function formatSeconds(seconds) {
  const total = Math.max(0, Math.ceil(seconds));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const remaining = total % 60;
  return hours > 0
    ? `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}:${String(remaining).padStart(2, "0")}`
    : `${String(minutes).padStart(2, "0")}:${String(remaining).padStart(2, "0")}`;
}

function status(message, started, completed, total) {
  const elapsed = Math.floor((Date.now() - started) / 1000);
  let suffix = `（経過 ${elapsed}秒）`;
  if (total > 0) {
    const remaining = completed > 0
      ? ((Date.now() - started) / 1000) * (total - completed) / completed
      : null;
    suffix = remaining === null ? "（残り --）" : `（残り ${formatSeconds(remaining)}）`;
  }
  progress(`${message}${suffix}`, false);
}

function nfc(value) {
  if (value === null || value === undefined) return "";
  return ObjC.unwrap($(value).precomposedStringWithCanonicalMapping);
}

function readValue(getter, fallback) {
  try {
    const value = getter();
    return value === null || value === undefined ? fallback : value;
  } catch (error) {
    return fallback;
  }
}

function trackLocation(track) {
  try {
    const location = track.location();
    return location ? nfc(location.toString()) : null;
  } catch (error) {
    return null;
  }
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
  const started = Date.now();
  const playlists = app.userPlaylists().filter((playlist) =>
    requested.length === 0 || requested.indexOf(playlist.name()) !== -1
  );
  status(`プレイリスト ${playlists.length}件を検出しました`, started, 0, 0);
  playlists.forEach((playlist, index) => {
    if (summary) {
      const trackCount = readValue(() => playlist.tracks().length, 0);
      result.push({name: playlist.name(), trackCount});
      status(`  ${index + 1}/${playlists.length}: ${playlist.name()}（${trackCount}曲）`, started, 0, 0);
      return;
    }
    status(`  ${index + 1}/${playlists.length}: ${playlist.name()}の曲一覧を取得中...`, started, 0, 0);
    let sourceTracks;
    try {
      sourceTracks = playlist.tracks();
    } catch (error) {
      status(`  ${index + 1}/${playlists.length}: ${playlist.name()}の曲一覧を取得できません`, started, 0, 0);
      result.push({name: playlist.name(), tracks: []});
      return;
    }
    status(`  ${index + 1}/${playlists.length}: ${sourceTracks.length}曲を処理中...`, started, 0, sourceTracks.length);
    const tracks = [];
    sourceTracks.forEach((track, trackIndex) => {
      if (trackIndex % 10 === 0) {
        status(`  ${index + 1}/${playlists.length}: ${playlist.name()} ${trackIndex + 1}/${sourceTracks.length}曲`, started, trackIndex + 1, sourceTracks.length);
      }
      tracks.push({
        name: nfc(readValue(() => track.name(), "")),
        artist: nfc(readValue(() => track.artist(), "")),
        album_artist: nfc(readValue(() => track.albumArtist(), "")),
        album: nfc(readValue(() => track.album(), "")),
        location: trackLocation(track),
        duration: readValue(() => track.duration(), null)
      });
    });
    result.push({name: playlist.name(), tracks});
    status(`  ${index + 1}/${playlists.length}: ${playlist.name()}（${tracks.length}曲）`, started, tracks.length, tracks.length);
  });
  progress(`取得完了（経過 ${Math.floor((Date.now() - started) / 1000)}秒）`, true);
  return JSON.stringify(result);
}
