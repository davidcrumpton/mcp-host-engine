package plugin

import (
	"regexp"
	"strings"
)

// DetectPluginName returns the plugin name exported by the JavaScript file at
// path, or "" when it cannot be determined statically. It never executes the
// file: digest output must be safe to produce on a machine that has not yet
// decided to trust the code.
//
// "Exported" is the operative word. The previous implementation took the first
// `name: "..."` anywhere in the file, so a nested object's `name` property (a
// tool parameter called `name`, a `name: {...}` input-schema field) could be
// reported as the plugin name. That mattered beyond cosmetics: the loader keys
// its directory index by this value and `mcphe digest -format yaml` /
// `-verify` key their output by it, so a wrong name means checking a different
// file than the one that gets loaded.
//
// Two export shapes are recognised, which is what the build produces:
//
//	const plugin = { name: "x", ... }; module.exports = plugin;
//	module.exports = { name: "x", ... };
func DetectPluginName(path string) string {
	data, err := readPluginFile(path)
	if err != nil {
		return ""
	}
	return detectPluginName(data)
}

// moduleExportsObjectPattern anchors on `module.exports = {`.
var moduleExportsObjectPattern = regexp.MustCompile(`module\s*\.\s*exports\s*=\s*\{`)

// moduleExportsIdentPattern anchors on `module.exports = someIdentifier`.
var moduleExportsIdentPattern = regexp.MustCompile(`module\s*\.\s*exports\s*=\s*([A-Za-z_$][A-Za-z0-9_$]*)`)

func detectPluginName(src []byte) string {
	// Shape 1: the object literal is the export.
	if loc := moduleExportsObjectPattern.FindIndex(src); loc != nil {
		// loc[1] is just past the "{", so the object's own properties are at
		// depth 1 counting from there.
		if name := objectPropertyAtDepth(src, loc[1]-1); name != "" {
			return name
		}
	}

	// Shape 2: `module.exports = plugin`, with `const plugin = {` above it.
	if m := moduleExportsIdentPattern.FindSubmatchIndex(src); m != nil {
		ident := string(src[m[2]:m[3]])
		start := declarationObjectStart(src, ident)
		if start >= 0 {
			if name := objectPropertyAtDepth(src, start); name != "" {
				return name
			}
		}
	}
	return ""
}

// declarationObjectStart finds the offset of the "{" in `<ident> = {`, the
// declaration a `module.exports = <ident>` refers to.
func declarationObjectStart(src []byte, ident string) int {
	pattern := regexp.MustCompile(`(?:^|[^\w$])` + regexp.QuoteMeta(ident) + `\s*=\s*\{`)
	if loc := pattern.FindIndex(src); loc != nil {
		return loc[1] - 1
	}
	return -1
}

// objectPropertyAtDepth returns the value of the first `name: "..."` property
// written directly in the object literal that starts at src[start] (which must
// be a "{"), or "".
//
// Only depth-1 properties count, and string and comment contents are skipped,
// so a nested object's `name` -- or the word "name" inside a description --
// cannot be mistaken for the plugin's own.
func objectPropertyAtDepth(src []byte, start int) string {
	if start < 0 || start >= len(src) || src[start] != '{' {
		return ""
	}
	depth := 0
	for i := start; i < len(src); {
		switch c := src[i]; {
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			i = skipLineComment(src, i)
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i = skipBlockComment(src, i)
		case c == '"' || c == '\'' || c == '`':
			_, next := readString(src, i)
			i = next
		case c == '{' || c == '[' || c == '(':
			depth++
			i++
		case c == '}' || c == ']' || c == ')':
			depth--
			if depth <= 0 {
				return "" // end of the exported object, no name found
			}
			i++
		case isIdentStart(c):
			word, next := readIdent(src, i)
			if depth == 1 && word == "name" {
				if value, ok := stringValueAfterColon(src, next); ok {
					return value
				}
			}
			i = next
		default:
			i++
		}
	}
	return ""
}

// stringValueAfterColon parses `: "value"` starting at i, skipping whitespace.
func stringValueAfterColon(src []byte, i int) (string, bool) {
	for i < len(src) && isSpace(src[i]) {
		i++
	}
	if i >= len(src) || src[i] != ':' {
		return "", false
	}
	i++
	for i < len(src) && isSpace(src[i]) {
		i++
	}
	if i >= len(src) || (src[i] != '"' && src[i] != '\'') {
		return "", false
	}
	value, _ := readString(src, i)
	return value, value != ""
}

// readString reads the quoted string starting at src[i] and returns its value
// and the offset just past the closing quote. An unterminated string consumes
// the rest of the input.
func readString(src []byte, i int) (string, int) {
	quote := src[i]
	var b strings.Builder
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			if j+1 < len(src) {
				b.WriteByte(src[j+1])
				j++
			}
		case quote:
			return b.String(), j + 1
		default:
			b.WriteByte(src[j])
		}
	}
	return b.String(), len(src)
}

func skipLineComment(src []byte, i int) int {
	for ; i < len(src); i++ {
		if src[i] == '\n' {
			return i + 1
		}
	}
	return len(src)
}

func skipBlockComment(src []byte, i int) int {
	for j := i + 2; j+1 < len(src); j++ {
		if src[j] == '*' && src[j+1] == '/' {
			return j + 2
		}
	}
	return len(src)
}

func readIdent(src []byte, i int) (string, int) {
	j := i
	for j < len(src) && (isIdentStart(src[j]) || (src[j] >= '0' && src[j] <= '9')) {
		j++
	}
	return string(src[i:j]), j
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}
