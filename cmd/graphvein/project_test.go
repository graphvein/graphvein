package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
)

// projectCheckouts makes web (no repository in its graphvein.yaml) and api
// (repository shop-api) under a temporary parent.
func projectCheckouts(t *testing.T) string {
	t.Helper()
	parent := t.TempDir()
	for dir, content := range map[string]string{"web": "version: 1\n", "api": "version: 1\nrepository: shop-api\n"} {
		if err := os.MkdirAll(filepath.Join(parent, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(parent, dir, "graphvein.yaml"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return parent
}

func runProject(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(append([]string{"project"}, args...), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestProjectInitAndAdd(t *testing.T) {
	parent := projectCheckouts(t)
	dir := filepath.Join(parent, "shop")
	web, api := filepath.Join(parent, "web"), filepath.Join(parent, "api")
	code, out, errOut := runProject(t, "init", "--name", "shop", "--dir", dir, "web="+web)
	if code != 0 {
		t.Fatal(code, errOut)
	}
	if !strings.Contains(out, "project shop, 1 members") || !strings.Contains(out, "web  ../web  repository from manifest") || !strings.Contains(out, "keep "+config.ProjectLocalFile+" (machine-specific member paths) out of version control") {
		t.Fatalf("output:\n%s", out)
	}
	manifest := filepath.Join(dir, config.ProjectFile)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "path: ../web") || !strings.Contains(string(data), config.ProjectLocalFile) {
		t.Fatalf("manifest:\n%s", data)
	}
	// A comment the team added survives add; the member's own name wins.
	if err := os.WriteFile(manifest, append([]byte("# shop team\n"), data...), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = runProject(t, "add", api, "--manifest", dir)
	if code != 0 {
		t.Fatal(code, errOut)
	}
	if !strings.Contains(out, "shop-api  ../api  repository from member graphvein.yaml") {
		t.Fatalf("output:\n%s", out)
	}
	p, err := config.LoadProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Members) != 2 || p.Members[1].Repository != "shop-api" {
		t.Fatalf("project = %+v", p)
	}
	data, _ = os.ReadFile(manifest)
	if !strings.Contains(string(data), "# shop team") {
		t.Fatalf("comment lost:\n%s", data)
	}
	// Refusals leave the file as it was.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"add", "--manifest", dir, "other=" + web}, "use the same directory"},
		{[]string{"add", "--manifest", dir, "web=" + api}, "which wins"},
		{[]string{"add", "--manifest", dir, filepath.Join(parent, "missing")}, "missing"},
		{[]string{"init", "--name", "shop", "--dir", dir, api}, "already exists"},
	} {
		code, _, errOut := runProject(t, tc.args...)
		if code != 2 || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: code %d, stderr %q", tc.args, code, errOut)
		}
		after, _ := os.ReadFile(manifest)
		if !bytes.Equal(after, data) {
			t.Errorf("%v changed the manifest", tc.args)
		}
	}
}

func TestProjectInitInsideMember(t *testing.T) {
	parent := projectCheckouts(t)
	web := filepath.Join(parent, "web")
	code, out, errOut := runProject(t, "init", "--name", "shop", "--dir", web, "web="+web, filepath.Join(parent, "api"))
	if code != 0 {
		t.Fatal(code, errOut)
	}
	if !strings.Contains(out, "web       .       repository from manifest") || !strings.Contains(out, "shop-api  ../api") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestProjectRefusals(t *testing.T) {
	parent := projectCheckouts(t)
	dir := filepath.Join(parent, "shop")
	web := filepath.Join(parent, "web")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "usage:"},
		{[]string{"status"}, "usage:"},
		{[]string{"init", "--dir", dir, "web=" + web}, "usage:"},
		{[]string{"init", "--name", "shop", "--dir", dir}, "usage:"},
		{[]string{"add", "--manifest", dir}, "usage:"},
		{[]string{"add", "--name", "x", web}, "usage:"},
		{[]string{"init", "--name", "shop", "--dir", dir, web}, "the directory name is never used"},
		{[]string{"init", "--name", "shop", "--dir", dir, "=" + web}, "empty repository name"},
		{[]string{"init", "--name", "shop@ref:x", "--dir", dir, "web=" + web}, "@ref:"},
		{[]string{"init", "--name", "shop", "--dir", dir, "web=" + web, "web=" + filepath.Join(parent, "api")}, "which wins"},
		{[]string{"add", "--manifest", dir, "web=" + web}, "graphvein project init"},
	} {
		code, _, errOut := runProject(t, tc.args...)
		if code != 2 || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: code %d, stderr %q", tc.args, code, errOut)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("a refused init created %s (%v)", dir, err)
	}
	// add refuses to rewrite a manifest that is already invalid.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := []byte("version: 1\nname: shop\nmembers: []\n")
	if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), bad, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := runProject(t, "add", "--manifest", dir, "web="+web); code != 2 || !strings.Contains(errOut, "nonempty list") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}
