package notify

import (
	"fmt"
	"io"
	"os/exec"
)

const completionSound = "/System/Library/Sounds/Glass.aiff"

type Runner func(name string, args ...string) error

type Notifier struct {
	output io.Writer
	run    Runner
}

func New(output io.Writer) *Notifier {
	return &Notifier{
		output: output,
		run: func(name string, args ...string) error {
			return exec.Command(name, args...).Run()
		},
	}
}

func NewWithRunner(output io.Writer, runner Runner) *Notifier {
	return &Notifier{output: output, run: runner}
}

func (notifier *Notifier) Completion() {
	// Terminal.appはBELでDockバッジ・Dockアイコンのバウンスを表示できる。
	fmt.Fprint(notifier.output, "\a")
	_ = notifier.run("afplay", completionSound)
}

func (notifier *Notifier) AndroidDisconnectReminder() {
	// 2回目以降はmacOSの通知として鳴らす。通知音は集中モードと通知設定に従う。
	const script = `display notification "Androidとの接続が切れたままです。自動再接続を続けています。" with title "Music Bridge" sound name "Glass"`
	_ = notifier.run("osascript", "-e", script)
}
