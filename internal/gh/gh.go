// Package gh reads and opens a branch's pull request with the GitHub CLI (gh). It runs on your
// machine, so none of it costs agent tokens.
package gh

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Check states.
const (
	Pass    = "pass"
	Fail    = "fail"
	Pending = "pending"
	Skip    = "skip"
)

type Check struct {
	Name, State, URL string
	Started, Done    time.Time
}

type Comment struct {
	Author, Body, URL string
	At                time.Time
}

type Review struct {
	Comment
	State string // APPROVED, CHANGES_REQUESTED, COMMENTED, …
}

// Thread is a conversation on a line of the diff.
type Thread struct {
	Path               string
	Line               int
	Resolved, Outdated bool
	Comments           []Comment
}

type PR struct {
	Number                                int
	Title, URL, State, Base               string // State: OPEN, CLOSED, MERGED
	Draft                                 bool
	Mergeable, MergeState, ReviewDecision string
	Additions, Deletions                  int
	Checks                                []Check
	Reviews                               []Review
	Comments                              []Comment
	Threads                               []Thread
	ThreadsErr                            string // line comments couldn't be read
}

func Installed() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// run runs gh in dir and returns its output, or an error with what it printed.
func run(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, errors.New(msg)
		}
	}
	return out, err
}

// View returns the open or latest PR for branch, or nil when the branch has none. With threads it
// also reads the line comments (one more API call).
func View(dir, branch string, threads bool) (*PR, error) {
	out, err := run(dir, "pr", "view", branch, "--json",
		"number,title,url,state,baseRefName,isDraft,mergeable,mergeStateStatus,reviewDecision,additions,deletions,statusCheckRollup,latestReviews,comments")
	if err != nil {
		if strings.Contains(err.Error(), "no pull requests found") {
			return nil, nil
		}
		return nil, err
	}
	type author struct{ Login string }
	var raw struct {
		Number                                      int
		Title, URL, State, BaseRefName              string
		IsDraft                                     bool
		Mergeable, MergeStateStatus, ReviewDecision string
		Additions, Deletions                        int
		StatusCheckRollup                           []struct {
			Name, Context, WorkflowName, Status, Conclusion, State string
			DetailsURL, TargetURL                                  string
			StartedAt, CompletedAt                                 time.Time
		}
		LatestReviews []struct {
			Author      author
			State, Body string
			SubmittedAt time.Time
		}
		Comments []struct {
			Author    author
			Body, URL string
			CreatedAt time.Time
		}
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	pr := &PR{Number: raw.Number, Title: raw.Title, URL: raw.URL, State: raw.State, Base: raw.BaseRefName,
		Draft: raw.IsDraft, Mergeable: raw.Mergeable, MergeState: raw.MergeStateStatus, ReviewDecision: raw.ReviewDecision,
		Additions: raw.Additions, Deletions: raw.Deletions}
	for _, c := range raw.StatusCheckRollup {
		ch := Check{Name: c.Name, URL: c.DetailsURL, Started: c.StartedAt, Done: c.CompletedAt}
		if c.Context != "" { // a commit status rather than a check run
			ch.Name, ch.URL = c.Context, c.TargetURL
			switch c.State {
			case "SUCCESS":
				ch.State = Pass
			case "PENDING", "EXPECTED":
				ch.State = Pending
			default:
				ch.State = Fail
			}
		} else {
			if c.WorkflowName != "" {
				ch.Name = c.WorkflowName + " / " + c.Name
			}
			switch {
			case c.Status != "COMPLETED":
				ch.State = Pending
			case c.Conclusion == "SUCCESS":
				ch.State = Pass
			case c.Conclusion == "NEUTRAL" || c.Conclusion == "SKIPPED":
				ch.State = Skip
			default:
				ch.State = Fail
			}
		}
		pr.Checks = append(pr.Checks, ch)
	}
	for _, r := range raw.LatestReviews {
		pr.Reviews = append(pr.Reviews, Review{Comment{r.Author.Login, r.Body, "", r.SubmittedAt}, r.State})
	}
	for _, c := range raw.Comments {
		pr.Comments = append(pr.Comments, Comment{c.Author.Login, c.Body, c.URL, c.CreatedAt})
	}
	if threads {
		if pr.Threads, err = readThreads(dir, pr); err != nil {
			pr.ThreadsErr = err.Error()
		}
	}
	return pr, nil
}

const threadsQuery = `query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviewThreads(first: 100) {
        nodes {
          isResolved isOutdated path line originalLine
          comments(first: 50) { nodes { author { login } body url createdAt } }
        }
      }
    }
  }
}`

// readThreads reads the PR's line comments, which `gh pr view` doesn't return.
// ponytail: the first 100 threads of 50 comments each; page if a PR ever has more.
func readThreads(dir string, pr *PR) ([]Thread, error) {
	u, err := url.Parse(pr.URL)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("unexpected PR URL %s", pr.URL)
	}
	args := []string{"api", "graphql", "-f", "query=" + threadsQuery,
		"-f", "owner=" + parts[0], "-f", "repo=" + parts[1], "-F", "number=" + strconv.Itoa(pr.Number)}
	if u.Host != "github.com" {
		args = append(args, "--hostname", u.Host)
	}
	out, err := run(dir, args...)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved, IsOutdated bool
							Path                   string
							Line, OriginalLine     int
							Comments               struct {
								Nodes []struct {
									Author    struct{ Login string }
									Body, URL string
									CreatedAt time.Time
								}
							}
						}
					}
				}
			}
		}
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	var threads []Thread
	for _, n := range raw.Data.Repository.PullRequest.ReviewThreads.Nodes {
		t := Thread{Path: n.Path, Line: n.Line, Resolved: n.IsResolved, Outdated: n.IsOutdated}
		if t.Line == 0 {
			t.Line = n.OriginalLine // outdated threads lose their current line
		}
		for _, c := range n.Comments.Nodes {
			t.Comments = append(t.Comments, Comment{c.Author.Login, c.Body, c.URL, c.CreatedAt})
		}
		threads = append(threads, t)
	}
	return threads, nil
}

var actionsURL = regexp.MustCompile(`/actions/runs/(\d+)(?:/job/(\d+))?`)

// FailedLog returns the failing steps' log of a GitHub Actions check, last lines first cut.
func FailedLog(dir, checkURL string) (string, error) {
	m := actionsURL.FindStringSubmatch(checkURL)
	if m == nil {
		return "", errors.New("not a GitHub Actions check · o opens it in the browser")
	}
	args := []string{"run", "view", m[1], "--log-failed"}
	if m[2] != "" {
		args = []string{"run", "view", "--job", m[2], "--log-failed"}
	}
	out, err := run(dir, args...)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	lines = lines[max(len(lines)-400, 0):]
	for i, l := range lines { // "job<TAB>step<TAB>text": keep the text
		if _, rest, ok := strings.Cut(l, "\t"); ok {
			if _, text, ok := strings.Cut(rest, "\t"); ok {
				lines[i] = text
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}

// Create pushes branch to origin and opens a PR for it, titled and described from its commits.
func Create(dir, branch string) (string, error) {
	if out, err := exec.Command("git", "-C", dir, "push", "-u", "origin", branch).CombinedOutput(); err != nil {
		return "", fmt.Errorf("git push: %s", strings.TrimSpace(string(out)))
	}
	out, err := run(dir, "pr", "create", "--fill", "--head", branch)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines[len(lines)-1], nil
}
