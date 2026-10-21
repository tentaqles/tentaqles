// Package notify shows one desktop notification, bounded to two seconds and never
// retried (spec §8.7). The hub's waiting list is the source of truth; this is a nudge.
package notify

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode"
)

const Timeout = 2 * time.Second

type Command struct {
	Name string
	Args []string
	Env  []string
}

const psToast = `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$t = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$x = $t.GetElementsByTagName('text')
$x.Item(0).AppendChild($t.CreateTextNode($env:TQ_NOTIFY_TITLE)) > $null
$x.Item(1).AppendChild($t.CreateTextNode($env:TQ_NOTIFY_BODY)) > $null
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('tq hub').Show([Windows.UI.Notifications.ToastNotification]::new($t))`

const appleScript = `display notification (system attribute "TQ_NOTIFY_BODY") with title (system attribute "TQ_NOTIFY_TITLE")`

func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len([]rune(s)) > 200 {
		s = string([]rune(s)[:200])
	}
	return strings.TrimSpace(s)
}

// Build returns the platform's notifier. Text travels in the environment, never inside a script.
func Build(goos, title, body string) (Command, bool) {
	env := []string{"TQ_NOTIFY_TITLE=" + clean(title), "TQ_NOTIFY_BODY=" + clean(body)}
	switch goos {
	case "windows":
		return Command{Name: "powershell", Args: []string{"-NoProfile", "-NonInteractive", "-Command", psToast}, Env: env}, true
	case "darwin":
		return Command{Name: "osascript", Args: []string{"-e", appleScript}, Env: env}, true
	case "linux", "freebsd", "openbsd", "netbsd":
		return Command{Name: "notify-send", Args: []string{"--app-name=tq-hub", "--", clean(title), clean(body)}, Env: env}, true
	}
	return Command{}, false
}

func Send(ctx context.Context, title, body string) error {
	c, ok := Build(runtime.GOOS, title, body)
	if !ok {
		return nil
	}
	return SendWith(ctx, c)
}

func SendWith(ctx context.Context, c Command) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Env = append(os.Environ(), c.Env...)
	cmd.WaitDelay = 500 * time.Millisecond
	if err := cmd.Run(); err != nil {
		return err
	}
	return ctx.Err()
}
