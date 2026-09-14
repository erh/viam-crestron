// Command crestron-cli explores and drives a Crestron Home processor, so the
// wiring can be checked by hand before it is wrapped in a Viam module.
//
//	export CRESTRON_TOKEN=...
//	crestron-cli lights
//	crestron-cli set 12 50
//	crestron-cli raw /devices
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/erh/viam-crestron/crestron"
)

func main() {
	// Pull CRESTRON_* defaults from ~/.crestron.env (override with CRESTRON_ENV)
	// before the flags read them. Real environment variables still win.
	crestron.LoadEnvFile()

	host := flag.String("host", envOr("CRESTRON_HOST", "192.168.3.2"), "processor address")
	token := flag.String("token", os.Getenv("CRESTRON_TOKEN"), "web API token")
	fade := flag.Int("fade", 0, "fade time in tenths of a second")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	// discover works from local config files and needs no token; every other
	// command talks to the live REST API.
	if args[0] != "discover" && *token == "" {
		fatal(fmt.Errorf("no token: set CRESTRON_TOKEN or pass -token"))
	}

	ctx := context.Background()
	c := crestron.New(*host, *token)

	if err := run(ctx, c, args, *fade); err != nil {
		fatal(err)
	}
}

func run(ctx context.Context, c *crestron.Client, args []string, fade int) error {
	switch args[0] {
	case "lights":
		return listLights(ctx, c)
	case "rooms":
		return listRooms(ctx, c)
	case "discover":
		if len(args) < 3 {
			return fmt.Errorf("discover needs LightingSystem.cfg and SystemConfig.cfg paths")
		}
		return discover(args[1], args[2])
	case "raw":
		if len(args) < 2 {
			return fmt.Errorf("raw needs a path, e.g. raw /devices")
		}
		return dumpRaw(ctx, c, args[1])
	case "on", "off":
		id, err := lightID(args, 1)
		if err != nil {
			return err
		}
		level := 0
		if args[0] == "on" {
			level = crestron.MaxLevel
		}
		return c.SetLevel(ctx, id, level, fade)
	case "set":
		if len(args) < 3 {
			return fmt.Errorf("set needs an id and a percent, e.g. set 12 50")
		}
		id, err := lightID(args, 1)
		if err != nil {
			return err
		}
		pct, err := strconv.Atoi(args[2])
		if err != nil {
			return fmt.Errorf("bad percent %q: %w", args[2], err)
		}
		return c.SetPercent(ctx, id, pct, fade)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func lightID(args []string, i int) (int, error) {
	if len(args) <= i {
		return 0, fmt.Errorf("%s needs a light id", args[0])
	}
	id, err := strconv.Atoi(args[i])
	if err != nil {
		return 0, fmt.Errorf("bad light id %q: %w", args[i], err)
	}
	return id, nil
}

func listLights(ctx context.Context, c *crestron.Client) error {
	lights, err := c.Lights(ctx)
	if err != nil {
		return err
	}
	rooms, err := c.Rooms(ctx)
	if err != nil {
		return err
	}
	byRoom := map[int]string{}
	for _, r := range rooms {
		byRoom[r.ID] = r.Name
	}

	sort.Slice(lights, func(i, j int) bool {
		if byRoom[lights[i].RoomID] != byRoom[lights[j].RoomID] {
			return byRoom[lights[i].RoomID] < byRoom[lights[j].RoomID]
		}
		return lights[i].Name < lights[j].Name
	})

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tROOM\tNAME\tTYPE\tLEVEL\tSTATUS")
	for _, l := range lights {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d%%\t%s\n",
			l.ID, byRoom[l.RoomID], l.Name, l.SubType, l.Percent(), l.ConnectionStatus)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\n%d lights in %d rooms\n", len(lights), len(rooms))
	return nil
}

func discover(lightingPath, systemPath string) error {
	loads, err := crestron.ParseLightingConfig(lightingPath, systemPath)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tROOM\tNAME\tKIND\tDIMMABLE")
	for _, l := range loads {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%v\n", l.ID, l.RoomName, l.Name, l.Kind(), l.Dimmable)
	}
	w.Flush()
	fmt.Printf("\n%d loads\n", len(loads))
	return nil
}

func listRooms(ctx context.Context, c *crestron.Client) error {
	rooms, err := c.Rooms(ctx)
	if err != nil {
		return err
	}
	for _, r := range rooms {
		fmt.Printf("%4d  %s\n", r.ID, r.Name)
	}
	return nil
}

func dumpRaw(ctx context.Context, c *crestron.Client, path string) error {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	raw, err := c.Raw(ctx, path)
	if err != nil {
		return err
	}
	var pretty any
	if err := json.Unmarshal(raw, &pretty); err != nil {
		fmt.Println(string(raw))
		return nil
	}
	out, err := json.MarshalIndent(pretty, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func usage() {
	fmt.Fprint(os.Stderr, `crestron-cli [flags] <command>

commands:
  lights            list every light load with its room and level
  rooms             list rooms (live, needs token)
  discover <lighting.cfg> <system.cfg>
                    list lights from config files pulled over SFTP (no token)
  raw <path>        GET an /cws/api path and pretty print the JSON
  on <id>           drive a load fully on
  off <id>          drive a load off
  set <id> <pct>    drive a load to a percentage

flags:
`)
	flag.PrintDefaults()
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
