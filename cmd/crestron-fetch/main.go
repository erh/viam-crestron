// Command crestron-fetch copies files off a Crestron processor over SFTP,
// which is far quicker than paging them through the console with TYPE.
//
//	crestron-fetch -out ./cfg /user/Data/Configuration/LightingSystem.cfg
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/erh/viam-crestron/crestron"
)

func main() {
	crestron.LoadEnvFile()

	host := flag.String("host", envOr("CRESTRON_HOST", "192.168.3.2"), "processor address")
	user := flag.String("user", os.Getenv("CRESTRON_USER"), "username")
	pass := flag.String("pass", os.Getenv("CRESTRON_PASS"), "password")
	out := flag.String("out", ".", "directory to write files into")
	list := flag.String("ls", "", "list a remote directory instead of fetching")
	put := flag.String("put", "", "upload a local file to this remote path instead of fetching")
	rm := flag.String("rm", "", "delete this remote path")
	flag.Parse()

	if *user == "" || *pass == "" {
		fatal(fmt.Errorf("set CRESTRON_USER and CRESTRON_PASS"))
	}

	client, err := dial(*host, *user, *pass)
	if err != nil {
		fatal(err)
	}
	defer client.Close()

	if *list != "" {
		entries, err := client.ReadDir(*list)
		if err != nil {
			fatal(err)
		}
		for _, e := range entries {
			kind := "     "
			if e.IsDir() {
				kind = "[DIR]"
			}
			fmt.Printf("%s %10d  %s\n", kind, e.Size(), e.Name())
		}
		return
	}

	if *rm != "" {
		if err := client.Remove(*rm); err != nil {
			fatal(err)
		}
		fmt.Printf("removed %s\n", *rm)
		return
	}
	if *put != "" {
		if flag.NArg() != 1 {
			fatal(fmt.Errorf("-put needs exactly one local file argument"))
		}
		n, err := upload(client, flag.Arg(0), *put)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s -> %s (%d bytes)\n", flag.Arg(0), *put, n)
		return
	}

	if flag.NArg() == 0 {
		fatal(fmt.Errorf("give at least one remote path, or -ls <dir>"))
	}
	for _, remote := range flag.Args() {
		local := filepath.Join(*out, path.Base(remote))
		n, err := fetch(client, remote, local)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("%s -> %s (%d bytes)\n", remote, local, n)
	}
}

func dial(host, user, pass string) (*sftp.Client, error) {
	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(pass),
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				answers := make([]string, len(qs))
				for i := range qs {
					answers[i] = pass
				}
				return answers, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	conn, err := ssh.Dial("tcp", host+":22", cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}
	c, err := sftp.NewClient(conn)
	if err != nil {
		return nil, fmt.Errorf("sftp: %w", err)
	}
	return c, nil
}

func upload(c *sftp.Client, local, remote string) (int64, error) {
	src, err := os.Open(local)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	dst, err := c.Create(remote)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", remote, err)
	}
	defer dst.Close()
	return io.Copy(dst, src)
}

func fetch(c *sftp.Client, remote, local string) (int64, error) {
	src, err := c.Open(remote)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", remote, err)
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return 0, err
	}
	dst, err := os.Create(local)
	if err != nil {
		return 0, err
	}
	defer dst.Close()
	return io.Copy(dst, src)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
