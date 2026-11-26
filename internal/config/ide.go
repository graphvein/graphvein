package config

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// IDE configures presentation positions without changing contract byte spans.
type IDE struct {
	// Editor is the preset the settings start from (DefaultEditor when the
	// configuration names none).
	Editor string
	// Label is the button text clients show for the link: the explicit
	// label, else the preset's label while the preset's own template is in
	// use, else empty (clients then recognise the link's scheme).
	Label        string
	LinkTemplate string
	ColumnUnit   string
	ColumnBase   int
}

// Preset is the verified link format of one editor. Each row was checked
// against the editor's own source or documentation (ADR 0044):
//
//   - vscode, vscode-insiders: microsoft/vscode-docs
//     docs/configure/command-line.md ("Opening VS Code with URLs"), the
//     handler getWindowOpenableFromProtocolUrl in microsoft/vscode
//     src/vs/code/electron-main/app.ts and uriToFsPath in
//     src/vs/base/common/uri.ts (a single leading slash before the path,
//     also before a Windows drive letter).
//   - vscodium: VSCodium/vscodium prepare_vscode.sh sets urlProtocol
//     "vscodium"; the handler is VS Code's.
//   - cursor: a Cursor staff answer on forum.cursor.com (topic 166093)
//     confirms local "cursor://file/..." links; the line and column suffix,
//     the Windows path form and the column unit are not documented and are
//     assumed to be VS Code's. Cursor's source is not public, so its links
//     carry ColumnCaveat and are never exact (ADR 0050, Review fixes R2).
//   - jetbrains: JetBrains/intellij-community MacDistributionCustomizer.kt
//     ("<scheme>://open?file=/some/file/path&line=0"),
//     IdeaCommunityProperties.kt (urlSchemes "idea"), CommandLineProcessor.kt
//     (file, line, column) and PlatformProjectOpenProcessor.kt
//     (OpenFileDescriptor(project, file, line - 1, column)): a 1-based line
//     and a 0-based column; registered on macOS only, for IntelliJ IDEA only.
//     The descriptor's offset wins: OpenFileDescriptor.java creates its
//     range marker through LazyRangeMarkerFactoryImpl.java, which calls
//     DocumentUtil.calculateOffset (platform/core-impl): that walks the
//     line with charAt, one UTF-16 unit per column except a tab, which adds
//     the tab size; navigateInEditor moves the caret to that offset. So the
//     unit is utf16. The tab size is an IDE setting the server does not
//     know, so on a line with a tab before the node, and on a file that
//     changed since indexing, the link names the line only:
//     CommandLineProcessor.kt processInternalProtocol reads a missing column
//     as -1 and navigateToFileFromCommandLine (PlatformProjectOpenProcessor.kt)
//     coerces it to 0, the start of the line.
//   - zed: zed-industries/zed crates/zed/src/zed/open_listener.rs (the rest
//     after "zed://file" is URL-decoded into the path, then
//     PathWithPosition::parse_str), crates/recent_projects/src/
//     remote_connections.rs navigate_to_positions (row and column minus one)
//     and crates/text/src/text.rs point_from_external_input (the column
//     counts characters, that is code points).
//
// Status and Note state how far each row was verified, so the web UI's
// editor picker (ADR 0050) can show the caveat next to the editor; the same
// words appear in docs/config.md.
type Preset struct {
	Name         string
	LinkTemplate string
	ColumnUnit   string
	ColumnBase   int
	Label        string
	// Status is one of the PresetStatus values.
	Status string
	// Note is one plain sentence on what was verified and what is assumed.
	Note string
	// LineTemplate is LinkTemplate without the column, for editors whose
	// column cannot always be computed by the server (IntelliJ IDEA expands
	// a tab to its tab size); empty when every column can be computed.
	LineTemplate string
	// ColumnCaveat is CaveatColumnUnitAssumed when the editor's column unit
	// is assumed rather than verified: its links are then never exact.
	// Empty when the unit is verified.
	ColumnCaveat string
	// ShortNote is Note's caveat in two or three words ("macOS only"),
	// for a compact list row that shows the full Note on request; empty
	// for a verified preset.
	ShortNote string
}

