package collect

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
)

const dockerCategory = "docker"

// builderPrunePath is the Path of the build-cache candidate: an action, not a
// filesystem path. Its ReclaimCmd is the command to run (-f only skips the
// confirmation prompt; images are never force-removed).
const builderPrunePath = "docker builder prune"

// Docker collects unused Docker images, build cache and volumes.
//
// It reads `docker image ls`, `docker ps -a`, `docker volume ls` and
// `docker system df` as `--format '{{json .}}'`, `docker system df -v` for
// volume sizes, plus `docker image inspect` per image for labels. It never reads CreatedAt: age is judged from history.
//
// Observation shape (for the history writer): an image with a container yields
// {Key: "docker:<id>", At: now}; an unused image with no history entry yields
// the same with FirstSeenOnly set, so it is not stale on its first run.
//
// Sizes are parsed from docker's decimal human strings ("1.5GB"), so they are
// approximate.
type Docker struct {
	Config  config.Config
	History history.History
	Now     func() time.Time
}

var _ Collector = (*Docker)(nil)

// Name returns the collector's category name.
func (d *Docker) Name() string { return dockerCategory }

type dockerImage struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	Size       string `json:"Size"`
}

func (i dockerImage) dangling() bool { return i.Repository == "<none>" && i.Tag == "<none>" }
func (i dockerImage) tagged() bool {
	return !i.dangling() && i.Repository != "<none>" && i.Tag != "<none>"
}
func (i dockerImage) name() string { return i.Repository + ":" + i.Tag }

// localImage is one image ID with every repo:tag that points at it.
// `docker image ls` prints one row per tag, so an image with several tags
// arrives as several rows sharing the ID.
type localImage struct {
	ID       string
	Size     string
	Names    []string // repo:tag references, in listing order
	Dangling bool     // every row is <none>:<none>
}

func (i localImage) tagged() bool { return len(i.Names) > 0 }

// groupImages merges the rows of `docker image ls` by ID, keeping the order
// in which each ID first appears.
func groupImages(rows []dockerImage) []localImage {
	var out []localImage
	at := map[string]int{}
	for _, r := range rows {
		k, seen := at[r.ID]
		if !seen {
			k = len(out)
			at[r.ID] = k
			out = append(out, localImage{ID: r.ID, Size: r.Size, Dangling: true})
		}
		img := &out[k]
		img.Dangling = img.Dangling && r.dangling()
		if r.tagged() {
			img.Names = append(img.Names, r.name())
		}
	}
	return out
}

