// Package layout defines the on-device format shared by drive and Android sync.
package layout

const (
	DataDirectory         = "music-bridge"
	LibraryDirectory      = "Library"
	TargetMarker          = ".music-bridge-target"
	Manifest              = ".music-bridge-manifest.json"
	PendingManifest       = ".music-bridge-pending-manifest"
	LibraryManifestMarker = ".music-bridge-library-indexed"
	AlbumArtwork          = "AlbumArt.jpg"
	PartialSuffix         = ".music-bridge-partial"
	PartialMetadataSuffix = ".meta"
	SyncLock              = ".music-bridge-sync.lock"
)