// Link caveats: why a link that the server converted from current source
// is still not exact. A link with exact false and no caveat comes from a
// file that changed (or a position that could not be checked).
const (
	// CaveatTabSize marks a line-only link because a tab precedes the node
	// and the editor expands it to a tab size the server does not know.
	CaveatTabSize = "tab_size_unknown"
	// CaveatColumnUnitAssumed marks a link whose column unit is assumed
	// from another editor (Cursor's from VS Code).
	CaveatColumnUnitAssumed = "column_unit_assumed"
)

// Preset statuses: verified (documentation or source, all link parts),
// partly verified (some link parts assumed) and limited (verified, but the
// link works only on some platforms or for some products).
const (
	StatusVerified       = "verified"
	StatusPartlyVerified = "partly_verified"
	StatusLimited        = "limited"
)

// DefaultEditor is the preset used when graphvein.yaml names no editor.
const DefaultEditor = "vscode"

var presets = []Preset{
	{"vscode", "vscode://file{abs_url_path}:{line}:{column}", "utf16", 1, "Open in VS Code",
		StatusVerified, "Verified from VS Code's documentation and its URL handler.", "", "", ""},
	{"vscode-insiders", "vscode-insiders://file{abs_url_path}:{line}:{column}", "utf16", 1, "Open in VS Code Insiders",
		StatusVerified, "Verified from VS Code's handler, and a link was clicked on Windows.", "", "", ""},
	{"cursor", "cursor://file{abs_url_path}:{line}:{column}", "utf16", 1, "Open in Cursor",
		StatusPartlyVerified, "cursor://file is confirmed; line, column and the Windows path form are assumed from VS Code, so the column is marked as not exact.", "",
		CaveatColumnUnitAssumed, "column assumed"},
	{"vscodium", "vscodium://file{abs_url_path}:{line}:{column}", "utf16", 1, "Open in VSCodium",
		StatusVerified, "Verified from VSCodium's build script, which keeps VS Code's handler; not documented or clicked.", "", "", ""},
	{"jetbrains", "idea://open?file={abs_path}&line={line}&column={column}", "utf16", 0, "Open in IntelliJ IDEA",
		StatusLimited, "macOS only, IntelliJ IDEA only; where a tab precedes the node on its line or the file changed since indexing, the link opens the line only (the IDE's tab size is not known).",
		"idea://open?file={abs_path}&line={line}", "", "macOS only"},
	{"zed", "zed://file{abs_path}:{line}:{column}", "codepoint", 1, "Open in Zed",
		StatusVerified, "Verified from Zed's source, including Windows; not yet clicked on Windows.", "", "", ""},
}

// Presets lists the editor presets in documentation order. The slice is a
// copy, so callers cannot change the table.
func Presets() []Preset { return append([]Preset(nil), presets...) }

// PresetIDE returns the settings of the named preset alone, ignoring any
// repository configuration; the server uses it for the editor a browser
// chose (ADR 0050). ok is false for an unknown name.
func PresetIDE(name string) (ide IDE, ok bool) {
	p, ok := preset(name)
	if !ok {
		return IDE{}, false
	}
	return fromPreset(p), true
}

// LineTemplate returns the line-only template that belongs to linkTemplate,
// or "" when its editor's column can always be computed. It is keyed on
// the template, not the preset name, because the template names the URL
// handler: an explicit link_template equal to the jetbrains preset's opens
// IntelliJ IDEA just the same, and a column_unit or column_base override
// does not change how IntelliJ counts a tab (rule 14: no column the server
// cannot vouch for).
func LineTemplate(linkTemplate string) string {
	for _, p := range presets {
		if p.LinkTemplate == linkTemplate && p.LineTemplate != "" {
			return p.LineTemplate
		}
	}
	return ""
}

// ColumnCaveat returns the caveat of the editor that linkTemplate opens
// when its column unit is assumed, or "". Like LineTemplate it is keyed on
// the template: an explicit link_template equal to the cursor preset's
// opens Cursor just the same, and a column_unit override proves nothing.
func ColumnCaveat(linkTemplate string) string {
	for _, p := range presets {
		if p.LinkTemplate == linkTemplate && p.ColumnCaveat != "" {
			return p.ColumnCaveat
		}
	}
	return ""
}

// PresetNames lists the preset names in table order, for error messages.
func PresetNames() []string {
	names := make([]string, len(presets))
	for i, p := range presets {
		names[i] = p.Name
	}
	return names
}

func preset(name string) (Preset, bool) {
	for _, p := range presets {
		if p.Name == name {
			return p, true
		}
	}
	return Preset{}, false
}

