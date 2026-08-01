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

### Requirement: Golden-file compatibility tests
The template engine SHALL ship golden-file tests for every `.master` template bundled with go-ispconfig (nginx vhost, php-fpm pool, bind zone, named.conf.local primary and slave).

#### Scenario: Engine regression detected
- **WHEN** an engine change alters the rendered output of any bundled template fixture
- **THEN** the test suite fails showing the diff
