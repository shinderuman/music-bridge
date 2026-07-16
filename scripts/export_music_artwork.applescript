on run argv
	set artworkDir to item 1 of argv
	set progressPath to item 2 of argv
	set requestPath to item 3 of argv
	set totalTracks to item 4 of argv as integer
	set requestedNames to items 5 thru -1 of argv
	set requestedTracks to paragraphs of (read POSIX file requestPath)
	set selectedPlaylistIndex to 0
	set completedTracks to 0
	my writeProgress(progressPath, completedTracks, totalTracks)
	tell application "Music"
		repeat with playlistRef in every user playlist
			set currentPlaylist to contents of playlistRef
			set playlistName to name of currentPlaylist
			if playlistName is in requestedNames then
				set selectedPlaylistIndex to selectedPlaylistIndex + 1
				set playlistTracks to every file track of currentPlaylist
				repeat with trackIndex from 1 to count of playlistTracks
					set currentTrack to item trackIndex of playlistTracks
					set requestKey to (selectedPlaylistIndex as text) & ":" & (trackIndex as text)
					if requestKey is in requestedTracks then
						if (count of artworks of currentTrack) > 0 then
							set artworkData to data of artwork 1 of currentTrack
							set outputPath to artworkDir & "/" & selectedPlaylistIndex & "-" & trackIndex & ".jpg"
							set outputFile to open for access POSIX file outputPath with write permission
							set eof outputFile to 0
							write artworkData to outputFile
							close access outputFile
						end if
						set completedTracks to completedTracks + 1
						my writeProgress(progressPath, completedTracks, totalTracks)
					end if
				end repeat
			end if
		end repeat
	end tell
	return ""
end run

on writeProgress(progressPath, completedTracks, totalTracks)
	set progressFile to open for access POSIX file progressPath with write permission
	set eof progressFile to 0
	write (completedTracks as text) & "/" & (totalTracks as text) to progressFile
	close access progressFile
end writeProgress
