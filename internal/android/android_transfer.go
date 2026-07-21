package android

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"music-bridge/internal/layout"
)

const androidPartialSuffix = layout.PartialSuffix
const androidPartialMetaSuffix = layout.PartialMetadataSuffix
const androidPendingManifest = pendingManifest

var androidChunkSize int64 = 8 << 20

// Music.appへの問い合わせやローカルでの計画作成中もWireless debuggingの
// 切断を早期検知できるよう、端末まで届く軽量なshellコマンドを定期送信する。
var androidConnectionCheckInterval = 15 * time.Second

type androidFileState struct {
	Size    int64
	ModTime int64
	Hash    string
}

type androidTransferBackend interface {
	PartialState(path string) (int64, string, error)
	PreparePartial(path, signature string) error
	ResetPartial(path string) error
	MakeDir(path string) error
	Append(path string, input io.Reader) error
	Finalize(partial, destination string, modTime int64, managedRelative string) error
	Remove(path string) error
}

type adbAndroidBackend struct {
	serial androidSerial
	root   string
}

func (backend adbAndroidBackend) PartialState(filePath string) (int64, string, error) {
	metaPath := filePath + androidPartialMetaSuffix
	command := "if [ -f " + shellQuote(filePath) + " ]; then stat -c %s " +
		shellQuote(filePath) + "; else echo -1; fi; if [ -f " + shellQuote(metaPath) +
		" ]; then cat " + shellQuote(metaPath) + "; else echo -; fi"
	out, err := adbShell(backend.serial(), command)
	if err != nil {
		return 0, "", adbError("Android上の部分ファイルを確認できませんでした", out, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return 0, "", fmt.Errorf("Android上の部分ファイル情報を解釈できませんでした: %q", strings.TrimSpace(string(out)))
	}
	size, err := strconv.ParseInt(strings.TrimSpace(lines[0]), 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("Android上のファイルサイズを解釈できませんでした: %q", strings.TrimSpace(lines[0]))
	}
	signature := strings.TrimSpace(lines[1])
	if signature == "-" {
		signature = ""
	}
	return size, signature, nil
}

func (backend adbAndroidBackend) PreparePartial(filePath, signature string) error {
	return adbWrite(backend.serial(), filePath+androidPartialMetaSuffix, []byte(signature+"\n"))
}

func (backend adbAndroidBackend) ResetPartial(filePath string) error {
	out, err := adbShell(backend.serial(), "rm -f "+shellQuote(filePath)+" "+
		shellQuote(filePath+androidPartialMetaSuffix))
	if err != nil {
		return adbError("Android上の古い部分ファイルを削除できませんでした", out, err)
	}
	return nil
}

func (backend adbAndroidBackend) MakeDir(directory string) error {
	out, err := adbShell(backend.serial(), "mkdir -p "+shellQuote(directory))
	if err != nil {
		return adbError("Android上にディレクトリを作成できませんでした", out, err)
	}
	return nil
}

func (backend adbAndroidBackend) Append(filePath string, input io.Reader) error {
	out, err := adbInput(backend.serial(), input, "shell", "-T", "cat >> "+shellQuote(filePath))
	if err != nil {
		return adbError("Androidへの転送が中断されました", out, err)
	}
	return nil
}

func (backend adbAndroidBackend) Finalize(partial, destination string, modTime int64, managedRelative string) error {
	journal := path.Join(backend.root, androidPendingManifest)
	command := "if [ -f " + shellQuote(partial) + " ]; then mv " + shellQuote(partial) + " " +
		shellQuote(destination) + "; fi && touch -d @" + strconv.FormatInt(modTime, 10) + " " +
		shellQuote(destination) + " && rm -f " + shellQuote(partial+androidPartialMetaSuffix) +
		" && printf '%s\\n' " + shellQuote(managedRelative) + " >> " +
		shellQuote(journal)
	out, err := adbShell(backend.serial(), command)
	if err != nil {
		return adbError("Android上の転送済みファイルを確定できませんでした", out, err)
	}
	return nil
}

func (backend adbAndroidBackend) Remove(filePath string) error {
	out, err := adbShell(backend.serial(), "rm -f "+shellQuote(filePath))
	if err != nil {
		return adbError("Android上のファイルを削除できませんでした", out, err)
	}
	return nil
}

type androidReconnectWait func(context.Context, error) error

func withAndroidConnectionMonitor(
	ctx context.Context,
	serial androidSerial,
	wait androidReconnectWait,
	operation func() error,
) error {
	monitorContext, cancel := context.WithCancel(ctx)
	monitorDone := make(chan error, 1)
	go func() {
		monitorDone <- monitorAndroidConnection(monitorContext, serial, wait)
	}()

	operationErr := operation()
	cancel()
	monitorErr := <-monitorDone
	if operationErr != nil {
		return operationErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if monitorErr != nil && !errors.Is(monitorErr, context.Canceled) {
		return monitorErr
	}
	return nil
}

func monitorAndroidConnection(ctx context.Context, serial androidSerial, wait androidReconnectWait) error {
	ticker := time.NewTicker(androidConnectionCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			for {
				out, err := adbOutput(serial(), "shell", "-T", ":")
				if err == nil {
					break
				}
				probeErr := adbError("Androidとの接続確認に失敗しました", out, err)
				if !isRetryableADBError(probeErr) {
					return probeErr
				}
				if err := wait(ctx, probeErr); err != nil {
					return err
				}
			}
		}
	}
}

func copyAndroidFile(
	ctx context.Context,
	backend androidTransferBackend,
	source, destination, managedRelative string,
	wait androidReconnectWait,
	progress func(int64),
) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("転送元が通常ファイルではありません: %s", source)
	}
	if err := retryAndroidOperation(ctx, func() error {
		return backend.MakeDir(path.Dir(destination))
	}, wait); err != nil {
		return err
	}
	partial := destination + androidPartialSuffix
	signature := fmt.Sprintf("%d:%d", info.Size(), info.ModTime().Unix())
	for {
		var offset int64
		var partialSignature string
		if err := retryAndroidOperation(ctx, func() error {
			var sizeErr error
			offset, partialSignature, sizeErr = backend.PartialState(partial)
			return sizeErr
		}, wait); err != nil {
			return err
		}
		if offset < 0 {
			offset = 0
		}
		if (offset > 0 && partialSignature != signature) || offset > info.Size() {
			if err := retryAndroidOperation(ctx, func() error {
				return backend.ResetPartial(partial)
			}, wait); err != nil {
				return err
			}
			offset = 0
			partialSignature = ""
		}
		if partialSignature != signature {
			if err := retryAndroidOperation(ctx, func() error {
				return backend.PreparePartial(partial, signature)
			}, wait); err != nil {
				return err
			}
		}
		if progress != nil {
			progress(offset)
		}
		if offset == info.Size() {
			return retryAndroidOperation(ctx, func() error {
				return backend.Finalize(partial, destination, info.ModTime().Unix(), managedRelative)
			}, wait)
		}

		file, err := os.Open(source)
		if err != nil {
			return err
		}
		remaining := info.Size() - offset
		length := androidChunkSize
		if remaining < length {
			length = remaining
		}
		reader := io.NewSectionReader(file, offset, length)
		appendErr := backend.Append(partial, reader)
		closeErr := file.Close()
		if appendErr == nil && closeErr != nil {
			return closeErr
		}
		if appendErr != nil {
			if !isRetryableADBError(appendErr) {
				return appendErr
			}
			if err := wait(ctx, appendErr); err != nil {
				return err
			}
		}
	}
}

