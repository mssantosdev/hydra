package git

import "testing"

func TestBranchNameFromRef(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "local branch with slash", ref: "refs/heads/marcus/feat-onboarding", want: "marcus/feat-onboarding"},
		{name: "local branch simple", ref: "refs/heads/stage", want: "stage"},
		{name: "remote branch with slash", ref: "refs/remotes/origin/feature/test", want: "feature/test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := branchNameFromRef(tt.ref); got != tt.want {
				t.Fatalf("branchNameFromRef(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}