func fromPreset(p Preset) IDE {
	return IDE{Editor: p.Name, Label: p.Label, LinkTemplate: p.LinkTemplate, ColumnUnit: p.ColumnUnit, ColumnBase: p.ColumnBase}
}

// DefaultIDE is the vscode preset: VS Code's UTF-16, one-based positions.
func DefaultIDE() IDE {
	p, _ := preset(DefaultEditor)
	return fromPreset(p)
}

func unknownEditor(name string) error {
	names := PresetNames()
	return fmt.Errorf("unknown ide editor %q (want %s or %s)", name, strings.Join(names[:len(names)-1], ", "), names[len(names)-1])
}

// decodeIDE accepts an editor name (`ide: cursor`) or a mapping. Each
// explicit key overrides the preset's value; an explicit link_template also
// drops the preset's label, because the template may open another editor.
func decodeIDE(n *yaml.Node) (IDE, error) {
	c := DefaultIDE()
	if n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		if n.Value == "" {
			return c, fmt.Errorf("ide editor must be a nonempty string")
		}
		p, ok := preset(n.Value)
		if !ok {
			return c, unknownEditor(n.Value)
		}
		return fromPreset(p), nil
	}
	if n.Kind != yaml.MappingNode {
		return c, fmt.Errorf("ide must be an editor name or a mapping")
	}
	seen := map[string]bool{}
	values := map[string]*yaml.Node{}
	for i := 0; i < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode || k.Tag != "!!str" || seen[k.Value] {
			return c, fmt.Errorf("invalid or duplicate ide key")
		}
		seen[k.Value] = true
		switch k.Value {
		case "editor", "label", "link_template", "column_unit":
			if v.Kind != yaml.ScalarNode || v.Tag != "!!str" {
				return c, fmt.Errorf("%s must be a string", k.Value)
			}
			if v.Value == "" {
				return c, fmt.Errorf("%s must be a nonempty string", k.Value)
			}
		case "column_base":
			if v.Kind != yaml.ScalarNode || v.Tag != "!!int" {
				return c, fmt.Errorf("column_base must be integer 0 or 1")
			}
		default:
			return c, fmt.Errorf("unknown ide key %s", k.Value)
		}
		values[k.Value] = v
	}
	if v, ok := values["editor"]; ok {
		p, ok := preset(v.Value)
		if !ok {
			return c, unknownEditor(v.Value)
		}
		c = fromPreset(p)
	}
	if v, ok := values["link_template"]; ok {
		c.LinkTemplate = v.Value
		c.Label = ""
	}
	if v, ok := values["column_unit"]; ok {
		c.ColumnUnit = v.Value
	}
	if v, ok := values["column_base"]; ok {
		if v.Decode(&c.ColumnBase) != nil {
			return c, fmt.Errorf("column_base must be integer 0 or 1")
		}
	}
	if v, ok := values["label"]; ok {
		if err := validLabel(v.Value); err != nil {
			return c, err
		}
		c.Label = v.Value
	}
	return c, c.validate()
}

// maxLabel bounds the label in characters so a button stays one short line.
const maxLabel = 60

func validLabel(s string) error {
	if utf8.RuneCountInString(s) > maxLabel {
		return fmt.Errorf("label must be at most %d characters", maxLabel)
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("label must not contain control characters")
		}
	}
	return nil
}

// validate rejects unsupported placeholders before any store is opened.
func (c IDE) validate() error {
	if c.ColumnUnit != "utf16" && c.ColumnUnit != "codepoint" && c.ColumnUnit != "byte" {
		return fmt.Errorf("column_unit must be utf16, codepoint or byte")
	}
	if c.ColumnBase != 0 && c.ColumnBase != 1 {
		return fmt.Errorf("column_base must be 0 or 1")
	}
	if !strings.Contains(c.LinkTemplate, "{abs_path}") && !strings.Contains(c.LinkTemplate, "{abs_url_path}") && !strings.Contains(c.LinkTemplate, "{rel_path}") {
		return fmt.Errorf("link_template needs a path placeholder")
	}
	for _, p := range regexp.MustCompile(`\{[^{}]*\}`).FindAllString(c.LinkTemplate, -1) {
		switch p {
		case "{abs_path}", "{abs_url_path}", "{rel_path}", "{line}", "{column}", "{end_line}", "{end_column}":
		default:
			return fmt.Errorf("unknown placeholder %s", p)
		}
	}
	return nil
}
