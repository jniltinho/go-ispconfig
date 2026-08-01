package mastertpl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func render(t *testing.T, src string, vars map[string]any, loops map[string][]map[string]any) string {
	t.Helper()
	tpl := New(src)
	for k, v := range vars {
		tpl.SetVar(k, v)
	}
	for k, v := range loops {
		tpl.SetLoop(k, v)
	}
	out, err := tpl.Render()
	require.NoError(t, err)
	return out
}

func TestVar(t *testing.T) {
	vars := map[string]any{"a": "x", "n": 8080, "b": true, "f": false, "z": nil}
	tests := []struct{ name, src, want string }{
		{"double quotes", `<tmpl_var name="a">`, "x"},
		{"single quotes", `<tmpl_var name='a'>`, "x"},
		{"no quotes", `<tmpl_var name=a>`, "x"},
		{"bare name", `<tmpl_var a>`, "x"},
		{"curly delimiters", `{tmpl_var name='a'}`, "x"},
		{"comment delimiters", `<!-- tmpl_var name='a' -->`, "x"},
		{"case insensitive tag", `<TMPL_VAR NAME='a'>`, "x"},
		{"whitespace in tag", `<tmpl_var  name = "a" >`, "x"},
		{"self closing", `<tmpl_var name='a' />`, "x"},
		{"int", `<tmpl_var name='n'>`, "8080"},
		{"bool true prints 1", `<tmpl_var name='b'>`, "1"},
		{"bool false prints empty", `<tmpl_var name='f'>`, ""},
		{"unset prints nothing", `[<tmpl_var name='missing'>]`, "[]"},
		{"nil prints nothing", `[<tmpl_var name='z'>]`, "[]"},
		{"newline after var is kept", "<tmpl_var name='a'>\nrest", "x\nrest"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, render(t, tc.src, vars, nil))
		})
	}
}

func TestIfTruthy(t *testing.T) {
	src := `<tmpl_if name='v'>T<tmpl_else>F</tmpl_if>`
	tests := []struct {
		name string
		v    any
		want string
	}{
		{"nil", nil, "F"},
		{"false", false, "F"},
		{"empty string", "", "F"},
		{"zero string", "0", "F"},
		{"zero int", 0, "F"},
		{"true", true, "T"},
		{"y", "y", "T"},
		{"nonzero", 1, "T"},
		{"0.0 string is truthy in PHP", "0.0", "T"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, render(t, src, map[string]any{"v": tc.v}, nil))
		})
	}
	t.Run("unset var", func(t *testing.T) {
		require.Equal(t, "F", render(t, src, nil, nil))
	})
}

func TestIfOpValue(t *testing.T) {
	tests := []struct {
		name string
		src  string
		vars map[string]any
		want string
	}{
		{"eq string true", `<tmpl_if name='pm' op='==' value='dynamic'>y</tmpl_if>`, map[string]any{"pm": "dynamic"}, "y"},
		{"eq string false", `<tmpl_if name='pm' op='==' value='dynamic'>y</tmpl_if>`, map[string]any{"pm": "ondemand"}, ""},
		{"eq without op defaults to ==", `<tmpl_if name='pm' value='static'>y</tmpl_if>`, map[string]any{"pm": "static"}, "y"},
		{"ne true when unset", `<tmpl_if name='use_proxy' op='!=' value='y'>y</tmpl_if>`, nil, "y"},
		{"ne diamond op", `<tmpl_if name='a' op='<>' value='b'>y</tmpl_if>`, map[string]any{"a": "b"}, ""},
		{"numeric eq across types", `<tmpl_if name='security_level' op='==' value='20'>y</tmpl_if>`, map[string]any{"security_level": "20"}, "y"},
		{"numeric gt string var", `<tmpl_if name='p' op='>' value='0'>y</tmpl_if>`, map[string]any{"p": "8080"}, "y"},
		{"numeric gt zero var", `<tmpl_if name='p' op='>' value='0'>y</tmpl_if>`, map[string]any{"p": 0}, ""},
		{"numeric gt unset var", `<tmpl_if name='p' op='>' value='0'>y</tmpl_if>`, nil, ""},
		{"numeric gt empty string var", `<tmpl_if name='p' op='>' value='0'>y</tmpl_if>`, map[string]any{"p": ""}, ""},
		{"lte", `<tmpl_if name='p' op='<=' value='10'>y</tmpl_if>`, map[string]any{"p": 10}, "y"},
		{"gte", `<tmpl_if name='p' op='>=' value='10'>y</tmpl_if>`, map[string]any{"p": 9}, ""},
		{"version gt true", `<tmpl_if name='v' op='>' value='1.25.0' format='version'>y</tmpl_if>`, map[string]any{"v": "1.26.1"}, "y"},
		{"version gt false", `<tmpl_if name='v' op='>' value='1.25.0' format='version'>y</tmpl_if>`, map[string]any{"v": "1.22.1"}, ""},
		{"version shorter is lower", `<tmpl_if name='v' op='<' value='1.25.0' format='version'>y</tmpl_if>`, map[string]any{"v": "1.25"}, "y"},
		{"version unset", `<tmpl_if name='v' op='>' value='1.25.0' format='version'>y</tmpl_if>`, nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, render(t, tc.src, tc.vars, nil))
		})
	}
}

