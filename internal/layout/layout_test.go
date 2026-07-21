package layout

import "testing"

func TestSharedStorageFormatNames(t *testing.T) {
	want := map[string]string{
		"data":             "music-bridge",
		"library":          "Library",
		"marker":           ".music-bridge-target",
		"manifest":         ".music-bridge-manifest.json",
		"pending manifest": ".music-bridge-pending-manifest",
		"artwork":          "AlbumArt.jpg",
		"partial":          ".music-bridge-partial",
	}
	got := map[string]string{
		"data":             DataDirectory,
		"library":          LibraryDirectory,
		"marker":           TargetMarker,
		"manifest":         Manifest,
		"pending manifest": PendingManifest,
		"artwork":          AlbumArtwork,
		"partial":          PartialSuffix,
	}
	for name, expected := range want {
		if got[name] != expected {
			t.Errorf("%s = %q, want %q", name, got[name], expected)
		}
	}
}
