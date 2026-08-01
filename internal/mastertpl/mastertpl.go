// Package mastertpl renders ISPConfig ".master" configuration templates.
//
// It is a port of the subset of ISPConfig's vlibTemplate engine
// (server/lib/classes/tpl.inc.php) that the bundled nginx and bind
// templates use:
//
//   - <tmpl_var name="x"> — prints the variable if it is set and non-nil
//     (attribute quoting with ", ' or none; a bare attribute without the
//     name= key is treated as the name).
//   - <tmpl_if name="x"> — PHP truthy test: nil, false, "", "0" and
//     numeric zero are false; empty loop datasets are false too.
//   - <tmpl_if name="x" op="==|!=|<>|>|<|>=|<=" value="y"> — PHP 8 loose
//     comparison: numeric when value and variable are both numeric,
//     string comparison otherwise. format="version" compares
//     dot-separated numeric versions like PHP version_compare.
//   - <tmpl_unless ...> — negated <tmpl_if>.
//   - <tmpl_else> — both the <tmpl_else> and </tmpl_else> spellings, as
//     accepted by the PHP engine.
//   - <tmpl_loop name="items"> — iterates a dataset. Inside a loop, row
//     values take precedence over global variables; a missing or nil row
//     value falls back to the globals (GLOBAL_VARS=1 in vlibIni). Nested
//     loops read their dataset from the current row of the enclosing
//     loop ([]map[string]any value under the loop name).
//   - Closing tags </tmpl_if>, </tmpl_unless>, </tmpl_loop>.
//
// Tag names are case-insensitive and every vlibTemplate delimiter pair
// is accepted: <...>, </...>, {...}, {/...}, <!-- ... --> and
// <!--/ ... -->. Like the PHP engine (which compiles tags to
// "<?php ... ?>" blocks), one newline immediately following a
// tmpl_if/tmpl_unless/tmpl_else/tmpl_loop tag or a closing tag is
// swallowed, as is one newline at the very start of the template;
// newlines after <tmpl_var> are preserved.
//
// Deliberately not ported because the bundled templates do not use it:
// tmpl_include/tmpl_dyninclude/tmpl_hook/tmpl_comment/tmpl_elseif tags,
// escape=/format= output filters on tmpl_var, the global./loop. name
// prefixes, loop context vars (__FIRST__ etc.), and the PHP quirk where
// an explicitly set empty top-level loop renders its body once with
// global fallback (here it renders zero times). Unsupported tags are a
// render error rather than being silently dropped.
package mastertpl

import (
	"cmp"
	"embed"
	"fmt"
	"strconv"
	"strings"
)

// Templates holds the ".master" templates copied verbatim from
// ISPConfig 3 (server/conf/). Use Templates.ReadFile("templates/<name>")
// to load one.
//
//go:embed templates
var Templates embed.FS

// Template is a parsed-on-demand ".master" template plus the variables
// and loop datasets to render it with.
type Template struct {
	src      string
	vars     map[string]any
	loops    map[string][]map[string]any
	tree     []node
	parsed   bool
	parseErr error
}

// New returns a Template for the given template source.
func New(src string) *Template {
	return &Template{
		src:   src,
		vars:  map[string]any{},
		loops: map[string][]map[string]any{},
	}
}

// SetVar sets a global template variable. A nil value behaves like an
// unset variable, mirroring the PHP engine's "!== null" checks.
func (t *Template) SetVar(name string, value any) {
	t.vars[name] = value
}

// SetLoop sets the dataset for a top-level <tmpl_loop>. Like the PHP
// engine with SET_LOOP_VAR=1, a non-empty dataset also sets a truthy
// global variable under the loop name.
func (t *Template) SetLoop(name string, rows []map[string]any) {
	t.loops[name] = rows
	if len(rows) > 0 {
		t.vars[name] = 1
	}
}

