package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// siteConfig is site/site.json: the one public-repository switch and the
// addresses that depend on it.
type siteConfig struct {
	// PublicRepository turns on everything that needs the public
	// repository: GitHub links, the star button and its count, release and
	// issue links, "Report a problem" and links to repository files. While
	// it is false, no page links to GitHub or asks GitHub for anything.
	PublicRepository bool `json:"public_repository"`
	// Repository is the public repository's address, used only with the
	// switch on.
	Repository string `json:"repository"`
	// SiteURL is the site's own address (no trailing slash), used for the
	// social preview image and canonical links. Empty: the build uses
	// Cloudflare's CF_PAGES_URL, or leaves those tags out.
	SiteURL string `json:"site_url"`
}

// homeCopy is site/content/home.json: every sentence of the home page and
// the shared page parts. Numbers appear only as {id} placeholders that
// name an entry of the number registry.
type homeCopy struct {
	Lead          string      `json:"lead"`
	Sublead       string      `json:"sublead"`
	Eyebrow       string      `json:"eyebrow"`
	StatusShort   string      `json:"status_short"`
	DecidesShort  []shortItem `json:"decides_short"`
	DecidesIntro  string      `json:"decides_intro"`
	VerifiedShort []shortItem `json:"verified_short"`
	VerifiedIntro string      `json:"verified_intro"`
	CardsTitle    string      `json:"cards_title"`
	Cards         []card      `json:"cards"`
	Reads         reads       `json:"reads"`
	Install       install     `json:"install"`
	Terminal      string      `json:"terminal_note"`
	Features      []feature   `json:"features"`
	FeatTitle     string      `json:"features_title"`
	FeatIntro     string      `json:"features_intro"`
	Decides       []decision  `json:"decides"`
	Verified      []verified  `json:"verified"`
	Gallery       string      `json:"gallery_title"`
	Coming        []string    `json:"coming"`
	Footer        string      `json:"footer"`
}

// card is one measured number under the hero.
type card struct {
	Number string `json:"number"` // registry id of the big number
	// NumberTo, when set, is the registry id of the upper end of a range:
	// the card shows "{number}–{number_to}", each end checked against its
	// own quote like any other number.
	NumberTo string `json:"number_to"`
	// RangeSep joins the two ends of a range: an en dash by default, an
	// arrow for a before-and-after pair such as "53 → 62".
	RangeSep string `json:"range_sep"`
	Label  string `json:"label"`
	Text   string `json:"text"`
	Source string `json:"source"` // published evidence page (repository path)
	// Anchor is an optional heading id on the evidence page, for a card
	// whose number is one section of a longer page; the link check
	// verifies it.
	Anchor string `json:"source_anchor"`
	Link   string `json:"source_label"`
	// Baseline is a small note next to the number, such as the value
	// without the MCP server, so the number reads at a glance.
	Baseline string `json:"baseline"`
}

// reads is the compact "what it reads" row under the number cards: each
// language with its main frameworks, as the README status table lists them.
type reads struct {
	Title     string     `json:"title"`
	Note      string     `json:"note"`
	Link      string     `json:"link"`
	Page      string     `json:"page"`
	Languages []language `json:"languages"`
}

type language struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// shortItem is one line of a short home-page list, linking a published
// docs page (repository path) or a site page ("/...").
type shortItem struct {
	Text string `json:"text"`
	Page string `json:"page"`
}

// feature is one built section of the product in "What it does": one
// checked sentence, the docs page that states it, and optionally a
// screenshot named by its manifest file.
type feature struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	Page  string `json:"page"`
	Shot  string `json:"shot"`
}

// install is the pre-release build-from-source block.
type install struct {
	Note      string   `json:"note"`
	NoSource  string   `json:"no_source"`
	Clone     []string `json:"clone"`
	Build     []string `json:"build"`
	BuildNote string   `json:"build_comment"`
	Then      string   `json:"then"`
	Run       []string `json:"run"`
	ServeNote string   `json:"serve_comment"`
	More      string   `json:"more"`
	// NoteRelease and ThenRelease replace Note and Then on the home page
	// once the release exists (public-repository switch on): building
	// from source is then the alternative to the download, and the run
	// step follows a download or go install as well as a build, so it
	// cannot say bin/.
	NoteRelease string `json:"note_release"`
	ThenRelease string `json:"then_release"`
}

// decision is one card of "How Graphvein decides".
type decision struct {
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Rank  string `json:"rank"`
	Body  string `json:"body"`
}

// verified is one item of "How it is verified": it links a published page,
// a repository path (switch on only) or nothing.
type verified struct {
	Title   string `json:"title"`
	Body    string `json:"body"`
	Page    string `json:"page"`
	Repo    string `json:"repo"`
	Planned bool   `json:"planned"`
}

