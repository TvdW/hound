package vcs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultRef = "master"
const autoGeneratedAttribute = "linguist-generated"

var headBranchRegexp = regexp.MustCompile(`HEAD branch: (?P<branch>.+)`)

func init() {
	Register(newGit, "git")
}

type GitDriver struct {
	DetectRef     bool   `json:"detect-ref"`
	Ref           string `json:"ref"`
	refDetetector refDetetector
}

type refDetetector interface {
	detectRef(dir string) string
}

type headBranchDetector struct {
}

func newGit(b []byte) (Driver, error) {
	var d GitDriver

	if b != nil {
		if err := json.Unmarshal(b, &d); err != nil {
			return nil, err
		}
	}

	d.refDetetector = &headBranchDetector{}

	return &d, nil
}

func (g *GitDriver) HeadRev(dir string) (string, error) {
	cmd := exec.Command(
		"git",
		"rev-parse",
		"HEAD")
	cmd.Dir = dir
	r, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	defer r.Close()

	if err := cmd.Start(); err != nil {
		return "", err
	}

	var buf bytes.Buffer

	if _, err := io.Copy(&buf, r); err != nil {
		return "", err
	}

	return strings.TrimSpace(buf.String()), cmd.Wait()
}

func run(desc, dir, cmd string, args ...string) (string, error) {
	c := exec.Command(cmd, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		log.Printf(
			"Failed to %s %v at %q, see output below\n%s: %+v\nContinuing...",
			desc,
			c.Args, c.Dir,
			out, err)
	}

	return string(out), nil
}

func (g *GitDriver) Pull(dir string) (string, error) {
	targetRef := g.targetRef(dir)

	if _, err := run("git fetch", dir,
		"git",
		"fetch",
		"--prune",
		"--no-tags",
		"--depth", "1",
		"origin",
		fmt.Sprintf("+%s:remotes/origin/%s", targetRef, targetRef)); err != nil {
		return "", err
	}

	if _, err := run("git reset", dir,
		"git",
		"reset",
		"--hard",
		fmt.Sprintf("origin/%s", targetRef)); err != nil {
		return "", err
	}

	return g.HeadRev(dir)
}

func (g *GitDriver) targetRef(dir string) string {
	var targetRef string
	if g.Ref != "" {
		targetRef = g.Ref
	} else if g.DetectRef {
		targetRef = g.refDetetector.detectRef(dir)
	}

	if targetRef == "" {
		targetRef = defaultRef
	}

	return targetRef
}

func (g *GitDriver) Clone(dir, url string) (string, error) {
	par, rep := filepath.Split(dir)
	cmd := exec.Command(
		"git",
		"clone",
		"--depth", "1",
		url,
		rep)
	cmd.Dir = par
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to clone %s, see output below\n%sContinuing...", url, out)
		return "", err
	}

	return g.Pull(dir)
}

func (g *GitDriver) SpecialFiles() []string {
	return []string{
		".git",
	}
}

func (g *GitDriver) AutoGeneratedFiles(dir string) []string {
	var files []string

	filesCmd := exec.Command("git", "ls-files", "-z")
	filesCmd.Dir = dir
	pipe, err := filesCmd.StdoutPipe()

	if err != nil {
		log.Printf("Error occured when running git ls-files in %s: %s.", dir, err)
		return files
	}

	if err := filesCmd.Start(); err != nil {
		log.Printf("Error occured when running git ls-files in %s: %s.", dir, err)
		return files
	}

	attributesCmd := exec.Command("git", "check-attr", "--stdin", "-z", autoGeneratedAttribute)
	attributesCmd.Dir = dir
	attributesCmd.Stdin = pipe

	out, err := attributesCmd.Output()

	if err != nil {
		log.Printf("Error occured when running git check-attr in %s: %s.", dir, err)
		return files
	}

	// Split by NUL and we expect the format: <path> NUL <attribute> NUL <info> NUL
	tokens := bytes.Split(out, []byte{0})

	for i := 2; i < len(tokens); i += 3 {
		if string(tokens[i]) == "true" && string(tokens[i-1]) == autoGeneratedAttribute {
			files = append(files, string(tokens[i-2]))
		}
	}

	if err := filesCmd.Wait(); err != nil {
		log.Printf("Error occured when running git ls-files in %s: %s.", dir, err)
		return files
	}

	return files
}

func (d *headBranchDetector) detectRef(dir string) string {
	output, err := run("git show remote info", dir,
		"git",
		"remote",
		"show",
		"origin",
	)

	if err != nil {
		log.Printf(
			"error occured when fetching info to determine target ref in %s: %s. Will fall back to default ref %s",
			dir,
			err,
			defaultRef,
		)
		return ""
	}

	matches := headBranchRegexp.FindStringSubmatch(output)
	if len(matches) != 2 {
		log.Printf(
			"could not determine target ref in %s. Will fall back to default ref %s",
			dir,
			defaultRef,
		)
		return ""
	}

	return matches[1]
}