func TestUnlessAndElse(t *testing.T) {
	require.Equal(t, "no ssl", render(t, `<tmpl_unless name='ssl'>no ssl</tmpl_unless>`, nil, nil))
	require.Equal(t, "", render(t, `<tmpl_unless name='ssl'>no ssl</tmpl_unless>`, map[string]any{"ssl": "y"}, nil))
	// nginx_vhost.conf.master closes else branches with </tmpl_else>,
	// which the PHP engine also compiles to "} else {".
	src := `<tmpl_if name='php' op='==' value='php-fpm'>fpm</tmpl_else><tmpl_if name='php' op='==' value='hhvm'>hhvm</tmpl_else>none</tmpl_if></tmpl_if>`
	require.Equal(t, "fpm", render(t, src, map[string]any{"php": "php-fpm"}, nil))
	require.Equal(t, "hhvm", render(t, src, map[string]any{"php": "hhvm"}, nil))
	require.Equal(t, "none", render(t, src, map[string]any{"php": "no"}, nil))
}

func TestLoop(t *testing.T) {
	t.Run("iteration and row precedence over globals", func(t *testing.T) {
		src := `<tmpl_loop name='rr'><tmpl_var name='name'> <tmpl_var name='ttl'>;</tmpl_loop>`
		out := render(t, src,
			map[string]any{"ttl": 3600, "name": "global"},
			map[string][]map[string]any{"rr": {
				{"name": "www", "ttl": 60},
				{"name": "mail"},           // missing ttl falls back to global
				{"name": "ftp", "ttl": ""}, // empty string does NOT fall back
			}})
		require.Equal(t, "www 60;mail 3600;ftp ;", out)
	})
	t.Run("empty and unset loops render nothing", func(t *testing.T) {
		src := `<tmpl_loop name='rr'>x</tmpl_loop>`
		require.Equal(t, "", render(t, src, nil, nil))
		require.Equal(t, "", render(t, src, nil, map[string][]map[string]any{"rr": {}}))
	})
	t.Run("set_loop_var makes loop name truthy", func(t *testing.T) {
		src := `<tmpl_if name='rr'>has rows</tmpl_if>`
		require.Equal(t, "has rows", render(t, src, nil, map[string][]map[string]any{"rr": {{"a": 1}}}))
		require.Equal(t, "", render(t, src, nil, map[string][]map[string]any{"rr": {}}))
	})
	t.Run("nested loop reads dataset from current row", func(t *testing.T) {
		src := `<tmpl_loop name='redirects'>[<tmpl_loop name='proxy_directives'><tmpl_var name='proxy_directive'>;</tmpl_loop>]</tmpl_loop>`
		out := render(t, src, nil, map[string][]map[string]any{"redirects": {
			{"proxy_directives": []map[string]any{{"proxy_directive": "a"}, {"proxy_directive": "b"}}},
			{}, // no nested dataset -> zero iterations
		}})
		require.Equal(t, "[a;b;][]", out)
	})
	t.Run("row dataset is truthy in tmpl_if", func(t *testing.T) {
		src := `<tmpl_loop name='r'><tmpl_if name='sub'>y<tmpl_else>n</tmpl_if></tmpl_loop>`
		out := render(t, src, nil, map[string][]map[string]any{"r": {
			{"sub": []map[string]any{{"a": 1}}},
			{"sub": []map[string]any{}},
			{},
		}})
		require.Equal(t, "ynn", out)
	})
}

func TestNewlineSwallowing(t *testing.T) {
	// Block tags compile to "<?php ... ?>" in PHP, which eats one
	// following newline; a leading template newline is eaten by the
	// "?>" prepended at eval time.
	src := "\nA\n<tmpl_if name='x'>\nB\n</tmpl_if>\nC\n"
	require.Equal(t, "A\nB\nC\n", render(t, src, map[string]any{"x": "y"}, nil))
	require.Equal(t, "A\nC\n", render(t, src, nil, nil))
	// CRLF is eaten as one newline too.
	require.Equal(t, "B\r\n", render(t, "<tmpl_if name='x'>\r\nB\r\n</tmpl_if>\r\n", map[string]any{"x": "y"}, nil))
	// Inline tags followed by non-newline text swallow nothing.
	require.Equal(t, "a b;", render(t, "a<tmpl_if name='x'> b</tmpl_if>;", map[string]any{"x": "y"}, nil))
}

func TestParseErrors(t *testing.T) {
	for name, src := range map[string]string{
		"unclosed if":      `<tmpl_if name='x'>a`,
		"unclosed loop":    `<tmpl_loop name='x'>a`,
		"stray close":      `a</tmpl_if>`,
		"mismatched close": `<tmpl_if name='x'>a</tmpl_loop>`,
		"else outside if":  `<tmpl_else>`,
		"unsupported tag":  `<tmpl_include name='x'>`,
		"var without name": `<tmpl_var>`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := New(src).Render()
			require.Error(t, err)
		})
	}
}

func TestNonTagBracesLeftAlone(t *testing.T) {
	src := "location ~ \"[a-z]{2}\" {\n    root /x;\n}\n"
	require.Equal(t, src, render(t, src, nil, nil))
}
