package drive

import "music-bridge/internal/portable"

func portablePathKey(value string) string {
	return portable.Key(value)
}

// exFAT上のUnicodeファイル名はos.ReadDirではNFDに見えても、変更操作では
// NFCまたはNFDの片方だけを受け付けることがある。変更対象の解決を一か所に集約する。
func portableMutationPath(value string) string {
	return portable.MutationPath(value)
}

func removePortablePath(value string) error {
	return portable.Remove(value)
}

func isAppleDoublePath(value string) bool {
	return portable.IsAppleDouble(value)
}

// macOSはFAT/exFATで使えない文字をApple互換の私用文字へ変換して保存する。
// Androidから同じボリュームを見ると変換後の文字がそのまま見えるため、
// Androidへ直接同期するパスとM3U内の参照も同じ表現へ揃える。
func androidVisiblePath(value string) string {
	return portable.AndroidVisible(value)
}

func logicalPathFromAndroid(value string) string {
	return portable.LogicalFromAndroid(value)
}
