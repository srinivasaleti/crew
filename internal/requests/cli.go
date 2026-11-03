package requests

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

// CLI is `crew req …`, what Claude runs from the REQUESTS chat (and you can too). It never prints a
// key: requests are shown as written, and responses with keys and sensitive fields hidden.
//
//	crew req apps                        applications and their environments (key names only)
//	crew req list                        the requests
//	crew req run <app>/<file>[#n] [--env e]   send a request (#n or #name picks one in a file)
func CLI(dir string, args []string, out io.Writer) int {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	if err := MoveLoose(dir); err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	if len(args) == 0 {
		fmt.Fprintln(out, "usage: crew req apps | list | run <app>/<file>[#n] [--env <name>]")
		return 2
	}
	switch args[0] {
	case "apps":
		for _, app := range Apps(dir) {
			cur := Current(dir, app)
			fmt.Fprintf(out, "%s (requests go with env %s)\n", app, cur)
			for _, name := range Envs(dir, app) {
				e, _ := LoadEnv(dir, app, name)
				fmt.Fprintf(out, "  %s: baseUrl %s · %s\n", name, e.BaseURL(), e.AuthLabel())
				for k, v := range e.Vars {
					if k != "baseUrl" {
						fmt.Fprintf(out, "    %s = %s\n", k, v)
					}
				}
				for _, k := range e.Keys {
					set := "not set"
					if HasKey(e, k) {
						set = "set"
					}
					fmt.Fprintf(out, "    key %s (%s; the value is never shown)\n", k, set)
				}
				fmt.Fprintf(out, "    keys go only to: %s\n", strings.Join(e.AllHosts(), ", "))
				if e.Insecure {
					fmt.Fprintf(out, "    insecure: its servers' certificates aren't checked\n")
				}
			}
		}
	case "list":
		reqs, err := Load(dir)
		if err != nil {
			fmt.Fprintln(out, err)
			return 1
		}
		for _, r := range reqs {
			fmt.Fprintf(out, "%s\t%s %s\t%s\n", r.ID(), r.Method, r.URL, r.Name)
		}
	case "run":
		return run(dir, args[1:], out)
	default:
		fmt.Fprintln(out, "unknown command "+args[0])
		return 2
	}
	return 0
}

func run(dir string, args []string, out io.Writer) int {
	env, target := "", ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--env" && i+1 < len(args) {
			env = args[i+1]
			i++
		} else {
			target = args[i]
		}
	}
	reqs, err := Load(dir)
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	file, n, _ := strings.Cut(target, "#")
	i := slices.IndexFunc(reqs, func(r Request) bool {
		return r.File == file && (n == "" && r.Index == 0 || fmt.Sprint(r.Index) == n || r.Name == n)
	})
	if i < 0 {
		fmt.Fprintf(out, "no request %s (crew req list shows them)\n", target)
		return 1
	}
	r := reqs[i]
	if env == "" {
		env = Current(dir, r.App)
	}
	e, err := LoadEnv(dir, r.App, env)
	if err != nil {
		fmt.Fprintln(out, err.Error()+" (crew req apps shows them)")
		return 1
	}
	keys, err := KeysFor(r, e)
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	fmt.Fprintf(out, "%s · %s %s\n%s\n", r.Name, r.App, env, r.WithAuth(e).Text())
	res, err := Send(context.Background(), r, e, keys)
	if err != nil {
		fmt.Fprintln(out, Redact(err.Error(), keys, true))
		return 1
	}
	fmt.Fprintln(out, Format(res, keys, true))
	return 0
}
