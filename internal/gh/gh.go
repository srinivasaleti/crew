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
	ID                 string // GraphQL node id, for resolving and replying
	Path               string
	Line               int
	Resolved, Outdated bool
	Comments           []Comment
	DiffHunk           string // the code the thread is on, as GitHub shows it
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
          id isResolved isOutdated path line originalLine
          comments(first: 50) { nodes { author { login } body url createdAt diffHunk } }
        }
      }
    }
  }
}`

// graphql runs a GitHub GraphQL query for the PR's repository; vars are string variables, and
// number (when not 0) is passed as the integer $number. The repo's owner and name come from the
// PR's URL as $owner and $repo.
func graphql(dir, prURL, query string, number int, vars ...string) ([]byte, error) {
	u, err := url.Parse(prURL)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("unexpected PR URL %s", prURL)
	}
	args := []string{"api", "graphql", "-f", "query=" + query}
	if strings.Contains(query, "$owner") {
		args = append(args, "-f", "owner="+parts[0], "-f", "repo="+parts[1])
	}
	if number != 0 {
		args = append(args, "-F", "number="+strconv.Itoa(number))
	}
	for i := 0; i+1 < len(vars); i += 2 {
		args = append(args, "-f", vars[i]+"="+vars[i+1])
	}
	if u.Host != "github.com" {
		args = append(args, "--hostname", u.Host)
	}
	return run(dir, args...)
}

// readThreads reads the PR's line comments, which `gh pr view` doesn't return.
// ponytail: the first 100 threads of 50 comments each; page if a PR ever has more.
func readThreads(dir string, pr *PR) ([]Thread, error) {
	out, err := graphql(dir, pr.URL, threadsQuery, pr.Number)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							ID                     string
							IsResolved, IsOutdated bool
							Path                   string
							Line, OriginalLine     int
							Comments               struct {
								Nodes []struct {
									Author              struct{ Login string }
									Body, URL, DiffHunk string
									CreatedAt           time.Time
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
		t := Thread{ID: n.ID, Path: n.Path, Line: n.Line, Resolved: n.IsResolved, Outdated: n.IsOutdated}
		if t.Line == 0 {
			t.Line = n.OriginalLine // outdated threads lose their current line
		}
		for _, c := range n.Comments.Nodes {
			t.Comments = append(t.Comments, Comment{c.Author.Login, c.Body, c.URL, c.CreatedAt})
			if t.DiffHunk == "" {
				t.DiffHunk = c.DiffHunk
			}
		}
		threads = append(threads, t)
	}
	return threads, nil
}

// SetResolved resolves or unresolves a line-comment thread.
func SetResolved(dir, prURL, threadID string, resolved bool) error {
	op := "unresolveReviewThread"
	if resolved {
		op = "resolveReviewThread"
	}
	_, err := graphql(dir, prURL, `mutation($id: ID!) { `+op+`(input: {threadId: $id}) { thread { isResolved } } }`, 0, "id", threadID)
	return err
}

// ReplyThread adds a reply to a line-comment thread.
func ReplyThread(dir, prURL, threadID, body string) error {
	_, err := graphql(dir, prURL, `mutation($id: ID!, $body: String!) {
  addPullRequestReviewThreadReply(input: {pullRequestReviewThreadId: $id, body: $body}) { comment { id } }
}`, 0, "id", threadID, "body", body)
	return err
}

// AddComment adds a comment to the PR's conversation.
func AddComment(dir, prURL, body string) error {
	_, err := run(dir, "pr", "comment", prURL, "--body", body)
	return err
}

var actionsURL = regexp.MustCompile(`github\.com/([^/]+/[^/]+)/actions/runs/(\d+)(?:/job/(\d+))?`)

// FailedLog returns the failing steps' log of a GitHub Actions check, last lines first cut.
func FailedLog(dir, checkURL string) (string, error) {
	m := actionsURL.FindStringSubmatch(checkURL)
	if m == nil {
		return "", errors.New("not a GitHub Actions check · o opens it in the browser")
	}
	args := []string{"run", "view", m[2], "--log-failed", "-R", m[1]} // -R: no local checkout needed
	if m[3] != "" {
		args = []string{"run", "view", "--job", m[3], "--log-failed", "-R", m[1]}
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

// Item is a PR from a search: what a list of PRs shows.
type Item struct {
	Number           int
	Title, URL, Repo string // Repo is owner/name
	Author           string
	Draft            bool
	Created, Updated time.Time
	Mine             bool // you opened it (else your review is requested)
}

// Search lists open PRs matching a GitHub search, across every repo your account can see.
func Search(query ...string) ([]Item, error) {
	args := append([]string{"search", "prs"}, query...)
	args = append(args, "--state", "open", "--limit", "100", "--json", "number,title,url,repository,author,isDraft,createdAt,updatedAt")
	out, err := run("", args...)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number               int
		Title, URL           string
		Repository           struct{ NameWithOwner string }
		Author               struct{ Login string }
		IsDraft              bool
		CreatedAt, UpdatedAt time.Time
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(raw))
	for _, r := range raw {
		items = append(items, Item{r.Number, r.Title, r.URL, r.Repository.NameWithOwner, r.Author.Login, r.IsDraft, r.CreatedAt, r.UpdatedAt, false})
	}
	return items, nil
}

// ToReview lists open PRs where your review is requested from you (not from a team you're in).
func ToReview() ([]Item, error) { return Search("user-review-requested:@me") }

// Mine lists your open PRs.
func Mine() ([]Item, error) {
	items, err := Search("--author", "@me")
	for i := range items {
		items[i].Mine = true
	}
	return items, err
}

// Diff is a PR's diff, read by its URL (no local checkout needed).
func Diff(prURL string) (string, error) {
	out, err := run("", "pr", "diff", prURL, "--color", "never")
	return string(out), err
}

// ReviewComment is a comment on a line of the PR's new code.
type ReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

// PostReview posts a review as you: event is APPROVE, COMMENT or REQUEST_CHANGES; comments go on
// lines of the new code (lines the diff shows).
func PostReview(prURL, event, body string, comments []ReviewComment) error {
	u, err := url.Parse(prURL)
	if err != nil {
		return err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/") // owner/repo/pull/N
	if len(parts) < 4 {
		return fmt.Errorf("unexpected PR URL %s", prURL)
	}
	for i := range comments {
		comments[i].Side = "RIGHT"
	}
	payload, _ := json.Marshal(map[string]any{"event": event, "body": body, "comments": comments})
	args := []string{"api", "-X", "POST", fmt.Sprintf("repos/%s/%s/pulls/%s/reviews", parts[0], parts[1], parts[3]), "--input", "-"}
	if u.Host != "github.com" {
		args = append(args, "--hostname", u.Host)
	}
	cmd := exec.Command("gh", args...)
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if _, err := cmd.Output(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}
