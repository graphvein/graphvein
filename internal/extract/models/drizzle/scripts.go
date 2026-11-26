package drizzle

import (
	"strings"
)

// drizzle-kit picks its options in assertCollisions (bin.cjs:10716-10731):
// with no option but --config (and the command's whitelist) it loads a
// config file, --config <path> or the default drizzle.config.ts, .js, .json
// in the working directory (drizzleConfigFromFile, bin.cjs:15838-15870);
// with options and no --config it uses the options ("cli"); with both it
// exits. The package's own scripts show how drizzle-kit is run there
// (DR-V5), so discovery reads them.

// kitInvocation is one drizzle-kit command of a package script.
type kitInvocation struct {
	Script string
	// Config is the literal --config value; "" when none is given.
	Config string
	// Flags are the literal --schema, --dialect and --casing values of a
	// command without --config (drizzle-kit's "cli" mode).
	Flags map[string]string
	// Unproven says why the command's configuration cannot be proven (a
	// changed working directory, a non-literal value, --config together
	// with other options); "" when it can.
	Unproven string
}

// cliOptions are the drizzle-kit options that select "cli" mode; the
// first three are read, the others make a flag configuration unproven
// unless they are only output or connection settings.
var cliOptions = map[string]bool{"schema": true, "dialect": true, "casing": true, "out": true, "driver": true, "url": true, "breakpoints": true}

// readFlags are the cli options a flag configuration reads.
var readFlags = []string{"schema", "dialect", "casing"}

// token is one shell word; Literal is false when it holds an expansion
// ($, backquote) outside single quotes.
type token struct {
	Text    string
	Literal bool
}

// shellCommands splits a script into commands at `&&`, `||`, `;`, `|`,
// `&` and newlines outside quotes, and each command into words with quotes
// removed. It is a reader for package scripts, not a shell.
func shellCommands(script string) [][]token {
	var cmds [][]token
	var cur []token
	var word strings.Builder
	inWord, literal := false, true
	var quote byte
	endWord := func() {
		if inWord {
			cur = append(cur, token{Text: word.String(), Literal: literal})
		}
		word.Reset()
		inWord, literal = false, true
	}
	endCmd := func() {
		endWord()
		if len(cur) > 0 {
			cmds = append(cmds, cur)
		}
		cur = nil
	}
	for i := 0; i < len(script); i++ {
		c := script[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
				continue
			}
			if quote == '"' && (c == '$' || c == '`') {
				literal = false
			}
			word.WriteByte(c)
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == ' ' || c == '\t':
			endWord()
		case c == '\n' || c == '\r' || c == ';' || c == '|' || c == '&':
			endCmd()
		default:
			if c == '$' || c == '`' || c == '\\' {
				literal = false
			}
			word.WriteByte(c)
			inWord = true
		}
	}
	endCmd()
	return cmds
}

// isKit reports a word that runs drizzle-kit: the bare name, a path to
// its binary, or a versioned npx/dlx name.
func isKit(w string) bool {
	return w == "drizzle-kit" || strings.HasSuffix(w, "/drizzle-kit") || strings.HasPrefix(w, "drizzle-kit@")
}

// cwdFlags are runner options before drizzle-kit that change the working
// directory (pnpm -C/--dir/--filter, npm --prefix, yarn --cwd).
var cwdFlags = map[string]bool{"-C": true, "--dir": true, "--prefix": true, "--cwd": true, "--filter": true, "-F": true, "--workspace": true, "-w": true}

// kitInvocations returns the drizzle-kit commands of a package's scripts,
// in script-name order.
func kitInvocations(scripts map[string]string, names []string) []kitInvocation {
	var out []kitInvocation
	for _, name := range names {
		movedDir := false
		for _, cmd := range shellCommands(scripts[name]) {
			if cmd[0].Text == "cd" || cmd[0].Text == "pushd" {
				movedDir = true
				continue
			}
			at := -1
			for i, w := range cmd {
				if isKit(w.Text) {
					at = i
					break
				}
			}
			if at < 0 {
				continue
			}
			inv := kitInvocation{Script: name, Flags: map[string]string{}}
			for _, w := range cmd[:at] {
				if cwdFlags[w.Text] || strings.HasPrefix(w.Text, "--dir=") || strings.HasPrefix(w.Text, "--prefix=") || strings.HasPrefix(w.Text, "--cwd=") || strings.HasPrefix(w.Text, "--filter=") {
					movedDir = true
				}
			}
			if movedDir {
				inv.Unproven = "script " + quote(name) + " runs drizzle-kit in another working directory"
			}
			hasConfig, cli := false, false
			args := cmd[at+1:]
			for i := 0; i < len(args); i++ {
				w := args[i]
				if !strings.HasPrefix(w.Text, "--") {
					continue
				}
				key, value, inline := strings.Cut(strings.TrimPrefix(w.Text, "--"), "=")
				if key != "config" && !cliOptions[key] {
					continue
				}
				literal := w.Literal
				if !inline {
					if i+1 >= len(args) || strings.HasPrefix(args[i+1].Text, "--") {
						if key == "config" || key == "schema" || key == "dialect" || key == "casing" {
							literal = false
						}
					} else {
						i++
						value, literal = args[i].Text, args[i].Literal
					}
				}
				if key == "config" {
					hasConfig = true
					inv.Config = value
				} else {
					cli = true
				}
				for _, f := range readFlags {
					if key == f {
						inv.Flags[f] = value
					}
				}
				if !literal && inv.Unproven == "" {
					inv.Unproven = "script " + quote(name) + " passes a non-literal --" + key
				}
			}
			if hasConfig && cli && inv.Unproven == "" {
				inv.Unproven = "script " + quote(name) + " passes --config with other options, which drizzle-kit refuses (bin.cjs:10729)"
			}
			if !cli {
				inv.Flags = nil
			}
			out = append(out, inv)
		}
	}
	return out
}