// Collect gathers Docker candidates. If the docker CLI is missing or any of
// the listings fail (e.g. the daemon is down) the collector is skipped with
// the reason and produces no candidates: without the container list it cannot
// tell what is safe.
func (d *Docker) Collect(ctx context.Context) Result {
	res := Result{Revalidators: map[string]func(context.Context) classify.Decision{}}
	skip := func(reason string) Result {
		return Result{
			Skipped:  []Skip{{Collector: dockerCategory, Reason: reason}},
			Coverage: []history.Coverage{{Category: dockerCategory, Complete: false}},
		}
	}

	var rows []dockerImage
	if err := dockerJSON(ctx, &rows, "image", "ls", "--no-trunc", "--all", "--format", "{{json .}}"); err != nil {
		return skip(err.Error())
	}
	images := groupImages(rows)
	var containers []struct {
		Image  string `json:"Image"`
		Names  string `json:"Names"`
		Mounts string `json:"Mounts"` // comma-separated volume names and bind paths
	}
	if err := dockerJSON(ctx, &containers, "ps", "-a", "--no-trunc", "--format", "{{json .}}"); err != nil {
		return skip(err.Error())
	}
	usedBy := map[string][]string{}
	for _, c := range containers {
		for _, m := range strings.Split(c.Mounts, ",") {
			if m != "" {
				usedBy[m] = append(usedBy[m], c.Names)
			}
		}
	}
	var volumes []struct {
		Name string `json:"Name"`
	}
	if err := dockerJSON(ctx, &volumes, "volume", "ls", "--format", "{{json .}}"); err != nil {
		return skip(err.Error())
	}

	now := d.Now()
	threshold := time.Duration(d.Config.ThresholdDays(dockerCategory)) * 24 * time.Hour
	ephemeral := classify.Ephemeral{Label: d.Config.Docker.EphemeralLabel, Globs: d.Config.Docker.EphemeralGlobs}
	cov := history.Coverage{Category: dockerCategory, Complete: true, Seen: map[string]bool{}}

	for _, img := range images {
		key := dockerCategory + ":" + img.ID
		cov.Seen[key] = true
		hasContainer := false
		for _, c := range containers {
			if containerUsesImage(c.Image, img) {
				hasContainer = true
				break
			}
		}
		entry, known := d.History.Entries[key]
		switch {
		case hasContainer:
			res.Observations = append(res.Observations, Observation{Key: key, At: now})
		case !known:
			res.Observations = append(res.Observations, Observation{Key: key, At: now, FirstSeenOnly: true})
		}

		facts := classify.ImageFacts{
			Dangling: img.Dangling, Tagged: img.tagged(), HasContainer: hasContainer,
			LastSeen: entry.LastUsed, FirstSeen: entry.FirstSeen, Names: img.Names,
		}
		if !known {
			facts.FirstSeen = now
		}
		if !hasContainer {
			facts.Labels = d.labels(ctx, img.ID, &res)
		}
		dec := classify.ClassifyImage(facts, now, threshold, ephemeral)
		if dec.Tier != classify.TierGarbage && dec.Tier != classify.TierStale {
			continue
		}
		res.Revalidators[img.ID] = d.imageRevalidator(img, threshold, ephemeral)
		res.Candidates = append(res.Candidates, classify.Candidate{
			Category: classify.CategoryDocker, Tier: dec.Tier, Path: img.ID, Refs: img.Names,
			Size: parseDockerSize(img.Size), Reason: dec.Reason, LastUse: dec.LastUse,
		})
	}

	if size, ok := d.buildCacheReclaimable(ctx); ok && size > 0 {
		res.Candidates = append(res.Candidates, classify.Candidate{
			Category: classify.CategoryDocker, Tier: classify.TierGarbage, Path: builderPrunePath,
			Size: size, Reason: "reclaimable build cache", ReclaimCmd: "docker builder prune -f",
		})
	}
	usage, err := volumeUsage(ctx)
	if err != nil {
		res.Warnings = append(res.Warnings, "docker: cannot measure volumes: "+err.Error())
	}
	for _, v := range volumes {
		u := usage[v.Name]
		reason := ReasonUnusedVolume
		if u.links > 0 || len(usedBy[v.Name]) > 0 {
			reason = ReasonUsedVolume
		}
		res.Candidates = append(res.Candidates, classify.Candidate{
			Category: classify.CategoryDocker, Tier: classify.TierManual, Path: v.Name, UsedBy: usedBy[v.Name],
			Size: u.size, SizeUnknown: !u.measured, Reason: reason, ReclaimCmd: "docker volume rm " + v.Name,
		})
	}
	res.Coverage = []history.Coverage{cov}
	return res
}

// imageRevalidator re-reads the containers and labels and re-runs ClassifyImage
// with the current history. If docker fails there is no evidence, so the
// image is not deleted (TierNone).
func (d *Docker) imageRevalidator(img localImage, threshold time.Duration, eph classify.Ephemeral) func(context.Context) classify.Decision {
	return func(ctx context.Context) classify.Decision {
		var containers []struct {
			Image string `json:"Image"`
		}
		if err := dockerJSON(ctx, &containers, "ps", "-a", "--no-trunc", "--format", "{{json .}}"); err != nil {
			return classify.Decision{Tier: classify.TierNone, Reason: "cannot revalidate: " + err.Error()}
		}
		has := false
		for _, c := range containers {
			if containerUsesImage(c.Image, img) {
				has = true
				break
			}
		}
		now := d.Now()
		key := dockerCategory + ":" + img.ID
		entry, known := d.History.Entries[key]
		facts := classify.ImageFacts{
			Dangling: img.Dangling, Tagged: img.tagged(), HasContainer: has,
			LastSeen: entry.LastUsed, FirstSeen: entry.FirstSeen, Names: img.Names,
		}
		if !known {
			facts.FirstSeen = now
		}
		if !has {
			var scratch Result
			facts.Labels = d.labels(ctx, img.ID, &scratch)
		}
		return classify.ClassifyImage(facts, now, threshold, eph)
	}
}

