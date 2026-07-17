package bridge

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func rsyncItemForOutput(items map[string]Planned, line string) (Planned, bool) {
	relative := filepath.Clean(filepath.FromSlash(strings.TrimSpace(line)))
	item, ok := items[relative]
	return item, ok
}

type transferProgress struct {
	mu        sync.Mutex
	item      Planned
	done      int64
	processed int
	rate      int64
	eta       time.Duration
}

func (p *transferProgress) startBatch(item Planned) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.item = item
}

func (p *transferProgress) complete(item Planned, total int64, elapsed time.Duration, size int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.item = item
	p.processed++
	p.done += size
	p.rate, p.eta = transferEstimate(total, p.done, elapsed)
}

func (p *transferProgress) snapshot() (Planned, int64, int, int64, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.item, p.done, p.processed, p.rate, p.eta
}

func transfer(plan []Planned, root string, dry bool, labels map[string]string) error {
	started := time.Now()
	pending := make([]Planned, 0, len(plan))
	for _, item := range plan {
		if !sameFile(item.Track.Location, filepath.Join(root, item.Relative)) {
			pending = append(pending, item)
		}
	}
	total := totalBytes(pending)
	const maxBatchBytes int64 = 1 << 30
	batches := make([][]Planned, 0)
	for _, item := range pending {
		if len(batches) == 0 || (batchBytes(batches[len(batches)-1])+item.Size > maxBatchBytes && len(batches[len(batches)-1]) > 0) {
			batches = append(batches, nil)
		}
		batches[len(batches)-1] = append(batches[len(batches)-1], item)
	}
	// ETA と速度は曲の完了時だけ更新する。スピナーの再描画で再計算すると、
	// 実転送量が増えないまま速度だけ下がり、ETAが毎秒増えてしまう。
	var transferElapsed time.Duration
	progress := &transferProgress{}
	printProgress := func(item Planned, spinner string) {
		_, done, processed, rate, eta := progress.snapshot()
		etaText := "-"
		speed := "-"
		if rate > 0 {
			etaText = eta.Round(time.Second).String()
			speed = humanBytes(rate) + "/s"
		}
		percent := 100.0
		if total > 0 {
			percent = float64(done) * 100 / float64(total)
		}
		label := labels[item.Track.Location]
		if label != "" {
			label = " | プレイリスト: " + label
		}
		activity := ""
		if spinner != "" {
			activity = " | コピー中 " + spinner
		}
		fmt.Printf("\033[2K\rETA %s | 速度 %s | 転送中 [%d/%d曲] %5.1f%% (%s/%s)%s | %s%s",
			etaText, speed, processed, len(pending), percent,
			humanBytes(done), humanBytes(total), label, truncateRunes(item.Track.Name, 20), activity)
	}
	for batchIndex, items := range batches {
		stage, err := stagePlan(items)
		if err != nil {
			return err
		}
		func() {
			defer os.RemoveAll(stage)
			args := []string{"-ahL", "--partial", "--append-verify", "--out-format=%n"}
			if dry {
				args = append(args, "--dry-run")
			}
			// 転送は常に1本で実行し、microSD上の帯域競合を避ける。
			if len(items) > 0 {
				progress.startBatch(items[0])
				printProgress(items[0], "|")
			}
			args = append(args, stage+string(os.PathSeparator), root+string(os.PathSeparator))
			cmd := exec.Command("rsync", args...)
			cmd.Stderr = diagnosticWriter()
			logf("rsync batch %d/%d: rsync %s", batchIndex+1, len(batches), strings.Join(args, " "))
			stdout, pipeErr := cmd.StdoutPipe()
			if pipeErr != nil {
				err = pipeErr
				return
			}
			batchStarted := time.Now()
			stopSpinner := make(chan struct{})
			var spinnerWG sync.WaitGroup
			if len(items) > 0 && !dry {
				spinnerWG.Add(1)
				go func() {
					defer spinnerWG.Done()
					frames := []string{"|", "/", "-", "\\"}
					frame := 0
					ticker := time.NewTicker(time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							frame = (frame + 1) % len(frames)
							item, _, _, _, _ := progress.snapshot()
							printProgress(item, frames[frame])
						case <-stopSpinner:
							return
						}
					}
				}()
			}
			batchItems := make(map[string]Planned, len(items))
			for _, item := range items {
				batchItems[filepath.Clean(item.Relative)] = item
			}
			complete := func(item Planned) {
				if _, pending := batchItems[filepath.Clean(item.Relative)]; !pending {
					return
				}
				delete(batchItems, filepath.Clean(item.Relative))
				progress.complete(item, total, transferElapsed+time.Since(batchStarted), item.Size)
				printProgress(item, "")
			}
			if startErr := cmd.Start(); startErr != nil {
				err = startErr
				return
			}
			scanDone := make(chan error, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				scanner.Buffer(make([]byte, 64*1024), 1024*1024)
				for scanner.Scan() {
					if item, ok := rsyncItemForOutput(batchItems, scanner.Text()); ok {
						complete(item)
					}
				}
				scanDone <- scanner.Err()
			}()
			runErr := cmd.Wait()
			scanErr := <-scanDone
			close(stopSpinner)
			spinnerWG.Wait()
			if runErr != nil {
				err = runErr
				return
			}
			if scanErr != nil {
				err = scanErr
				return
			}
			transferElapsed += time.Since(batchStarted)
			for _, item := range items {
				complete(item)
			}
		}()
		if err != nil {
			fmt.Print("\033[2K\r")
			return fmt.Errorf("転送バッチ %d/%d: %w", batchIndex+1, len(batches), err)
		}
	}
	fmt.Print("\033[2K\r")
	fmt.Println()
	fmt.Printf("音源転送時間: %s\n", time.Since(started).Round(time.Second))
	return nil
}

func transferEstimate(total, done int64, elapsed time.Duration) (int64, time.Duration) {
	if done <= 0 || elapsed <= 0 || total <= done {
		return 0, 0
	}
	rate := int64(float64(done) / elapsed.Seconds())
	if rate <= 0 {
		return 0, 0
	}
	return rate, time.Duration(float64(total-done)/float64(rate)) * time.Second
}

func batchBytes(items []Planned) int64 {
	var total int64
	for _, item := range items {
		total += item.Size
	}
	return total
}