// number is one entry of site/content/numbers.json. The build fails when
// Quote is not in File, or when a digit of Show is not in Quote.
type number struct {
	ID    string `json:"id"`
	Show  string `json:"show"`
	File  string `json:"file"`
	Quote string `json:"quote"`
}

// mediaManifest is site/content/media.json: every image and terminal
// transcript the site shows, with how it was made.
type mediaManifest struct {
	Hero     string      `json:"hero"`
	Social   string      `json:"social"`
	Terminal string      `json:"terminal"`
	Gallery  []string    `json:"gallery"`
	Files    []mediaFile `json:"files"`
	Assets   []asset     `json:"assets"`
}

// mediaFile is one manifest entry of a screenshot, a transcript or an
// image derived from a screenshot.
type mediaFile struct {
	File              string   `json:"file"`
	Kind              string   `json:"kind"` // screenshot, terminal or derived
	MainCommit        string   `json:"main_commit"`
	ExtractorVersion  string   `json:"extractor_version"`
	Corpus            string   `json:"corpus"`
	CorpusRepository  string   `json:"corpus_repository"`
	CorpusPin         string   `json:"corpus_pin"`
	Viewport          viewport `json:"viewport"`
	DeviceScaleFactor float64  `json:"device_scale_factor"`
	Theme             string   `json:"theme"`
	Shown             string   `json:"shown"`
	Command           string   `json:"command"`
	DerivedFrom       string   `json:"derived_from"`
	Text              string   `json:"text"` // words drawn on a derived image
	Crop              *cropBox `json:"crop,omitempty"`
	// Dark names the dark-theme twin of a light screenshot: the same view,
	// layout and crop box, taken with the app in dark mode. The site shows
	// screenshots in the theme opposite to the page's (a dark screenshot on
	// a light page and the other way round), so both read as pictures of
	// the app rather than as part of the page.
	Dark string `json:"dark,omitempty"`
	// DemoEdit marks a screenshot of a change made only to show the
	// feature (a one-line edit in a copy of the corpus, not an upstream
	// change). Its caption must say so.
	DemoEdit bool   `json:"demo_edit,omitempty"`
	Alt      string `json:"alt"`
	Caption  string `json:"caption"`
}

type viewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// asset is an image that is not a screenshot (logo, icons).
type asset struct {
	File   string `json:"file"`
	Source string `json:"source"`
}

// docsConfig is site/content/docs.json: which public docs become pages,
// the pages held back, and the docs sidebar.
type docsConfig struct {
	Sources  []string       `json:"sources"`
	Withheld []withheldPage `json:"withheld"`
	Sidebar  []sidebarGroup `json:"sidebar"`
}

// withheldPage is a source page not published yet, with the reason.
type withheldPage struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type sidebarGroup struct {
	Group string        `json:"group"`
	Items []sidebarItem `json:"items"`
}

// sidebarItem names a published page (Page) or a generated one (URL).
type sidebarItem struct {
	Page  string `json:"page"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// content is everything read from the site directory.
type content struct {
	Config  siteConfig
	Home    homeCopy
	Numbers []number
	Media   mediaManifest
	Docs    docsConfig
}

func loadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}

// loadContent reads site.json and the content files of a site directory.
func loadContent(siteDir string) (*content, error) {
	c := &content{}
	files := []struct {
		name string
		v    any
	}{
		{"site.json", &c.Config},
		{"content/home.json", &c.Home},
		{"content/numbers.json", &c.Numbers},
		{"content/media.json", &c.Media},
		{"content/docs.json", &c.Docs},
	}
	for _, f := range files {
		if err := loadJSON(filepath.Join(siteDir, filepath.FromSlash(f.name)), f.v); err != nil {
			return nil, err
		}
	}
	if c.Config.PublicRepository && !strings.HasPrefix(c.Config.Repository, "https://github.com/") {
		return nil, fmt.Errorf("site.json: repository must be a https://github.com/ address")
	}
	c.Config.SiteURL = strings.TrimSuffix(c.Config.SiteURL, "/")
	return c, nil
}

var placeholder = regexp.MustCompile(`\{([a-z0-9-]+)\}`)

// expand replaces {id} placeholders with the registry's text for id.
func (c *content) expand(s string) (string, error) {
	var missing []string
	out := placeholder.ReplaceAllStringFunc(s, func(m string) string {
		id := m[1 : len(m)-1]
		for _, n := range c.Numbers {
			if n.ID == id {
				return n.Show
			}
		}
		missing = append(missing, id)
		return m
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("unknown number id %s in %q", strings.Join(missing, ", "), s)
	}
	return out, nil
}
