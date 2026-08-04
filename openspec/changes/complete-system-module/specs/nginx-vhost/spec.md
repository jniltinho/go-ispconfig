# nginx-vhost

## MODIFIED Requirements

### Requirement: Custom nginx directives merged with blacklist enforcement
The nginx plugin SHALL merge the `nginx_directives` field into the rendered vhost (port of `nginx_merge_locations`): custom `location` blocks with the same path replace/extend the template's block, `{FASTCGIPASS}` placeholders are substituted, and other directives are appended inside the `server` block. Before merging, every directive line SHALL be checked against the embedded `security/nginx_directives.blacklist` regex list; matching lines SHALL be stripped from the output and reported as a datalog error, never written to disk.

The plugin SHALL additionally emit the directive snippet referenced by `web_domain.directive_snippets_id` at a **named insertion point** in the template rather than by appending it, and SHALL subject the snippet body to the same blacklist check as `nginx_directives`.

#### Scenario: Custom location block overrides template
- **WHEN** `nginx_directives` contains `location / { try_files $uri /index.php?$args; }`
- **THEN** the rendered vhost's `location /` block contains the custom content merged with the template content

#### Scenario: Blacklisted directive is stripped and reported
- **WHEN** `nginx_directives` contains `load_module /tmp/evil.so;`
- **THEN** the rendered vhost does not contain the line and a datalog error records the rejection

#### Scenario: Referenced snippet is emitted at its insertion point
- **WHEN** a site references an active nginx directive snippet and its vhost is rendered
- **THEN** the snippet body appears at the named insertion point and nowhere else

#### Scenario: Blacklisted line inside a snippet is stripped
- **WHEN** a referenced snippet contains a blacklisted directive
- **THEN** that line is absent from the rendered vhost and a datalog error records the rejection

#### Scenario: No reference means no change to the output
- **WHEN** a site references no snippet
- **THEN** the rendered vhost is byte-identical to what it was before this change
