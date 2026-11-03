package containers

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"slices"
	"time"
)

// crew ctr: the read-only commands a container chat's Claude runs (and you can too). Each one is a
// fixed docker or kubectl read: nothing here starts, stops, deletes, applies or execs, and
// inspect and describe output has secret-looking values hidden. Secrets list their keys only.

// ChatBrief is what a thing's chat is told first; exe is the crew binary Claude runs as `crew ctr`.
func ChatBrief(exe, kind, id, name, ns, kctx string) string {
	what := fmt.Sprintf("Docker %s %s (id %s)", lower(kind), name, short(id))
	if kctx != "" {
		what = fmt.Sprintf("Kubernetes %s %s in namespace %s, context %s", lower(kind), name, ns, kctx)
	}
	return fmt.Sprintf("You're helping me with %s, in crew. You can read, and only read, with %[2]q ctr:\n%s\n"+
		"Start from its logs or inspect/describe output. You can't change anything (no restart, delete, exec, apply or scale): "+
		"when a fix needs a change, say the exact command and why, and I'll run it. Secret values are hidden on purpose; "+
		"don't ask for them.", what, exe, usage)
}

// ChatArgs lock a thing's chat down to crew ctr: no files, MCP servers or user settings, and no
// other command. Anything else is refused, not asked.
func ChatArgs(exe string) []string {
	return []string{"--restricted", "--strict-mcp-config", "--permission-mode", "dontAsk", "--permission-prompts", "none",
		"--tools", "Bash", "--allowedTools", "Bash(" + exe + " ctr:*)"}
}

const usage = `crew ctr docker ps | images | volumes | networks | df | stats
crew ctr docker logs <container> [--tail N]
crew ctr docker inspect <container|image|volume|network>
crew ctr kube [--context C] contexts
crew ctr kube [--context C] get <kind> [-n namespace]     (kind: pods deployments services jobs cronjobs replicasets statefulsets daemonsets ingresses configmaps secrets pvc nodes namespaces events)
crew ctr kube [--context C] describe <kind> <name> [-n namespace]
crew ctr kube [--context C] logs <pod|deployment/x|job/x> [-n namespace] [--tail N] [--previous]
crew ctr kube [--context C] top pods|nodes [-n namespace]`

// kubeGettable are the kinds crew ctr kube get and describe read.
var kubeGettable = []string{"pods", "pod", "deployments", "deployment", "services", "service", "svc", "jobs", "job", "cronjobs", "cronjob",
	"replicasets", "replicaset", "rs", "statefulsets", "statefulset", "daemonsets", "daemonset", "ingresses", "ingress",
	"configmaps", "configmap", "cm", "secrets", "secret", "pvc", "persistentvolumeclaims", "nodes", "node", "namespaces", "ns", "events"}

// word is a name, namespace, context or count crew ctr passes on: no flags, no shell.
var word = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]*$`)

// CLI runs crew ctr with args, writing to out; it returns the exit code.
func CLI(args []string, out io.Writer) int {
	fail := func(msg string) int { fmt.Fprintln(out, msg+"\n\nusage:\n"+usage); return 2 }
	if len(args) == 0 {
		return fail("say docker or kube")
	}
	// Flags may come anywhere: read them out, keep the rest in order.
	var rest []string
	kctx, ns, tailN, previous := "", "", "200", false
	for i := 1; i < len(args); i++ {
		switch a := args[i]; a {
		case "--context", "-n", "--namespace", "--tail":
			if i+1 >= len(args) || !word.MatchString(args[i+1]) {
				return fail(a + " needs a value")
			}
			i++
			switch a {
			case "--context":
				kctx = args[i]
			case "--tail":
				tailN = args[i]
			default:
				ns = args[i]
			}
		case "--previous", "-p":
			previous = true
		default:
			if !word.MatchString(a) {
				return fail("not a name crew ctr reads: " + a)
			}
			rest = append(rest, a)
		}
	}
	if len(rest) == 0 {
		return fail("say what to read")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var s string
	var err error
	if args[0] == "docker" {
		s, err = dockerRead(ctx, rest, tailN)
	} else if args[0] == "kube" {
		s, err = kubeRead(ctx, kctx, ns, rest, tailN, previous)
	} else {
		return fail("say docker or kube")
	}
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	fmt.Fprintln(out, s)
	return 0
}

// dockerRead is crew ctr docker.
func dockerRead(ctx context.Context, a []string, tailN string) (string, error) {
	switch {
	case a[0] == "ps" && len(a) == 1:
		return run(ctx, "docker", "ps", "-a")
	case a[0] == "images" && len(a) == 1:
		return run(ctx, "docker", "images")
	case a[0] == "volumes" && len(a) == 1:
		return run(ctx, "docker", "volume", "ls")
	case a[0] == "networks" && len(a) == 1:
		return run(ctx, "docker", "network", "ls")
	case a[0] == "df" && len(a) == 1:
		return run(ctx, "docker", "system", "df", "-v")
	case a[0] == "stats" && len(a) == 1:
		return run(ctx, "docker", "stats", "--no-stream")
	case a[0] == "logs" && len(a) == 2:
		return run(ctx, "docker", "logs", "--tail", tailN, "--timestamps", a[1])
	case a[0] == "inspect" && len(a) == 2:
		out, err := run(ctx, "docker", "inspect", a[1])
		return redact(out), err
	}
	return "", fmt.Errorf("crew ctr docker doesn't do that\n\nusage:\n%s", usage)
}

// kubeRead is crew ctr kube. Secrets are listed with kubectl's table (names, types, how many keys)
// and described (key sizes), never as YAML or JSON.
func kubeRead(ctx context.Context, kctx, ns string, a []string, tailN string, previous bool) (string, error) {
	nsArgs := []string{"-A"}
	if ns != "" {
		nsArgs = []string{"-n", ns}
	}
	switch {
	case a[0] == "contexts" && len(a) == 1:
		return kubectl(ctx, "", "config", "get-contexts")
	case a[0] == "get" && len(a) == 2 && slices.Contains(kubeGettable, a[1]):
		return kubectl(ctx, kctx, append([]string{"get", a[1], "-o", "wide"}, nsArgs...)...)
	case a[0] == "describe" && len(a) == 3 && slices.Contains(kubeGettable, a[1]):
		args := []string{"describe", a[1], a[2]}
		if ns != "" {
			args = append(args, "-n", ns)
		}
		out, err := kubectl(ctx, kctx, args...)
		return redact(out), err
	case a[0] == "logs" && len(a) == 2:
		args := []string{"logs", a[1], "--all-containers", "--prefix", "--tail", tailN}
		if ns != "" {
			args = append(args, "-n", ns)
		}
		if previous {
			args = append(args, "--previous")
		}
		return kubectl(ctx, kctx, args...)
	case a[0] == "top" && len(a) == 2 && (a[1] == "pods" || a[1] == "nodes"):
		args := []string{"top", a[1]}
		if a[1] == "pods" {
			args = append(args, nsArgs...)
		}
		return kubectl(ctx, kctx, args...)
	}
	return "", fmt.Errorf("crew ctr kube doesn't do that\n\nusage:\n%s", usage)
}

// lower is kind in lower case, for sentences.
func lower(kind string) string {
	b := []byte(kind)
	if len(b) > 0 && b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}
