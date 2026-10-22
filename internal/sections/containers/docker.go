package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Docker, read with the docker CLI: containers (one docker inspect for all of them), images,
// volumes, networks and the disk use summary. Nothing here draws.

// run runs a CLI and returns its trimmed output; a failure carries what it printed.
//
// It runs in its own session, with no terminal. crew's terminal is the app's window, and a command
// started from crew would otherwise share it: a kubectl sign-in plugin (AWS SSO, kubelogin, gcloud)
// or an ssh docker host that asks on /dev/tty would read the keys you type into crew, and would hold
// the window open after crew quits. Without a terminal such a prompt fails, and the read says why.
// Stopping it (its timeout, crew quitting) kills the whole group, plugins included.
func run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second // don't wait on a child that kept the output open
	out, err := cmd.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		if s == "" {
			s = err.Error()
		}
		return s, fmt.Errorf("%s", s)
	}
	return s, nil
}

// container is what docker inspect says about one container, the parts crew shows.
type container struct {
	ID      string `json:"Id"`
	Name    string
	ImageID string `json:"Image"`
	Config  struct {
		Image  string
		Labels map[string]string
	}
	State struct {
		Status     string
		Running    bool
		ExitCode   int
		StartedAt  time.Time
		FinishedAt time.Time
	}
	NetworkSettings struct {
		Ports    map[string][]struct{ HostPort string }
		Networks map[string]json.RawMessage
	}
	Mounts []struct{ Type, Name string }
	cpu    string // from docker stats, while it runs
	mem    string
}

// name is the container's name without docker's leading slash.
func (c container) name() string { return strings.TrimPrefix(c.Name, "/") }

// project is the Compose project the container belongs to, or "".
func (c container) project() string { return c.Config.Labels["com.docker.compose.project"] }

// ports are its published ports, "8080→8080".
func (c container) ports() string {
	var ps []string
	for p, bs := range c.NetworkSettings.Ports {
		for _, b := range bs {
			if b.HostPort != "" {
				ps = append(ps, b.HostPort+"→"+strings.TrimSuffix(p, "/tcp"))
			}
		}
	}
	slices.Sort(ps)
	return strings.Join(slices.Compact(ps), " ")
}

// image is a local image, as docker images lists it.
type image struct {
	ID, Repository, Tag, Size, CreatedSince string
}

// dangling reports whether the image has lost its name (a rebuild took it).
func (i image) dangling() bool { return i.Repository == "<none>" }

// label is how an image reads: repository:tag, or its short id when it has no name.
func (i image) label() string {
	if i.dangling() {
		return short(i.ID)
	}
	return i.Repository + ":" + i.Tag
}

// volume is a local volume, with its size when docker system df could tell.
type volume struct {
	Name, Driver, Labels string
	Size                 string `json:"-"`
}

// project is the Compose project the volume was made for, or "".
func (v volume) project() string {
	for _, l := range strings.Split(v.Labels, ",") {
		if p, ok := strings.CutPrefix(l, "com.docker.compose.project="); ok {
			return p
		}
	}
	return ""
}

// network is a Docker network.
type network struct{ ID, Name, Driver, Scope string }

// builtin reports whether docker made the network itself; it can't be removed.
func (n network) builtin() bool { return n.Name == "bridge" || n.Name == "host" || n.Name == "none" }

// diskUse is one line of docker system df: Images, Containers, Local Volumes or Build Cache.
type diskUse struct{ Type, TotalCount, Active, Size, Reclaimable string }

// dockerData is everything the DOCKER section shows, read at once.
type dockerData struct {
	containers []container
	images     []image
	volumes    []volume
	networks   []network
	disk       map[string]diskUse // by Type
	at         time.Time
}

// usedBy counts the containers that use an image, a volume or a network.
func (d dockerData) usedBy(match func(c container) bool) []string {
	var names []string
	for _, c := range d.containers {
		if match(c) {
			names = append(names, c.name())
		}
	}
	return names
}

func (d dockerData) imageUsers(i image) []string {
	return d.usedBy(func(c container) bool { return c.ImageID == i.ID })
}

func (d dockerData) volumeUsers(v volume) []string {
	return d.usedBy(func(c container) bool {
		return slices.ContainsFunc(c.Mounts, func(m struct{ Type, Name string }) bool { return m.Type == "volume" && m.Name == v.Name })
	})
}

