package bridge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

func transfer(plan []Planned, root string, dry bool, labels map[string]string) error {
	started := time.Now()
	pending := make([]Planned, 0, len(plan))
	for _, item := range plan {
		if !sameFile(item.Track.Location, filepath.Join(root, item.Relative)) {
			pending = append(pending, item)
		}
	}
	total := totalBytes(pending)
	var done int64
	const maxBatchBytes int64 = 1 << 30
	batches := make([][]Planned, 0)
	for _, item := range pending {
		if len(batches) == 0 || (batchBytes(batches[len(batches)-1])+item.Size > maxBatchBytes && len(batches[len(batches)-1]) > 0) {
			batches = append(batches, nil)
		}
		batches[len(batches)-1] = append(batches[len(batches)-1], item)
	}
	processed := 0
	// ETA と速度は、完了した rsync バッチの実測時間だけから算出する。
	// スピナーの再描画時刻を分母に含めると、バッチ転送中に見かけの速度が
	// 毎秒下がり、ETA が毎秒増えてしまう。
	var transferElapsed time.Duration
	var displayMu sync.Mutex
	printProgress := func(item Planned, spinner string) {
		displayMu.Lock()
		defer displayMu.Unlock()
		rate, eta := transferEstimate(total, done, transferElapsed)
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
			args := []string{"-ahL", "--partial", "--append-verify"}
			if dry {
				args = append(args, "--dry-run")
			}
			// 転送は常に1本で実行し、microSD上の帯域競合を避ける。
			if len(items) > 0 {
				printProgress(items[0], "|")
			}
			args = append(args, stage+string(os.PathSeparator), root+string(os.PathSeparator))
			cmd := exec.Command("rsync", args...)
			batchStarted := time.Now()
			stopSpinner := make(chan struct{})
			var spinnerWG sync.WaitGroup
			if len(items) > 0 && !dry {
				spinnerWG.Add(1)
				go func(item Planned) {
					defer spinnerWG.Done()
					frames := []string{"|", "/", "-", "\\"}
					frame := 0
					ticker := time.NewTicker(time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							frame = (frame + 1) % len(frames)
							printProgress(item, frames[frame])
						case <-stopSpinner:
							return
						}
					}
				}(items[0])
			}
			runErr := cmd.Run()
			close(stopSpinner)
			spinnerWG.Wait()
			if runErr != nil {
				err = runErr
				return
			}
			transferElapsed += time.Since(batchStarted)
			for _, item := range items {
				processed++
				done += item.Size
				printProgress(item, "")
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
