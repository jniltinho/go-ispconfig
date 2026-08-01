package mastertpl

import (
	"fmt"
	"regexp"
	"strings"
)

type node any

// textNode is literal template text.
type textNode string

// varNode prints a variable.
type varNode struct{ name string }

// condNode is a <tmpl_if> or <tmpl_unless> with an optional else branch.
type condNode struct {
	name     string
	op       string
	value    string
	format   string
	hasValue bool
	negate   bool // tmpl_unless
	then     []node
	els      []node
}

// loopNode iterates a dataset.
type loopNode struct {
	name string
	body []node
}

// tagRe is a port of the tag regex in tpl.inc.php (_getData), with the
// PCRE lookbehind for quoted attribute values rewritten as explicit
// quoted alternatives (Go RE2 has no lookbehind).
var tagRe = regexp.MustCompile(`(?i)(<!--/|<!--|</|<|\{/|\{)\s*tmpl_(\w+)((?:\s*(?:(?:name|format|escape|op|value|file)\s*=\s*)?(?:"[^"]*"|'[^']*'|[a-zA-Z0-9_.]*))*?)\s*(?:/?>|\}|-->)`)

// attrRe extracts key="value" pairs from a tag's attribute blob. A bare
// value without a key is the name, like the PHP per-attribute regex.
var attrRe = regexp.MustCompile(`(?i)(?:(name|format|escape|op|value|file)\s*=\s*)?(?:"([^"]*)"|'([^']*)'|([a-zA-Z0-9_.]+))`)

type attrs struct {
	name, op, format string
	value            string
	hasValue         bool
}

func parseAttrs(blob string) attrs {
	var a attrs
	for _, m := range attrRe.FindAllStringSubmatch(blob, -1) {
		key := strings.ToLower(m[1])
		if key == "" {
			key = "name"
		}
		val := m[2] + m[3] + m[4] // exactly one alternative matched
		switch key {
		case "name":
			a.name = val
		case "op":
			a.op = val
		case "value":
			a.value = val
			a.hasValue = true
		case "format":
			a.format = val
		}
	}
	return a
}

// frame is a parse-stack entry: the root, or an open cond/loop.
type frame struct {
	cond   *condNode
	loop   *loopNode
	inElse bool
	nodes  []node
}

func (f *frame) add(n node) { f.nodes = append(f.nodes, n) }

// eatNewline skips one newline at src[pos:], mirroring PHP swallowing a
// single newline right after a "?>" close tag.
func eatNewline(src string, pos int) int {
	if strings.HasPrefix(src[pos:], "\r\n") {
		return pos + 2
	}
	if strings.HasPrefix(src[pos:], "\n") {
		return pos + 1
	}
	return pos
}

func parse(src string) ([]node, error) {
	stack := []*frame{{}}
	top := func() *frame { return stack[len(stack)-1] }

	// The PHP engine evals "?>" + template, which swallows one leading
	// newline.
	pos := eatNewline(src, 0)

	for _, m := range tagRe.FindAllStringSubmatchIndex(src, -1) {
		if m[0] < pos {
			continue // tag swallowed by leading-newline skip (cannot really happen)
		}
		if text := src[pos:m[0]]; text != "" {
			top().add(textNode(text))
		}
		pos = m[1]
		prefix := src[m[2]:m[3]]
		tag := strings.ToLower(src[m[4]:m[5]])
		a := parseAttrs(src[m[6]:m[7]])
		closing := strings.Contains(prefix, "/")

		switch {
		case tag == "else":
			// PHP turns both <tmpl_else> and </tmpl_else> into "} else {".
			f := top()
			if f.cond == nil || f.inElse {
				return nil, fmt.Errorf("mastertpl: unexpected tmpl_else")
			}
			f.cond.then = f.nodes
			f.nodes = nil
			f.inElse = true
			pos = eatNewline(src, pos)

		case closing:
			f := top()
			var built node
			switch {
			case tag == "loop" && f.loop != nil:
				f.loop.body = f.nodes
				built = f.loop
			case (tag == "if" && f.cond != nil && !f.cond.negate) ||
				(tag == "unless" && f.cond != nil && f.cond.negate):
				if f.inElse {
					f.cond.els = f.nodes
				} else {
					f.cond.then = f.nodes
				}
				built = f.cond
			default:
				return nil, fmt.Errorf("mastertpl: unexpected closing tag tmpl_%s", tag)
			}
			stack = stack[:len(stack)-1]
			top().add(built)
			pos = eatNewline(src, pos)

		case tag == "var":
			if a.name == "" {
				return nil, fmt.Errorf("mastertpl: tmpl_var without name")
			}
			top().add(&varNode{name: a.name})
			// No newline swallowed: the PHP engine appends its own "\n"
			// after the compiled var block, which is what "?>" eats.

		case tag == "if" || tag == "unless":
			if a.name == "" {
				return nil, fmt.Errorf("mastertpl: tmpl_%s without name", tag)
			}
			stack = append(stack, &frame{cond: &condNode{
				name:     a.name,
				op:       a.op,
				value:    a.value,
				format:   a.format,
				hasValue: a.hasValue,
				negate:   tag == "unless",
			}})
			pos = eatNewline(src, pos)

		case tag == "loop":
			if a.name == "" {
				return nil, fmt.Errorf("mastertpl: tmpl_loop without name")
			}
			stack = append(stack, &frame{loop: &loopNode{name: a.name}})
			pos = eatNewline(src, pos)

		default:
			return nil, fmt.Errorf("mastertpl: unsupported tag tmpl_%s", tag)
		}
	}

	if len(stack) != 1 {
		return nil, fmt.Errorf("mastertpl: unclosed tmpl_if/tmpl_unless/tmpl_loop")
	}
	if text := src[pos:]; text != "" {
		top().add(textNode(text))
	}
	return top().nodes, nil
}
