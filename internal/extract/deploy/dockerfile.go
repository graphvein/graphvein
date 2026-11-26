package deploy

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// instructions are the Dockerfile instructions BuildKit accepts. An
// instruction outside this list refuses the file: a file named like a
// Dockerfile that is not one (`Dockerfile.md`) states nothing.
var instructions = map[string]bool{
	"add": true, "arg": true, "cmd": true, "copy": true, "entrypoint": true, "env": true, "expose": true,
	"from": true, "healthcheck": true, "label": true, "maintainer": true, "onbuild": true, "run": true,
	"shell": true, "stopsignal": true, "user": true, "volume": true, "workdir": true,
}

// heredocMarker finds `<<WORD`, `<<-WORD`, `<<"WORD"` and `<<'WORD'` in
// RUN, COPY and ADD, the instructions BuildKit reads heredocs for.
var heredocMarker = regexp.MustCompile(`<<(-?)(["']?)([A-Za-z_][A-Za-z0-9_]*)(["']?)`)

// instruction is one logical Dockerfile line.
type instruction struct {
	line int
	word string // lower case
	rest string
}

// stage is one build stage of DC-F1.
type stage struct {
	line                int
	alias               string
	image, platform     string
	fromStage           string
	expose, interpolate []string
}

// readDockerfile reads one Dockerfile (row DC-F1): the `FROM` image of each
// stage and its `EXPOSE` ports, as written. It refuses a file with no
// FROM, an instruction outside BuildKit's list, a stage instruction before
// the first FROM, a malformed FROM, or an unterminated heredoc.
func readDockerfile(file string, source []byte) (contract.Node, error) {
	lines := sourceLines(source)
	list, err := logicalLines(lines)
	if err != nil {
		return contract.Node{}, err
	}
	var stages []*stage
	for _, in := range list {
		if !instructions[in.word] {
			return contract.Node{}, fmt.Errorf("line %d: unknown instruction %q", in.line, in.word)
		}
		switch in.word {
		case "from":
			s, err := fromStage(in, stages)
			if err != nil {
				return contract.Node{}, err
			}
			stages = append(stages, s)
		case "arg":
		default:
			if len(stages) == 0 {
				return contract.Node{}, fmt.Errorf("line %d: %s before the first FROM", in.line, strings.ToUpper(in.word))
			}
			if in.word == "expose" {
				s := stages[len(stages)-1]
				for _, port := range strings.Fields(in.rest) {
					s.expose = append(s.expose, port)
					if interpolated(port) {
						s.interpolate = append(s.interpolate, "expose")
					}
				}
			}
		}
	}
	if len(stages) == 0 {
		return contract.Node{}, errors.New("no FROM instruction")
	}
	tag("DC-F1")
	out := make([]any, 0, len(stages))
	for i, s := range stages {
		entry := map[string]any{"index": i, "line": s.line, "alias": nil, "platform": nil, "from_stage": nil, "expose": anyList(s.expose)}
		if s.alias != "" {
			entry["alias"] = s.alias
		}
		if s.platform != "" {
			entry["platform"] = s.platform
			if interpolated(s.platform) {
				s.interpolate = append(s.interpolate, "platform")
			}
		}
		if s.fromStage != "" {
			tag("DC-F1/stage")
			entry["from_stage"] = s.fromStage
			entry["image"], entry["image_name"], entry["image_tag"], entry["image_digest"] = s.image, nil, nil, nil
		} else {
			imageAttrs(entry, s.image, &s.interpolate)
		}
		if len(s.expose) > 0 {
			tag("DC-F1/expose")
		}
		entry["interpolated"] = anyList(distinctStrings(s.interpolate))
		out = append(out, entry)
	}
	identity, err := contract.Identity("dockerfile", file, path.Base(file))
	if err != nil {
		return contract.Node{}, errors.New("the path cannot form an identity")
	}
	span := contract.Span{StartLine: 1, EndLine: len(lines), EndColumn: len(lines[len(lines)-1])}
	return contract.Node{Type: "node", Identity: identity, Kind: "dockerfile", Level: 3, Name: file, QualifiedName: path.Base(file),
		File: file, Span: &span, ContentHash: contract.ContentHash(source), Attributes: map[string]any{"stages": out}}, nil
}

// fromStage parses `FROM [--flag=value ...] <image> [AS <name>]`. An image
// that names an earlier stage (case-insensitive, as BuildKit) is that
// stage, not an image.
func fromStage(in instruction, earlier []*stage) (*stage, error) {
	s := &stage{line: in.line}
	var words []string
	for _, w := range strings.Fields(in.rest) {
		if len(words) == 0 && strings.HasPrefix(w, "--") {
			if v, ok := strings.CutPrefix(w, "--platform="); ok {
				s.platform = v
			}
			continue
		}
		words = append(words, w)
	}
	switch {
	case len(words) == 1:
	case len(words) == 3 && strings.EqualFold(words[1], "as"):
		s.alias = words[2]
	default:
		return nil, fmt.Errorf("line %d: FROM is not `FROM <image> [AS <name>]`", in.line)
	}
	s.image = words[0]
	for _, e := range earlier {
		if e.alias != "" && strings.EqualFold(e.alias, s.image) {
			s.fromStage = e.alias
		}
	}
	return s, nil
}

// logicalLines joins continuation lines, drops comments, blank lines and
// parser directives, and skips heredoc bodies. The escape character is
// `\` unless a leading `# escape=` directive sets it.
func logicalLines(lines []string) ([]instruction, error) {
	escape := byte('\\')
	i := 0
	for ; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, "#") {
			break
		}
		key, value, ok := strings.Cut(strings.TrimSpace(t[1:]), "=")
		if !ok || strings.ContainsAny(strings.TrimSpace(key), " \t") {
			break
		}
		if strings.EqualFold(strings.TrimSpace(key), "escape") {
			switch v := strings.TrimSpace(value); v {
			case "`", "\\":
				escape = v[0]
			default:
				return nil, fmt.Errorf("line %d: invalid escape directive %q", i+1, v)
			}
		}
	}
	var out []instruction
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			i++
			continue
		}
		start := i + 1
		var text strings.Builder
		for {
			if i >= len(lines) {
				break
			}
			raw := lines[i]
			trimmed := strings.TrimSpace(raw)
			if text.Len() > 0 && (trimmed == "" || strings.HasPrefix(trimmed, "#")) {
				i++
				continue
			}
			right := strings.TrimRight(raw, " \t")
			i++
			if strings.HasSuffix(right, string(escape)) {
				text.WriteString(right[:len(right)-1])
				continue
			}
			text.WriteString(raw)
			break
		}
		logical := strings.TrimSpace(text.String())
		word, rest, _ := strings.Cut(logical, " ")
		if tab := strings.IndexByte(word, '\t'); tab >= 0 {
			word, rest = word[:tab], word[tab+1:]+" "+rest
		}
		in := instruction{line: start, word: strings.ToLower(word), rest: strings.TrimSpace(rest)}
		out = append(out, in)
		if in.word == "run" || in.word == "copy" || in.word == "add" {
			for _, m := range heredocMarker.FindAllStringSubmatch(in.rest, -1) {
				if m[2] != m[4] {
					continue
				}
				strip := m[1] == "-"
				for {
					if i >= len(lines) {
						return nil, fmt.Errorf("line %d: heredoc %s is not terminated", start, m[3])
					}
					body := lines[i]
					i++
					if strip {
						body = strings.TrimLeft(body, "\t")
					}
					if body == m[3] {
						break
					}
				}
			}
		}
	}
	return out, nil
}

// distinctStrings keeps the first occurrence of each string.
func distinctStrings(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
