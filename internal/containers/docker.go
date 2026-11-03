package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"crew/internal/proc"
)

// Docker, read with the docker CLI: containers (one docker inspect for all of them), images,
// volumes, networks and the disk use summary. Nothing here draws.

// Run runs a CLI and returns its trimmed output; a failure carries what it printed.
//
// It runs in its own session, with no terminal. crew's terminal is the app's window, and a command
// started from crew would otherwise share it: a kubectl sign-in plugin (AWS SSO, kubelogin, gcloud)
// or an ssh docker host that asks on /dev/tty would read the keys you type into crew, and would hold
// the window open after crew quits. Without a terminal such a prompt fails, and the read says why.
// Stopping it (its timeout, crew quitting) kills the whole group, plugins included.
func Run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := proc.Command(ctx, name, args...).CombinedOutput()
	s := strings.TrimSpace(string(out))
	if err != nil {
		if s == "" {
			s = err.Error()
		}
		return s, fmt.Errorf("%s", s)
	}
	return s, nil
}

// Container is what docker inspect says about one container, the parts crew shows.
type Container struct {
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
	CPU    string // from docker stats, while it runs
	Mem    string
}

// Label is the container's name without docker's leading slash.
func (c Container) Label() string { return strings.TrimPrefix(c.Name, "/") }

// Project is the Compose project the container belongs to, or "".
func (c Container) Project() string { return c.Config.Labels["com.docker.compose.project"] }

// Ports are its published ports, "8080→8080".
func (c Container) Ports() string {
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

// Image is a local image, as docker images lists it.
type Image struct {
	ID, Repository, Tag, Size, CreatedSince string
}

// Dangling reports whether the image has lost its name (a rebuild took it).
func (i Image) Dangling() bool { return i.Repository == "<none>" }

// Label is how an image reads: repository:tag, or its short id when it has no name.
func (i Image) Label() string {
	if i.Dangling() {
		return Short(i.ID)
	}
	return i.Repository + ":" + i.Tag
}

// Volume is a local volume, with its size when docker system df could tell.
type Volume struct {
	Name, Driver, Labels string
	Size                 string `json:"-"`
}

// Project is the Compose project the volume was made for, or "".
func (v Volume) Project() string {
	for _, l := range strings.Split(v.Labels, ",") {
		if p, ok := strings.CutPrefix(l, "com.docker.compose.project="); ok {
			return p
		}
	}
	return ""
}

// Network is a Docker network.
type Network struct{ ID, Name, Driver, Scope string }

// Builtin reports whether docker made the network itself; it can't be removed.
func (n Network) Builtin() bool { return n.Name == "bridge" || n.Name == "host" || n.Name == "none" }

// DiskUse is one line of docker system df: Images, Containers, Local Volumes or Build Cache.
type DiskUse struct{ Type, TotalCount, Active, Size, Reclaimable string }

// DockerData is everything the DOCKER section shows, read at once.
type DockerData struct {
	Containers []Container
	Images     []Image
	Volumes    []Volume
	Networks   []Network
	Disk       map[string]DiskUse // by Type
	At         time.Time
}

// UsedBy counts the containers that use an image, a volume or a network.
func (d DockerData) UsedBy(match func(c Container) bool) []string {
	var names []string
	for _, c := range d.Containers {
		if match(c) {
			names = append(names, c.Label())
		}
	}
	return names
}

func (d DockerData) ImageUsers(i Image) []string {
	return d.UsedBy(func(c Container) bool { return c.ImageID == i.ID })
}

func (d DockerData) VolumeUsers(v Volume) []string {
	return d.UsedBy(func(c Container) bool {
		return slices.ContainsFunc(c.Mounts, func(m struct{ Type, Name string }) bool { return m.Type == "volume" && m.Name == v.Name })
	})
}

func (d DockerData) NetworkUsers(n Network) []string {
	return d.UsedBy(func(c Container) bool { _, ok := c.NetworkSettings.Networks[n.Name]; return ok })
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

// LoadDocker reads containers, images, volumes, networks and disk use. stats adds each running
// container's CPU and memory, which takes docker a second or two, so only the Containers tab asks.
func LoadDocker(ctx context.Context, stats bool) (DockerData, error) {
	d := DockerData{Disk: map[string]DiskUse{}, At: time.Now()}
	ids, err := Run(ctx, "docker", "ps", "-aq", "--no-trunc")
	if err != nil {
		return d, err
	}
	if ids != "" {
		out, err := Run(ctx, "docker", append([]string{"inspect"}, strings.Fields(ids)...)...)
		if err != nil {
			return d, err
		}
		json.Unmarshal([]byte(out), &d.Containers)
	}
	if out, err := Run(ctx, "docker", "images", "--no-trunc", "--format", "{{json .}}"); err == nil {
		d.Images = jsonLines[Image](out)
	}
	if out, err := Run(ctx, "docker", "volume", "ls", "--format", "{{json .}}"); err == nil {
		d.Volumes = jsonLines[Volume](out)
	}
	if out, err := Run(ctx, "docker", "network", "ls", "--no-trunc", "--format", "{{json .}}"); err == nil {
		d.Networks = jsonLines[Network](out)
	}
	if out, err := Run(ctx, "docker", "system", "df", "--format", "{{json .}}"); err == nil {
		for _, u := range jsonLines[DiskUse](out) {
			d.Disk[u.Type] = u
		}
	}
	// ponytail: volume sizes come from docker system df -v, which reads every volume; drop it if
	// that gets slow with many large volumes.
	if out, err := Run(ctx, "docker", "system", "df", "-v", "--format", "{{json .Volumes}}"); err == nil {
		var sized []struct{ Name, Size string }
		json.Unmarshal([]byte(out), &sized)
		for _, s := range sized {
			if i := slices.IndexFunc(d.Volumes, func(v Volume) bool { return v.Name == s.Name }); i >= 0 {
				d.Volumes[i].Size = s.Size
			}
		}
	}
	if stats && slices.ContainsFunc(d.Containers, func(c Container) bool { return c.State.Running }) {
		if out, err := Run(ctx, "docker", "stats", "--no-stream", "--no-trunc", "--format", "{{json .}}"); err == nil {
			for _, s := range jsonLines[struct{ ID, CPUPerc, MemUsage string }](out) {
				if i := slices.IndexFunc(d.Containers, func(c Container) bool { return c.ID == s.ID }); i >= 0 {
					mem, _, _ := strings.Cut(s.MemUsage, " / ")
					d.Containers[i].CPU, d.Containers[i].Mem = s.CPUPerc, mem
				}
			}
		}
	}
	slices.SortStableFunc(d.Containers, func(a, b Container) int { return strings.Compare(a.Label(), b.Label()) })
	return d, nil
}

// Short is a docker id cut to the 12 characters docker itself shows.
func Short(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// ParseSize reads a size as docker prints it ("412MB", "1.8GB", "0B"), for adding sizes up.
func ParseSize(s string) float64 {
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

// Size writes bytes the way docker does: 412 MB, 1.8 GB.
func Size(b float64) string {
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