// containerUsesImage reports whether a container's Image reference (a name, a
// short or full ID, with or without "sha256:") can refer to img. It errs
// toward true: a wrong "has container" only keeps an image.
func containerUsesImage(ref string, img localImage) bool {
	if ref == "" {
		return false
	}
	id := strings.TrimPrefix(img.ID, "sha256:")
	r := strings.TrimPrefix(ref, "sha256:")
	if strings.HasPrefix(id, r) {
		return true
	}
	for _, n := range img.Names {
		if ref == n || ref+":latest" == n {
			return true
		}
	}
	return false
}

// labels returns the image's labels; a failure is a warning and yields none.
func (d *Docker) labels(ctx context.Context, id string, res *Result) map[string]string {
	out, err := dockerRun(ctx, "image", "inspect", "--format", "{{json .Config.Labels}}", id)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("docker: cannot read labels of %s: %v", id, err))
		return nil
	}
	var labels map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(out), &labels); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("docker: bad labels for %s: %v", id, err))
		return nil
	}
	return labels
}

// Volume reasons; the report keys its prune hint on ReasonUnusedVolume.
const (
	ReasonUnusedVolume = "unused volume; may hold data, review before removing"
	ReasonUsedVolume   = "volume used by a container"
)

type volumeStat struct {
	size     int64
	measured bool // false when docker reports no size ("N/A")
	links    int  // containers, running or not, that mount the volume
}

// volumeUsage reads each volume's size and container count from
// `docker system df -v`, which measures the volumes and is slower than the
// other listings. A volume it does not size ("N/A", e.g. a non-local driver)
// is not measured.
func volumeUsage(ctx context.Context) (map[string]volumeStat, error) {
	var lists [][]struct {
		Name  string `json:"Name"`
		Size  string `json:"Size"`
		Links string `json:"Links"`
	}
	if err := dockerJSON(ctx, &lists, "system", "df", "-v", "--format", "{{json .Volumes}}"); err != nil {
		return nil, err
	}
	out := map[string]volumeStat{}
	for _, l := range lists {
		for _, v := range l {
			links, _ := strconv.Atoi(v.Links)
			st := volumeStat{measured: strings.HasSuffix(v.Size, "B"), links: links}
			if st.measured {
				st.size = parseDockerSize(v.Size)
			}
			out[v.Name] = st
		}
	}
	return out, nil
}

// buildCacheReclaimable reads the Build Cache row of `docker system df`.
func (d *Docker) buildCacheReclaimable(ctx context.Context) (int64, bool) {
	var rows []struct {
		Type        string `json:"Type"`
		Reclaimable string `json:"Reclaimable"`
	}
	if err := dockerJSON(ctx, &rows, "system", "df", "--format", "{{json .}}"); err != nil {
		return 0, false
	}
	for _, r := range rows {
		if r.Type == "Build Cache" {
			first, _, _ := strings.Cut(r.Reclaimable, " ")
			return parseDockerSize(first), true
		}
	}
	return 0, false
}

func dockerRun(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, errors.New("docker binary not found")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return out, nil
}

// dockerJSON runs docker and decodes its JSON-lines output into *[]T.
func dockerJSON[T any](ctx context.Context, dst *[]T, args ...string) error {
	out, err := dockerRun(ctx, args...)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var v T
		if err := json.Unmarshal(line, &v); err != nil {
			return fmt.Errorf("unexpected docker output: %w", err)
		}
		*dst = append(*dst, v)
	}
	return sc.Err()
}

// parseDockerSize converts docker's decimal human size ("1.5GB", "20kB",
// "0B") to bytes; unparseable input yields 0.
func parseDockerSize(s string) int64 {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		mult   float64
	}{{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3}, {"B", 1}}
	for _, u := range units {
		if num, ok := strings.CutSuffix(s, u.suffix); ok {
			f, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
			if err != nil {
				return 0
			}
			return int64(f*u.mult + 0.5)
		}
	}
	return 0
}
