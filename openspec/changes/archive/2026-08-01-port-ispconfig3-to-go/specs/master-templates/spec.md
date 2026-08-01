# master-templates

## ADDED Requirements

### Requirement: ISPConfig .master template renderer
The system SHALL render ISPConfig `.master` templates supporting `<tmpl_var name>`, `<tmpl_if name op value>`, `<tmpl_else>`, `<tmpl_unless>`, `<tmpl_loop name>` with scalar vars and loop datasets (port of the subset of `server/lib/classes/tpl.inc.php` used by nginx and bind templates).

#### Scenario: Vhost template renders
- **WHEN** `nginx_vhost.conf.master` (copied verbatim from ISPConfig) is rendered with a web_domain fixture
- **THEN** the output matches the golden file produced by the PHP engine for the same fixture

#### Scenario: Loop rendering
- **WHEN** `bind_pri.domain.master` is rendered with a zone and a list of records
- **THEN** each active record produces one zone-file line in order

### Requirement: Custom template override directory
Template resolution SHALL check the custom directory (`[templates] custom_dir`, default `/etc/go-ispconfig/templates-custom/`) before the embedded set: a file with the same name overrides the embedded template (conf-custom parity with ISPConfig3). Renders SHALL log which source was used.

#### Scenario: Custom template wins
- **WHEN** `nginx_vhost.conf.master` exists in the custom dir and a vhost is rendered
- **THEN** the custom file is used and the log records the custom source

#### Scenario: Missing custom falls back
- **WHEN** no custom file exists for a template
- **THEN** the embedded template is used

### Requirement: Template export CLI
`go-ispconfig templates` SHALL provide `list` (all embedded templates, marking which are overridden) and `export <name>|--all` (write embedded originals to the custom dir, refusing to overwrite existing files without `--force`).

#### Scenario: Export for customization
- **WHEN** the operator runs `go-ispconfig templates export nginx_vhost.conf.master`
- **THEN** the embedded original is written to the custom dir, ready to edit, and a second export without `--force` refuses

### Requirement: Golden-file compatibility tests
The template engine SHALL ship golden-file tests for every `.master` template bundled with go-ispconfig (nginx vhost, php-fpm pool, bind zone, named.conf.local primary and slave).

#### Scenario: Engine regression detected
- **WHEN** an engine change alters the rendered output of any bundled template fixture
- **THEN** the test suite fails showing the diff