func retryAndroidOperation(ctx context.Context, operation func() error, wait androidReconnectWait) error {
	for {
		err := operation()
		if err == nil {
			return nil
		}
		if !isRetryableADBError(err) {
			return err
		}
		if err := wait(ctx, err); err != nil {
			return err
		}
	}
}

func isRetryableADBError(err error) bool {
	if err == nil {
		return false
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 255 {
		// adb shellは転送路が途中で切れた場合、詳細を出さず255だけを返すことがある。
		return true
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "adb: device") && strings.Contains(message, "not found") {
		// mDNS名を含むエラーは "device '<serial>' not found" となり、
		// "device not found" という連続した文言にはならない。
		return true
	}
	for _, fragment := range []string{
		"device offline",
		"device not found",
		"no devices/emulators found",
		"closed",
		"connection reset",
		"connection refused",
		"failed to connect",
		"protocol fault",
		"transport error",
		"transport is closing",
		"more than one device",
		"unauthorized",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func androidInventory(serial, root string) (map[string]androidFileState, error) {
	command := androidInventoryCommand(root)
	out, err := adbShell(serial, command)
	if err != nil {
		return nil, adbError("Android上の既存ファイルを確認できませんでした", out, err)
	}
	inventory := parseAndroidInventory(string(out), root)
	hashOut, err := adbShell(serial, androidPlaylistHashCommand(root))
	if err != nil {
		return nil, adbError("Android上のプレイリストを確認できませんでした", hashOut, err)
	}
	for relative, hash := range parseAndroidPlaylistHashes(string(hashOut), root) {
		state, exists := inventory[relative]
		if !exists {
			continue
		}
		state.Hash = hash
		inventory[relative] = state
	}
	return inventory, nil
}

func androidInventoryWithProgress(
	serial, root string,
	progress func(int),
) (map[string]androidFileState, int, error) {
	command := androidInventoryCommand(root)
	reader, wait, streaming, err := adbStreamOutput(serial, "shell", command)
	if err != nil {
		return nil, 0, adbError("Android上の既存ファイルを確認できませんでした", nil, err)
	}
	if !streaming {
		inventory, err := androidInventory(serial, root)
		if err == nil && progress != nil {
			progress(len(inventory))
		}
		return inventory, len(inventory), err
	}
	defer reader.Close()
	inventory, count, parseErr := parseAndroidInventoryStream(reader, root, progress)
	stderr, waitErr := wait()
	if waitErr != nil {
		return nil, count, adbError("Android上の既存ファイルを確認できませんでした", stderr, waitErr)
	}
	if parseErr != nil {
		return nil, count, fmt.Errorf("Android上の既存ファイル情報を解釈できませんでした: %w", parseErr)
	}
	hashOut, err := adbShell(serial, androidPlaylistHashCommand(root))
	if err != nil {
		return nil, count, adbError("Android上のプレイリストを確認できませんでした", hashOut, err)
	}
	for relative, hash := range parseAndroidPlaylistHashes(string(hashOut), root) {
		state, exists := inventory[relative]
		if !exists {
			continue
		}
		state.Hash = hash
		inventory[relative] = state
	}
	return inventory, count, nil
}

func parseAndroidInventoryStream(
	input io.Reader,
	root string,
	progress func(int),
) (map[string]androidFileState, int, error) {
	result := map[string]androidFileState{}
	prefix := strings.TrimSuffix(root, "/") + "/"
	reader := bufio.NewReader(input)
	count := 0
	for {
		filePath, err := reader.ReadString(0)
		if errors.Is(err, io.EOF) && filePath == "" {
			return result, count, nil
		}
		if err != nil {
			return nil, count, err
		}
		sizeText, err := reader.ReadString(0)
		if err != nil {
			return nil, count, err
		}
		modTimeText, err := reader.ReadString(0)
		if err != nil {
			return nil, count, err
		}
		count++
		if progress != nil {
			progress(count)
		}
		filePath = strings.TrimSuffix(filePath, "\x00")
		sizeText = strings.TrimSuffix(sizeText, "\x00")
		modTimeText = strings.TrimSuffix(modTimeText, "\x00")
		if !strings.HasPrefix(filePath, prefix) {
			continue
		}
		size, sizeErr := strconv.ParseInt(sizeText, 10, 64)
		modTimeFloat, modErr := strconv.ParseFloat(modTimeText, 64)
		if sizeErr != nil || modErr != nil {
			continue
		}
		result[strings.TrimPrefix(filePath, prefix)] = androidFileState{
			Size:    size,
			ModTime: int64(modTimeFloat),
		}
	}
}

func androidInventoryCommand(root string) string {
	// find -exec ... {} + はAndroidのtoybox findが非常に多くの長いパスを
	// 一度にshへ渡し、ARG_MAXを超えることがある。find自身の-printfなら
	// 子プロセスも引数リストも作らず、ファイル数にかかわらず列挙できる。
	return "if [ -d " + shellQuote(root) + " ]; then find " + shellQuote(root) +
		` -type f -printf '%p\0%s\0%T@\0'; fi`
}

func androidPlaylistHashCommand(root string) string {
	return "for file in " + shellQuote(root) + "/*.m3u; do " +
		"[ -f \"$file\" ] || continue; " +
		"hash=$(sha256sum \"$file\") || exit 1; " +
		"printf '%s\\0%s\\0' \"$file\" \"${hash%% *}\"; done"
}

func parseAndroidPlaylistHashes(data, root string) map[string]string {
	result := map[string]string{}
	prefix := strings.TrimSuffix(root, "/") + "/"
	fields := strings.Split(strings.TrimSuffix(data, "\x00"), "\x00")
	for index := 0; index+1 < len(fields); index += 2 {
		if !strings.HasPrefix(fields[index], prefix) {
			continue
		}
		hash := strings.TrimSpace(fields[index+1])
		if len(hash) != sha256.Size*2 {
			continue
		}
		result[strings.TrimPrefix(fields[index], prefix)] = hash
	}
	return result
}

func parseAndroidInventory(data, root string) map[string]androidFileState {
	result := map[string]androidFileState{}
	prefix := strings.TrimSuffix(root, "/") + "/"
	fields := strings.Split(strings.TrimSuffix(data, "\x00"), "\x00")
	for index := 0; index+2 < len(fields); index += 3 {
		if !strings.HasPrefix(fields[index], prefix) {
			continue
		}
		size, sizeErr := strconv.ParseInt(fields[index+1], 10, 64)
		modTimeFloat, modErr := strconv.ParseFloat(fields[index+2], 64)
		if sizeErr != nil || modErr != nil {
			continue
		}
		result[strings.TrimPrefix(fields[index], prefix)] = androidFileState{
			Size:    size,
			ModTime: int64(modTimeFloat),
		}
	}
	return result
}

func sameAndroidFile(source string, remote androidFileState) bool {
	return androidFileDifference(source, remote, true) == ""
}

func androidFileDifference(source string, remote androidFileState, exists bool) string {
	if !exists {
		return "Android上に存在しません"
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Sprintf("Mac側ファイルを確認できません: %v", err)
	}
	if !info.Mode().IsRegular() {
		return "Mac側が通常ファイルではありません"
	}
	if info.Size() != remote.Size {
		return fmt.Sprintf("サイズ不一致 Mac=%d Android=%d", info.Size(), remote.Size)
	}
	if remote.Hash != "" {
		file, err := os.Open(source)
		if err != nil {
			return fmt.Sprintf("Mac側ファイルを開けません: %v", err)
		}
		defer file.Close()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return fmt.Sprintf("Mac側ハッシュを計算できません: %v", err)
		}
		localHash := fmt.Sprintf("%x", hash.Sum(nil))
		if !strings.EqualFold(localHash, remote.Hash) {
			return fmt.Sprintf("ハッシュ不一致 Mac=%s Android=%s", localHash, remote.Hash)
		}
		return ""
	}
	delta := info.ModTime().Unix() - remote.ModTime
	if delta < 0 {
		delta = -delta
	}
	if delta > 2 {
		return fmt.Sprintf(
			"更新時刻不一致 Mac=%d Android=%d 差=%d秒",
			info.ModTime().Unix(), remote.ModTime, delta,
		)
	}
	return ""
}

func androidFreeBytes(serial, volumePath string) (int64, error) {
	out, err := adbShell(serial, "df -k "+shellQuote(volumePath)+" | tail -n 1")
	if err != nil {
		return 0, adbError("Androidストレージの空き容量を確認できませんでした", out, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 4 {
		return 0, fmt.Errorf("Androidストレージの空き容量を解釈できませんでした: %q", strings.TrimSpace(string(out)))
	}
	available, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("Androidストレージの空き容量を解釈できませんでした: %w", err)
	}
	return available * 1024, nil
}

func loadAndroidManagedPaths(serial, root string) ([]string, error) {
	command := "for f in " + shellQuote(path.Join(root, manifest)) + " " +
		shellQuote(path.Join(root, androidPendingManifest)) +
		"; do [ -f \"$f\" ] && cat \"$f\"; done; true"
	out, err := adbShell(serial, command)
	if err != nil {
		return nil, adbError("Android上の同期マニフェストを確認できませんでした", out, err)
	}
	managed := parseAndroidManagedPaths(string(out))
	for index := range managed {
		managed[index] = androidVisiblePath(managed[index])
	}
	return uniqueStrings(managed), nil
}

func parseAndroidManagedPaths(data string) []string {
	var saved []string
	lines := strings.Split(strings.TrimSpace(data), "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "[") {
		end := 0
		for end < len(lines) {
			end++
			if strings.HasSuffix(strings.TrimSpace(lines[end-1]), "]") {
				break
			}
		}
		if err := json.Unmarshal([]byte(strings.Join(lines[:end], "\n")), &saved); err == nil {
			lines = lines[end:]
		}
	}
	seen := map[string]bool{}
	for _, relative := range append(saved, lines...) {
		relative = strings.TrimSpace(relative)
		if relative != "" {
			seen[relative] = true
		}
	}
	result := make([]string, 0, len(seen))
	for relative := range seen {
		result = append(result, relative)
	}
	sort.Strings(result)
	return result
}

func saveAndroidManifest(serial, root string, paths []string) error {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	data, err := json.MarshalIndent(sorted, "", "  ")
	if err != nil {
		return err
	}
	remoteTemp := path.Join(root, manifest+".tmp")
	if err := adbWrite(serial, remoteTemp, append(data, '\n')); err != nil {
		return err
	}
	command := "mv " + shellQuote(remoteTemp) + " " + shellQuote(path.Join(root, manifest)) +
		" && touch " + shellQuote(path.Join(root, libraryManifestMarker)) +
		" && rm -f " + shellQuote(path.Join(root, androidPendingManifest))
	out, err := adbShell(serial, command)
	if err != nil {
		return adbError("Android上の同期マニフェストを保存できませんでした", out, err)
	}
	return nil
}

func saveAndroidPendingPaths(serial, root string, managed []string, desired map[string]bool) error {
	paths := append([]string(nil), managed...)
	for relative := range desired {
		paths = append(paths, relative)
	}
	paths = uniqueStrings(paths)
	var data strings.Builder
	for _, relative := range paths {
		data.WriteString(relative)
		data.WriteByte('\n')
	}
	if err := adbWrite(serial, path.Join(root, androidPendingManifest), []byte(data.String())); err != nil {
		return fmt.Errorf("Android上の転送予定を保存できませんでした: %w", err)
	}
	return nil
}

func androidStalePaths(managed []string, desired map[string]bool) []string {
	var stale []string
	for _, relative := range managed {
		if !desired[relative] {
			stale = append(stale, relative)
		}
	}
	sort.Strings(stale)
	return stale
}

func removeAndroidEmptyDirs(serial, root string) error {
	command := "if [ -d " + shellQuote(path.Join(root, libraryDir)) + " ]; then find " +
		shellQuote(path.Join(root, libraryDir)) + " -depth -type d -empty -delete; fi"
	out, err := adbShell(serial, command)
	if err != nil {
		return adbError("Android上の空ディレクトリを削除できませんでした", out, err)
	}
	return nil
}
