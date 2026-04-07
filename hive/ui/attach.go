package ui

import (
	"context"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// AttachSession attaches to a tmux session full-screen.
// The TUI steps aside; raw stdin/stdout are piped to the tmux PTY.
// Returns when the user presses Ctrl+Space followed by q/f, or when
// the session ends.
func AttachSession(sessionName string) error {
	cmd := exec.Command("tmux", "attach-session", "-t", sessionName)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	defer ptmx.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Goroutine 1: Copy tmux stdout to os.Stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(os.Stdout, ptmx)
	}()

	// Goroutine 2: Copy stdin to tmux, intercepting Ctrl+Space chord
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		chordPending := false
		deadline := time.Now().Add(50 * time.Millisecond)

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			nr, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}

			// Skip early control sequences from terminal
			if time.Now().Before(deadline) {
				continue
			}

			for i := 0; i < nr; i++ {
				b := buf[i]
				if chordPending {
					chordPending = false
					if b == 'q' || b == 'f' {
						cancel()
						return
					}
					// Not a chord key, forward both the NUL and this byte
					ptmx.Write([]byte{0x00})
					ptmx.Write([]byte{b})
					continue
				}
				// Ctrl+Space sends NUL (0x00)
				if b == 0x00 {
					chordPending = true
					continue
				}
				ptmx.Write(buf[i : i+1])
			}
		}
	}()

	// Goroutine 3: Monitor window size
	wg.Add(1)
	go func() {
		defer wg.Done()
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGWINCH)
		defer signal.Stop(sigCh)

		if ws, err := pty.GetsizeFull(os.Stdin); err == nil {
			pty.Setsize(ptmx, ws)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-sigCh:
				if ws, err := pty.GetsizeFull(os.Stdin); err == nil {
					pty.Setsize(ptmx, ws)
				}
			}
		}
	}()

	cmd.Wait()
	cancel()
	wg.Wait()
	return nil
}