func (d dockerData) networkUsers(n network) []string {
	return d.usedBy(func(c container) bool { _, ok := c.NetworkSettings.Networks[n.Name]; return ok })
}

// jsonLines decodes docker's --format '{{json .}}' output, one object per line.
func jsonLines[T any](out string) []T {
	var all []T
	for _, l := range strings.Split(out, "\n") {
		var v T
		if l != "" && json.Unmarshal([]byte(l), &v) == nil {
			all = append(all, v)
		}
	}
	return all
}

// loadDocker reads containers, images, volumes, networks and disk use. stats adds each running
// container's CPU and memory, which takes docker a second or two, so only the Containers tab asks.
func loadDocker(ctx context.Context, stats bool) (dockerData, error) {
	d := dockerData{disk: map[string]diskUse{}, at: time.Now()}
	ids, err := run(ctx, "docker", "ps", "-aq", "--no-trunc")
	if err != nil {
		return d, err
	}
	if ids != "" {
		out, err := run(ctx, "docker", append([]string{"inspect"}, strings.Fields(ids)...)...)
		if err != nil {
			return d, err
		}
		json.Unmarshal([]byte(out), &d.containers)
	}
	if out, err := run(ctx, "docker", "images", "--no-trunc", "--format", "{{json .}}"); err == nil {
		d.images = jsonLines[image](out)
	}
	if out, err := run(ctx, "docker", "volume", "ls", "--format", "{{json .}}"); err == nil {
		d.volumes = jsonLines[volume](out)
	}
	if out, err := run(ctx, "docker", "network", "ls", "--no-trunc", "--format", "{{json .}}"); err == nil {
		d.networks = jsonLines[network](out)
	}
	if out, err := run(ctx, "docker", "system", "df", "--format", "{{json .}}"); err == nil {
		for _, u := range jsonLines[diskUse](out) {
			d.disk[u.Type] = u
		}
	}
	// ponytail: volume sizes come from docker system df -v, which reads every volume; drop it if
	// that gets slow with many large volumes.
	if out, err := run(ctx, "docker", "system", "df", "-v", "--format", "{{json .Volumes}}"); err == nil {
		var sized []struct{ Name, Size string }
		json.Unmarshal([]byte(out), &sized)
		for _, s := range sized {
			if i := slices.IndexFunc(d.volumes, func(v volume) bool { return v.Name == s.Name }); i >= 0 {
				d.volumes[i].Size = s.Size
			}
		}
	}
	if stats && slices.ContainsFunc(d.containers, func(c container) bool { return c.State.Running }) {
		if out, err := run(ctx, "docker", "stats", "--no-stream", "--no-trunc", "--format", "{{json .}}"); err == nil {
			for _, s := range jsonLines[struct{ ID, CPUPerc, MemUsage string }](out) {
				if i := slices.IndexFunc(d.containers, func(c container) bool { return c.ID == s.ID }); i >= 0 {
					mem, _, _ := strings.Cut(s.MemUsage, " / ")
					d.containers[i].cpu, d.containers[i].mem = s.CPUPerc, mem
				}
			}
		}
	}
	slices.SortStableFunc(d.containers, func(a, b container) int { return strings.Compare(a.name(), b.name()) })
	return d, nil
}

// short is a docker id cut to the 12 characters docker itself shows.
func short(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// parseSize reads a size as docker prints it ("412MB", "1.8GB", "0B"), for adding sizes up.
func parseSize(s string) float64 {
	s = strings.TrimSpace(s)
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i < 0 {
		i = len(s)
	}
	n, _ := strconv.ParseFloat(s[:i], 64)
	mult := map[string]float64{"B": 1, "KB": 1e3, "KIB": 1024, "MB": 1e6, "MIB": 1 << 20, "GB": 1e9, "GIB": 1 << 30, "TB": 1e12}
	if m, ok := mult[strings.ToUpper(strings.TrimSpace(s[i:]))]; ok {
		n *= m
	}
	return n
}

// size writes bytes the way docker does: 412 MB, 1.8 GB.
func size(b float64) string {
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.1f GB", b/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.0f MB", b/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.0f kB", b/1e3)
	}
	return fmt.Sprintf("%.0f B", b)
}
