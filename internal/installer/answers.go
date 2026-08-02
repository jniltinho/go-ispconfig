package installer

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/viper"
)

// Answers holds every installer question, resolved (design D3) in the
// order: CLI flag > answers file (--answers file.toml) > interactive
// prompt > default. With --yes the prompt layer is skipped and a missing
// required answer aborts naming its flag.
type Answers struct {
	Hostname       string
	PanelPort      int
	DBName         string
	DBUser         string
	DBRootPassword string
	EnableWeb      bool
	EnableDNS      bool
	InstallPHPFPM  bool
	InstallAcme    bool
	// AcmeClient is "acme.sh" (default) or "certbot".
	AcmeClient string
	AdminEmail string
}

// answerField describes one question: its flag/file key, prompt text,
// default and whether an empty final value aborts the install.
type answerField struct {
	key      string // flag name; answers-file key is the same with _ for -
	prompt   string
	def      func() string
	required bool
	boolean  bool
}

// hostnameDefault is overridable in tests.
var hostnameDefault = func() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

func answerFields() []answerField {
	s := func(v string) func() string { return func() string { return v } }
	return []answerField{
		{key: "hostname", prompt: "Full qualified hostname (FQDN) of the server", def: hostnameDefault, required: true},
		{key: "panel-port", prompt: "Panel port", def: s("8080"), required: true},
		{key: "db-name", prompt: "ISPConfig database name", def: s("dbispconfig"), required: true},
		{key: "db-user", prompt: "ISPConfig database user", def: s("ispconfig"), required: true},
		// Not prompted: unix-socket auth is the default (design D5); the
		// flag/answers file covers hosts where socket auth is disabled.
		{key: "db-root-password", def: s("")},
		{key: "web", prompt: "Configure web server (nginx)? (y/n)", def: s("y"), boolean: true},
		{key: "dns", prompt: "Configure DNS server (bind)? (y/n)", def: s("y"), boolean: true},
		{key: "php-fpm", prompt: "Install PHP-FPM for hosted sites? (y/n)", def: s("y"), boolean: true},
		{key: "acme", prompt: "Install acme.sh for site Let's Encrypt certificates? (y/n)", def: s("n"), boolean: true},
		{key: "acme-client", prompt: "ACME client (acme.sh/certbot)", def: s("acme.sh")},
		{key: "admin-email", prompt: "Admin email address", def: s("")},
	}
}

// ResolveOptions feeds ResolveAnswers with the flag layer (only flags the
// user explicitly set), the answers file path and the prompt streams.
type ResolveOptions struct {
	// Flags maps flag name -> value for flags explicitly set on the CLI.
	Flags map[string]string
	// AnswersFile is the --answers TOML path ("" = none).
	AnswersFile string
	// Interactive enables the prompt layer (false with --yes).
	Interactive bool
	In          io.Reader
	Out         io.Writer
}

// ResolveAnswers resolves every answer through the precedence chain and
// validates required values (port of the install.php question loop built on
// simple_query/free_query).
func ResolveAnswers(opts ResolveOptions) (*Answers, error) {
	var file *viper.Viper
	if opts.AnswersFile != "" {
		file = viper.New()
		file.SetConfigFile(opts.AnswersFile)
		file.SetConfigType("toml")
		if err := file.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("reading answers file: %w", err)
		}
	}
	if opts.In == nil {
		opts.In = os.Stdin
	}
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	reader := bufio.NewReader(opts.In)

	values := map[string]string{}
	for _, f := range answerFields() {
		fileKey := strings.ReplaceAll(f.key, "-", "_")
		switch {
		case opts.Flags[f.key] != "":
			values[f.key] = opts.Flags[f.key]
		case file != nil && file.IsSet(fileKey):
			values[f.key] = file.GetString(fileKey)
		case opts.Interactive && f.prompt != "":
			v, err := promptAnswer(reader, opts.Out, f.prompt, f.def())
			if err != nil {
				return nil, err
			}
			values[f.key] = v
		default:
			values[f.key] = f.def()
		}
		if f.required && values[f.key] == "" {
			return nil, fmt.Errorf("missing required answer --%s (pass the flag or set %s in the answers file)", f.key, fileKey)
		}
	}

	a := &Answers{
		Hostname:       values["hostname"],
		DBName:         values["db-name"],
		DBUser:         values["db-user"],
		DBRootPassword: values["db-root-password"],
		AcmeClient:     values["acme-client"],
		AdminEmail:     values["admin-email"],
	}
	port, err := strconv.Atoi(values["panel-port"])
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid --panel-port %q: must be 1-65535", values["panel-port"])
	}
	a.PanelPort = port
	for key, dst := range map[string]*bool{
		"web": &a.EnableWeb, "dns": &a.EnableDNS,
		"php-fpm": &a.InstallPHPFPM, "acme": &a.InstallAcme,
	} {
		v, err := parseBool(values[key])
		if err != nil {
			return nil, fmt.Errorf("invalid --%s value %q: use y/n", key, values[key])
		}
		*dst = v
	}
	if a.AcmeClient != "acme.sh" && a.AcmeClient != "certbot" {
		return nil, fmt.Errorf("invalid --acme-client %q: use acme.sh or certbot", a.AcmeClient)
	}
	return a, nil
}

// promptAnswer asks one question showing the default (port of free_query):
// empty input keeps the default.
func promptAnswer(r *bufio.Reader, w io.Writer, question, def string) (string, error) {
	if _, err := fmt.Fprintf(w, "%s [%s]: ", question, def); err != nil {
		return "", fmt.Errorf("writing prompt: %w", err)
	}
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("reading answer: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

// parseBool accepts the y/n answers of the PHP installer plus Go booleans.
func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "y", "yes", "true", "1":
		return true, nil
	case "n", "no", "false", "0", "":
		return false, nil
	}
	return false, fmt.Errorf("not a boolean: %q", v)
}
