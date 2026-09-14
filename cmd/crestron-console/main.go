// Command crestron-console runs commands on a Crestron processor's console
// over SSH. The processor only offers an interactive shell, not exec channels,
// so commands are fed to a PTY and the transcript is echoed back.
//
//	crestron-console ver "help"
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/erh/viam-crestron/crestron"
)

func main() {
	crestron.LoadEnvFile()

	host := flag.String("host", envOr("CRESTRON_HOST", "192.168.3.2"), "processor address")
	user := flag.String("user", os.Getenv("CRESTRON_USER"), "console username")
	pass := flag.String("pass", os.Getenv("CRESTRON_PASS"), "console password")
	settle := flag.Duration("settle", 2*time.Second, "how long to wait for output after each command")
	flag.Parse()

	if *user == "" || *pass == "" {
		fatal(fmt.Errorf("set CRESTRON_USER and CRESTRON_PASS"))
	}
	if flag.NArg() == 0 {
		fatal(fmt.Errorf("give at least one console command"))
	}

	if err := run(*host, *user, *pass, flag.Args(), *settle); err != nil {
		fatal(err)
	}
}

func run(host, user, pass string, cmds []string, settle time.Duration) error {
	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(pass),
			// Crestron prompts for the password interactively on some firmware.
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				answers := make([]string, len(qs))
				for i := range qs {
					answers[i] = pass
				}
				return answers, nil
			}),
		},
		// The processor ships a self signed host key; we are on the LAN talking
		// to a known address, so pin nothing.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", host+":22", cfg)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	sess.Stderr = os.Stderr

	if err := sess.RequestPty("vt100", 200, 80, ssh.TerminalModes{ssh.ECHO: 0}); err != nil {
		return fmt.Errorf("pty: %w", err)
	}
	if err := sess.Shell(); err != nil {
		return fmt.Errorf("shell: %w", err)
	}

	done := make(chan struct{})
	go func() {
		io.Copy(os.Stdout, stdout)
		close(done)
	}()

	time.Sleep(settle) // let the banner and first prompt arrive
	for _, c := range cmds {
		fmt.Printf("\n>>> %s\n", c)
		if _, err := io.WriteString(stdin, c+"\r\n"); err != nil {
			return err
		}
		time.Sleep(settle)
	}
	io.WriteString(stdin, "bye\r\n")
	stdin.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", strings.TrimSpace(err.Error()))
	os.Exit(1)
}
