package context

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryIdentityKeepsHost(t *testing.T) {
	for _, remote := range []string{"git@github.example:owner/repo.git", "https://user:password@github.example/owner/repo.git", "ssh://git@github.example/owner/repo.git"} {
		r := Repository(remote)
		if r.Host != "github.example" || r.Path != "owner/repo" {
			t.Fatal(r)
		}
	}
	if Repository("/local/repo").Key() != "" {
		t.Fatal("invented host")
	}
}
func TestScopeBoundaries(t *testing.T) {
	root := t.TempDir()
	if !Within(filepath.Join(root, "deleted", "worktree"), root) {
		t.Fatal("deleted cwd lost")
	}
	if Within(root+"-other", root) {
		t.Fatal("prefix boundary bypass")
	}
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	if Within(filepath.Join(link, "deleted"), root) {
		t.Fatal("symlink ancestor escaped approval")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "missing"), link); err != nil {
		t.Fatal(err)
	}
	if Within(filepath.Join(link, "deleted"), root) {
		t.Fatal("dangling symlink approved")
	}
}
