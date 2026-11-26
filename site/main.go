// Command site builds the Graphvein website from the public files of the
// repository into a folder of static files, and refuses to write it when
// a check fails: a number not quoted from its evidence page, a screenshot
// not taken with the current extractor version, a private path or a word
// about how the work is organised, a link that does not resolve, or a
// README that no longer shares the home page's lead.
//
// Usage, from site/:
//
//	go run . [-root ..] [-out dist] [-site-url https://...]
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
)

func main() {
	root := flag.String("root", "..", "repository root")
	out := flag.String("out", "dist", "output folder (replaced)")
	siteURL := flag.String("site-url", "", "the site's address, when site.json has none (for the social preview image)")
	flag.Parse()
	if err := run(*root, *out, *siteURL); err != nil {
		slog.Error("site build failed", "err", err)
		os.Exit(1)
	}
}

func run(root, out, siteURL string) error {
	s, err := build(options{Root: root, Site: ".", Theme: "theme", SiteURL: siteURL})
	if err != nil {
		return err
	}
	for _, n := range s.notes {
		slog.Warn(n)
	}
	if len(s.problems) > 0 {
		for _, p := range s.problems {
			fmt.Fprintln(os.Stderr, "site:", p)
		}
		return fmt.Errorf("%d check(s) failed; nothing written", len(s.problems))
	}
	if err := s.write(out); err != nil {
		return err
	}
	slog.Info("site written", "dir", out, "files", len(s.out), "pages", len(s.pages))
	return nil
}
