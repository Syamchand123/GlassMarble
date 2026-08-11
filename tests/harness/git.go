package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitInit initializes a git repository in the sandbox and makes an initial
// commit so HEAD exists. Fails the test on git errors.
func (s *Sandbox) GitInit() {
	s.T.Helper()
	s.RequireGit()
	s.MustGit("init", "-q", "-b", "main")
	s.GitConfig("user.email", "test@glassmarble.local")
	s.GitConfig("user.name", "GlassMarble Test")
	s.GitCommit("initial commit")
}

// GitConfig sets a git config value for the sandbox repo.
func (s *Sandbox) GitConfig(key, value string) {
	s.T.Helper()
	s.MustGit("config", key, value)
}

// GitCommit stages everything and commits; returns the full commit hash.
func (s *Sandbox) GitCommit(message string) string {
	s.T.Helper()
	s.RequireGit()
	_ = s.MustGit("add", "-A")
	s.MustGit("commit", "-q", "-m", message, "--allow-empty")
	hash := s.MustGit("rev-parse", "HEAD")
	return strings.TrimSpace(hash)
}

// GitCommitFiles writes files first, then commits them atomically.
func (s *Sandbox) GitCommitFiles(message string, files map[string]string) string {
	s.T.Helper()
	for rel, content := range files {
		s.WriteFile(rel, content)
	}
	return s.GitCommit(message)
}

// GitModifiedFiles commits only the given relative paths (the rest of the
// working tree stays dirty) and returns the new HEAD hash.
func (s *Sandbox) GitCommitOnly(message string, rels ...string) string {
	s.T.Helper()
	s.RequireGit()
	args := append([]string{"add", "--"}, rels...)
	s.MustGit(args...)
	s.MustGit("commit", "-q", "-m", message)
	return strings.TrimSpace(s.MustGit("rev-parse", "HEAD"))
}

// MustGit runs git inside the sandbox; fails the test on any error and
// returns combined output.
func (s *Sandbox) MustGit(args ...string) string {
	s.T.Helper()
	out, err := s.Git(args...)
	if err != nil {
		s.T.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// Git runs git inside the sandbox returning combined output and error.
func (s *Sandbox) Git(args ...string) (string, error) {
	s.RequireGit()
	cmd := exec.Command("git", args...)
	cmd.Dir = s.Root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// GitHead returns the current HEAD commit hash (trimmed).
func (s *Sandbox) GitHead() string {
	s.T.Helper()
	return strings.TrimSpace(s.MustGit("rev-parse", "HEAD"))
}

// GitStatusPorcelain returns `git status --porcelain` output.
func (s *Sandbox) GitStatusPorcelain() string {
	s.T.Helper()
	return s.MustGit("status", "--porcelain")
}

// TreeContents lists every file under a sandbox-relative directory, relative
// paths, sorted, for golden comparisons of what commands produced.
func (s *Sandbox) TreeContents(rel string) []string {
	s.T.Helper()
	var out []string
	root := s.Path(rel)
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(s.Root, path)
		if err != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(relPath))
		return nil
	})
	return out
}
