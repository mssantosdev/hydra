package cmd

import (
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestValidateRelativeProjectPath(t *testing.T) {
	if _, err := validateRelativeProjectPath("client/api"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := validateRelativeProjectPath("../escape"); err == nil {
		t.Fatal("expected error for parent traversal")
	}
}

func TestCreateProjectRootWithoutExistingConfig(t *testing.T) {
	env := testutil.NewTestEnv(t)
	if _, _, _, err := createProjectRoot(env.RootDir, "fresh"); err != nil {
		t.Fatalf("createProjectRoot: %v", err)
	}
	if _, _, _, err := createProjectRoot(env.RootDir, "fresh"); err == nil {
		t.Fatal("expected error creating duplicate project")
	}
}
