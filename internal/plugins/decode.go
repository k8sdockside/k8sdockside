package plugins

import (
	"bytes"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Reading a plugin file strictly, and saying where it went wrong.
//
// A member the manifest has no field for is refused rather than ignored. A
// misspelt "reqiures" read leniently is a plugin that loads and then quietly
// checks nothing, which is the hardest kind of mistake to find; refused, it is
// one line naming the word and the one it was probably meant to be. The cost
// is that a plugin written for a newer app than this one would be refused
// over a field it cannot know -- which is what minAppVersion is for, and why
// that is checked first, from a lenient read.

// decodeStrict reads one plugin, refusing unknown members.
func decodeStrict(raw []byte, into any) error {
	return json.Unmarshal(raw, into, json.RejectUnknownMembers(true))
}

// asked is what is read of a plugin before anything else: enough to know
// whether this app is new enough to read the rest of it.
type asked struct {
	ID            string `json:"id"`
	MinAppVersion string `json:"minAppVersion"`
}

// describe words a decoding error for someone editing the file: where it is
// and what is wrong, in the file's own terms rather than Go's.
//
// raw is the value that was decoded. When it is the whole file, lines is true
// and a position is given as a line and column; for a plugin inside a pack the
// offsets are the plugin's own, so only the path is given, under at.
func describe(raw []byte, err error, at jsontext.Pointer, lines bool) error {
	var syntax *jsontext.SyntacticError
	if errors.As(err, &syntax) {
		what := "not valid JSON"
		if syntax.Err != nil {
			what = syntax.Err.Error()
		}
		// The mistake a hand-edited manifest makes most, and one JSON's own
		// wording does not name.
		if strings.HasPrefix(what, "invalid character ',' at start of value") {
			what += " -- JSON allows no comma after the last item in a list or object"
		}
		return fmt.Errorf("not valid JSON%s: %s", where(raw, syntax.ByteOffset, at+syntax.JSONPointer, lines), what)
	}

	var semantic *json.SemanticError
	if !errors.As(err, &semantic) {
		return fmt.Errorf("could not be read: %w", err)
	}
	ptr := at + semantic.JSONPointer

	if errors.Is(semantic.Err, json.ErrUnknownName) {
		name := ptr.LastToken()
		msg := fmt.Sprintf("unknown field %q%s", name, where(raw, semantic.ByteOffset, ptr.Parent(), lines))
		if near := nearest(name, memberNames(semantic.GoType)); near != "" {
			msg += fmt.Sprintf(" -- did you mean %q?", near)
		} else {
			msg += " -- not a field this version of the app knows"
		}
		return errors.New(msg)
	}

	if semantic.GoType != nil && semantic.JSONKind != 0 {
		return fmt.Errorf("%s%s should be %s, not %s",
			fieldName(ptr), where(raw, semantic.ByteOffset, "", lines), expected(semantic.GoType), jsonKind(semantic.JSONKind))
	}
	return fmt.Errorf("could not be read%s: %w", where(raw, semantic.ByteOffset, ptr, lines), err)
}

// where is " at views[2] (line 14, column 9)", or as much of it as is known.
func where(raw []byte, offset int64, ptr jsontext.Pointer, lines bool) string {
	var parts []string
	if path := readablePath(ptr); path != "" {
		parts = append(parts, "in "+path)
	}
	if lines && offset >= 0 && int(offset) <= len(raw) {
		line, col := position(raw, int(offset))
		parts = append(parts, fmt.Sprintf("(line %d, column %d)", line, col))
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

// fieldName is how a failing member is named at the start of a sentence.
func fieldName(ptr jsontext.Pointer) string {
	if path := readablePath(ptr); path != "" {
		return path
	}
	return "the file"
}

// readablePath turns "/views/2/kind" into "views[2].kind", which is how the
// member is found by eye.
func readablePath(ptr jsontext.Pointer) string {
	var b strings.Builder
	for tok := range ptr.Tokens() {
		if _, err := strconv.Atoi(tok); err == nil {
			b.WriteString("[" + tok + "]")
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(tok)
	}
	return b.String()
}

// position is the one-based line and column of a byte offset.
func position(raw []byte, offset int) (line, col int) {
	before := raw[:offset]
	line = bytes.Count(before, []byte{'\n'}) + 1
	start := bytes.LastIndexByte(before, '\n') + 1
	return line, utf8.RuneCount(before[start:]) + 1
}

// expected is what a Go type is called in a manifest.
func expected(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeFor[ServicePort]() {
		return "a port name or number"
	}
	switch t.Kind() {
	case reflect.String:
		return "a string"
	case reflect.Bool:
		return "true or false"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Slice, reflect.Array:
		return "a list"
	case reflect.Map, reflect.Struct:
		return "an object"
	}
	return "something else"
}

// jsonKind is what a JSON value is called in the same sentence.
func jsonKind(k jsontext.Kind) string {
	switch k {
	case '"':
		return "a string"
	case '0':
		return "a number"
	case 't', 'f':
		return "true or false"
	case 'n':
		return "null"
	case '{':
		return "an object"
	case '[':
		return "a list"
	}
	return "that"
}

// memberNames are the members a struct type is read from, which is what an
// unknown one is compared against for a suggestion.
func memberNames(t reflect.Type) []string {
	for t != nil && (t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice) {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for field := range t.Fields() {
		if !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		names = append(names, name)
	}
	return names
}

// nearest is the known name an unknown one was most likely meant to be: the
// closest by edit distance, ignoring case, if it is close enough to be a typo
// rather than a different word.
func nearest(name string, known []string) string {
	best, bestDist := "", 0
	for _, candidate := range known {
		d := distance(strings.ToLower(name), strings.ToLower(candidate))
		if best == "" || d < bestDist {
			best, bestDist = candidate, d
		}
	}
	limit := 2
	if len(name) > 8 {
		limit = 3
	}
	if best == "" || bestDist > limit {
		return ""
	}
	return best
}

// distance is the Levenshtein distance between two short strings.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
