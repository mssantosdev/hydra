package cmd

import (
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/trust"
)

// Every worktree must appear on its own row with its branch, whatever the optional columns
// are. The rows are the contract; the styling is not, so this asserts content and never
// escape sequences.
func TestTableRendersEveryWorktreeOnItsOwnRow(t *testing.T) {
	items := []worktreeJSON{
		{Name: "api", Branch: "main", Path: "/ws/backend/api"},
		{Name: "api-stage", Branch: "stage", Path: "/ws/backend/api-stage"},
		{Name: "web", Branch: "feat/a-very-long-branch-name-that-would-wrap", Path: "/ws/frontend/web"},
	}
	got := worktreeTable(0, items, worktreeTableOpts{})
	for _, item := range items {
		if !strings.Contains(got, item.Name) {
			t.Errorf("row for %q is missing:\n%s", item.Name, got)
		}
		if !strings.Contains(got, item.Branch) {
			t.Errorf("branch %q is missing:\n%s", item.Branch, got)
		}
	}
	if lines := strings.Count(strings.TrimSpace(got), "\n"); lines < len(items) {
		t.Errorf("expected at least one line per worktree plus a header, got %d:\n%s", lines+1, got)
	}
}

func TestTableHeadersFollowTheRequestedColumns(t *testing.T) {
	items := []worktreeJSON{{Name: "api", Branch: "main", Path: "/ws/backend/api"}}

	base := worktreeTable(0, items, worktreeTableOpts{})
	if !strings.Contains(base, "WORKTREE") || !strings.Contains(base, "BRANCH") {
		t.Fatalf("the two mandatory columns are missing:\n%s", base)
	}
	// An unrequested column must not be drawn at all: an empty column costs width the
	// branch and path need more, which is why they are opt-in.
	if strings.Contains(base, "TOPIC") {
		t.Errorf("TOPIC was drawn without being requested:\n%s", base)
	}

	withTopic := worktreeTable(0, items, worktreeTableOpts{IncludeTopic: true})
	if !strings.Contains(withTopic, "TOPIC") {
		t.Errorf("IncludeTopic did not add the column:\n%s", withTopic)
	}
}

// An unassigned topic renders as a dash, never as blank: an empty cell is ambiguous with a
// missing value, and an agent reading the text form cannot tell them apart.
func TestTopicLabelNeverRendersBlank(t *testing.T) {
	if got := topicLabelJSON(worktreeJSON{}); strings.TrimSpace(got) == "" {
		t.Error("an unassigned topic rendered as blank")
	}
	if got := topicLabelJSON(worktreeJSON{Topic: new("2072958")}); got != "2072958" {
		t.Errorf("topic label = %q, want the id", got)
	}
}

func TestAgainstLabelDistinguishesMergedFromAhead(t *testing.T) {
	tests := []struct {
		name string
		item worktreeJSON
		want string
	}{
		{"no comparison", worktreeJSON{}, "—"},
		{"merged", worktreeJSON{Against: &againstJSON{Ref: "main", Merged: true}}, "merged"},
		{"ahead", worktreeJSON{Against: &againstJSON{Ref: "main", Ahead: 3}}, "+3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compare on content, not on the styled string: the colour is a choice, the
			// number is the answer.
			if got := againstLabelJSON(tt.item); !strings.Contains(got, tt.want) {
				t.Errorf("againstLabelJSON = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

// Below the readable minimum the table must size to its content rather than being forced into
// a width nothing fits in.
func TestTableWidthCollapsesRatherThanForcingAnUnreadableWidth(t *testing.T) {
	t.Setenv("COLUMNS", "20")
	if got := tableWidth(); got != 0 {
		t.Errorf("tableWidth() = %d at 20 columns, want 0 so the table sizes to content", got)
	}
	t.Setenv("COLUMNS", "200")
	if got := tableWidth(); got <= 0 {
		t.Errorf("tableWidth() = %d at 200 columns, want a positive usable width", got)
	}
}

func TestGroupLabelNamesTheGroupInUpperCase(t *testing.T) {
	got := groupLabel("backend")
	if !strings.Contains(got, "BACKEND") {
		t.Errorf("groupLabel(%q) = %q, want it to name the group", "backend", got)
	}
}

// An empty listing must render without panicking and without pretending there are rows.
func TestTableHandlesAnEmptyListing(t *testing.T) {
	got := worktreeTable(0, nil, worktreeTableOpts{IncludeTopic: true, IncludeAgainst: true})
	for _, name := range []string{"api", "main"} {
		if strings.Contains(got, name) {
			t.Errorf("empty listing produced a row mentioning %q:\n%s", name, got)
		}
	}
}

func TestTrustShowSummaryNamesWhatIsWrong(t *testing.T) {
	tests := []struct {
		name    string
		payload trustJSON
		want    string
	}{
		{"nothing executable", trustJSON{}, "executes nothing"},
		{"trusted", trustJSON{Executable: 1, Trusted: true, ApprovedAt: "t"}, "trusted"},
		{"stale", trustJSON{Executable: 1, Reason: trust.ReasonChanged, Changed: []string{"a", "b"}}, "changed"},
		{"never", trustJSON{Executable: 1}, "not trusted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trustShowSummary(tt.payload); !strings.Contains(got, tt.want) {
				t.Errorf("summary = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

// A store hydra refuses to honour is a machine problem, not a manifest problem, so it must not
// be reported as though the manifest were at fault.
func TestClassifyTrustErrNamesTheStoreNotTheManifest(t *testing.T) {
	err := classifyTrustErr(&trust.ErrUnsafeStore{Path: "/cfg/trust.yaml", Reason: "it is a symlink"})
	e := output.Classify(err)
	if e.Code != output.CodeConfigInvalid {
		t.Errorf("code = %q, want %q", e.Code, output.CodeConfigInvalid)
	}
	if got := e.Details["path"]; got != "/cfg/trust.yaml" {
		t.Errorf("details.path = %v, want the store path", got)
	}
	if !strings.Contains(e.Message, "symlink") {
		t.Errorf("message does not say what is wrong: %q", e.Message)
	}
}

func TestPluralAgreesWithTheCount(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{{0, "values"}, {1, "value"}, {2, "values"}} {
		if got := plural(tc.n, "value", "values"); got != tc.want {
			t.Errorf("plural(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

func TestFirstNonEmptyStringPrefersTheFlagOverTheEnvironment(t *testing.T) {
	if got := firstNonEmptyString("", "from-env"); got != "from-env" {
		t.Errorf("got %q, want the environment value when the flag is empty", got)
	}
	if got := firstNonEmptyString("from-flag", "from-env"); got != "from-flag" {
		t.Errorf("got %q, want the flag to win", got)
	}
	if got := firstNonEmptyString("", ""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