// Render parses the template (once) and renders it with the variables
// and loops set so far.
func (t *Template) Render() (string, error) {
	if !t.parsed {
		t.tree, t.parseErr = parse(t.src)
		t.parsed = true
	}
	if t.parseErr != nil {
		return "", t.parseErr
	}
	var b strings.Builder
	t.renderNodes(&b, t.tree, nil)
	return b.String(), nil
}

func (t *Template) renderNodes(b *strings.Builder, nodes []node, row map[string]any) {
	for _, n := range nodes {
		switch n := n.(type) {
		case textNode:
			b.WriteString(string(n))
		case *varNode:
			if v := t.lookup(n.name, row); v != nil {
				b.WriteString(phpString(v))
			}
		case *condNode:
			branch := n.then
			if t.evalCond(n, row) != !n.negate { // condition (xor negate) is false
				branch = n.els
			}
			t.renderNodes(b, branch, row)
		case *loopNode:
			var rows []map[string]any
			if row == nil {
				rows = t.loops[n.name]
			} else {
				// Nested loops read their dataset from the current row,
				// exactly like the compiled PHP:
				// $this->_arrvars['outer'][$_0]['inner'].
				rows, _ = row[n.name].([]map[string]any)
			}
			for _, r := range rows {
				t.renderNodes(b, n.body, r)
			}
		}
	}
}

// lookup resolves a variable: current loop row first (when the value is
// set and non-nil), then the globals. Only the innermost row is
// consulted, matching the compiled PHP lookup.
func (t *Template) lookup(name string, row map[string]any) any {
	if row != nil {
		if v, ok := row[name]; ok && v != nil {
			return v
		}
	}
	return t.vars[name]
}

func (t *Template) evalCond(n *condNode, row map[string]any) bool {
	v := t.lookup(n.name, row)
	if !n.hasValue {
		return truthy(v)
	}
	op := n.op
	switch op {
	case "":
		op = "=="
	case "<>":
		op = "!="
	}
	return applyOp(compareValues(v, n.value, n.format), op)
}

// compareValues returns -1/0/1 for the template variable v versus the
// literal attribute value, following PHP 8 loose comparison for the
// cases the templates exercise.
func compareValues(v any, value, format string) int {
	if format == "version" {
		return versionCompare(phpString(v), value)
	}
	if valF, err := strconv.ParseFloat(value, 64); err == nil {
		// PHP leaves numeric values unquoted: numeric comparison when
		// the variable is numeric too, string comparison otherwise.
		if vF, ok := toFloat(v); ok {
			return cmp.Compare(vF, valF)
		}
	}
	return strings.Compare(phpString(v), value)
}

func applyOp(c int, op string) bool {
	switch op {
	case "==":
		return c == 0
	case "!=":
		return c != 0
	case ">":
		return c > 0
	case "<":
		return c < 0
	case ">=":
		return c >= 0
	case "<=":
		return c <= 0
	}
	return false
}

// versionCompare compares dot-separated numeric versions ("1.25.0")
// like PHP version_compare does for them: part by part numerically,
// with a shorter version comparing lower when the shared parts match.
func versionCompare(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, _ := strconv.Atoi(pa[i])
		nb, _ := strconv.Atoi(pb[i])
		if na != nb {
			return cmp.Compare(na, nb)
		}
	}
	return cmp.Compare(len(pa), len(pb))
}

// truthy implements PHP truthiness for the value types the templates
// see: nil, false, "", "0", numeric zero and empty datasets are false.
func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != "" && v != "0"
	case []map[string]any:
		return len(v) > 0
	default:
		if f, ok := toFloat(v); ok {
			return f != 0
		}
		return true
	}
}

// toFloat reports whether v is numeric in the PHP loose-comparison
// sense (numbers, booleans, nil and numeric strings) and its value.
func toFloat(v any) (float64, bool) {
	switch v := v.(type) {
	case nil:
		return 0, true
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	}
	return 0, false
}

// phpString converts a value to output text like PHP print does.
func phpString(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "1"
		}
		return ""
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	default:
		return fmt.Sprint(v)
	}
}
