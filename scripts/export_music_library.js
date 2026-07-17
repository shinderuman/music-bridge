// Music.appのプレイリストをJSONとして標準出力へ出力するJXAスクリプト。
ObjC.import("Foundation");
const automationOptions = {timeout: 3600};

function progress(message, newline) {
  const data = $("\u001b[2K\r" + message + (newline ? "\n" : "")).dataUsingEncoding($.NSUTF8StringEncoding);
  $.NSFileHandle.fileHandleWithStandardError.writeData(data);
}

function status(message, started, completed, total) {
  const elapsed = Math.floor((Date.now() - started) / 1000);
  let suffix = `（経過 ${elapsed}秒）`;
  if (total > 0) {
    suffix = `（${(completed * 100 / total).toFixed(1)}%）`;
  }
  progress(`${message}${suffix}`, false);
}

function playlistPrefix(index, total) {
  return total > 1 ? `  ${index + 1}/${total}: ` : "  ";
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

function trackProperties(track) {
  try {
    return track.properties(automationOptions);
  } catch (error) {
    return null;
  }
}

function bulkTrackProperties(playlist, sourceTracks) {
  const candidates = [
    ["playlist.tracks.properties()", () => playlist.tracks.properties(automationOptions)],
    ["sourceTracks.properties()", () => sourceTracks.properties(automationOptions)]
  ];
  for (const [method, candidate] of candidates) {
    try {
      const properties = candidate();
      if (properties && properties.length === sourceTracks.length) {
        return {properties, method};
      }
    } catch (error) {
      // Music.app/JXAのバージョンによって一括取得APIが異なるため、次へ進む。
    }
  }
  return null;
}

function waitForPlaylists(app, started) {
  const timeoutSeconds = 60;
  for (let elapsed = 0; elapsed < timeoutSeconds; elapsed++) {
    try {
      // 起動直後のMusic.appはプレイリスト数だけ返し、個々のオブジェクトはまだ
      // 取得できないことがある。全件の名前を個別に取得できるまで待機する。
      const playlists = app.userPlaylists();
      const ready = playlists.length > 0 && playlists.every((playlist) => playlist.name() !== null && playlist.name() !== undefined);
      if (ready) {
        return playlists;
      }
    } catch (error) {
      // Music.appのライブラリ初期化中は -1728 になるため、起動完了まで待つ。
    }
    status("Music.appの起動完了を待機中...", started, 0, 0);
    delay(1);
  }
  throw new Error(`Music.appの起動完了を${timeoutSeconds}秒待ちましたが、プレイリストを取得できませんでした`);
}

function run(argv) {
  argv = argv || [];
  const summary = argv.indexOf("--summary") !== -1;
  const fingerprint = argv.indexOf("--fingerprint") !== -1;
  const requested = [];
  let progressPrefix = "";
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === "--playlist" && i + 1 < argv.length) requested.push(argv[++i]);
    if (argv[i] === "--progress-prefix" && i + 1 < argv.length) progressPrefix = argv[++i];
  }
  const app = Application("Music");
  app.includeStandardAdditions = true;
  const result = [];
  const started = Date.now();
  const playlists = waitForPlaylists(app, started).filter((playlist) =>
    requested.length === 0 || requested.indexOf(playlist.name()) !== -1
  );
  status(`プレイリスト ${playlists.length}件を検出しました`, started, 0, 0);
  playlists.forEach((playlist, index) => {
    const label = progressPrefix || `${playlistPrefix(index, playlists.length)}${playlist.name()}`;
    if (summary) {
      const trackCount = readValue(() => playlist.tracks(automationOptions).length, 0);
      result.push({name: playlist.name(), trackCount});
      status(`${label}（${trackCount}曲）`, started, 0, 0);
      return;
    }
    if (fingerprint) {
      status(`${label}の曲ID一覧を取得中...`, started, 0, 0);
      const trackIDs = readValue(() => playlist.tracks.persistentID(automationOptions), []);
      result.push({name: playlist.name(), trackCount: trackIDs.length, trackIDs});
      progress(`${label}（${trackIDs.length}曲）`, true);
      return;
    }
    status(`${label}の曲一覧を取得中...`, started, 0, 0);
    let sourceTracks;
    try {
      sourceTracks = playlist.tracks(automationOptions);
    } catch (error) {
      status(`${label}の曲一覧を取得できません`, started, 0, 0);
      result.push({name: playlist.name(), tracks: []});
      return;
    }
    status(`${label} ${sourceTracks.length}曲を処理中...`, started, 0, sourceTracks.length);
    const tracks = [];
    // 大規模プレイリストを単一のproperties()呼び出しで取得すると、
    // Music.appが長時間応答を返さず、AppleEventタイムアウトにもなり得る。
    // 1,000曲を超える場合は曲単位で取得し、進捗を継続して表示する。
    const bulkResult = sourceTracks.length <= 1000
      ? bulkTrackProperties(playlist, sourceTracks)
      : null;
    const bulkProperties = bulkResult ? bulkResult.properties : null;
    const retrievalMode = bulkResult
      ? `一括取得（${bulkResult.method}）`
      : sourceTracks.length > 1000
        ? "曲ごとの取得（大規模プレイリスト）"
        : "曲ごとの取得（フォールバック）";
    status(`${label} ${retrievalMode}`, started, 0, sourceTracks.length);
    sourceTracks.forEach((track, trackIndex) => {
      if (trackIndex % 10 === 0) {
        status(`${label} ${trackIndex + 1}/${sourceTracks.length}曲`, started, trackIndex + 1, sourceTracks.length);
      }
      const properties = bulkProperties ? bulkProperties[trackIndex] : trackProperties(track);
      const value = (name, fallback) => properties
        ? readValue(() => properties[name], fallback)
        : fallback;
      const location = properties
        ? readValue(() => properties.location, null)
        : null;
      tracks.push({
        name: properties ? value("name", "") : nfc(readValue(() => track.name(), "")),
        artist: properties ? value("artist", "") : nfc(readValue(() => track.artist(), "")),
        album_artist: properties ? value("albumArtist", "") : nfc(readValue(() => track.albumArtist(), "")),
        album: properties ? value("album", "") : nfc(readValue(() => track.album(), "")),
        location: properties ? (location ? nfc(location.toString()) : null) : trackLocation(track),
      });
    });
    result.push({name: playlist.name(), tracks});
    progress(progressPrefix || `${label}（${tracks.length}曲）`, true);
  });
  return JSON.stringify(result);
}
