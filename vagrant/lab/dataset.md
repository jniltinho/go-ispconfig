# Lab fixture dataset

Catalog of every fixture entity `fixtures.sh` creates on the two legacy
lab VMs — `legacy` (nginx, 192.168.56.20, suffix `n`) and `legacy-apache`
(apache2, 192.168.56.21, suffix `a`). All credentials below are fixed
**test-only** values for this throwaway lab (same convention as
`GoIspParity2026` in the Vagrantfile); never reuse them anywhere real.

Creation is remote-API driven (`remote/json.php`, user `goisp-lab` /
`GoIspRemote2026`) and idempotent: each entity is looked up by its natural
key (username / domain / email / ...) and only created when missing.
Re-running `make vagrant-lab-fixtures` is always safe.

Replace `$S` with the VM suffix (`n` or `a`) throughout.

## Clients (password `LabPw2026!`)

| Username    | Role                    | Limit profile                          |
|-------------|-------------------------|----------------------------------------|
| labc1$S     | direct client           | 5 sites, 2 maildomains, 10 mailboxes   |
| labc2$S     | direct client           | 2 sites, 1 maildomain, 3 mailboxes     |
| labc3$S     | direct client           | 1 site, no mail                        |
| labres1$S   | reseller (limit_client 5) | 10 sites, 5 maildomains              |
| labchild1$S | child of labres1$S      | 1 site, 1 maildomain                   |

The `legacy` VM additionally carries the parity clients `pclient1` /
`pclient2` (password `ParityPw2026!`) — see `vagrant/parity/dataset.md`.

## Websites

| Domain                | Owner       | Kind                                    |
|-----------------------|-------------|-----------------------------------------|
| wp1$S.goisp.test      | labc1$S     | WordPress (installed by task 3.1)       |
| wp2$S.goisp.test      | labc2$S     | WordPress (installed by task 3.1)       |
| php1$S.goisp.test     | labchild1$S | plain PHP (php-fpm)                     |
| static1$S.goisp.test  | labc3$S     | static (php=no) + custom directives (`X-Goisp-Lab` header) |

All vhosts: auto-subdomain `www`, quotas -1. The `legacy` VM also has the
parity sites `parity1/parity2.goisp.test`.

## DNS zones (owner labc1$S; wizard template "Default")

Zones: `wp1$S`, `wp2$S`, `php1$S`, `static1$S` `.goisp.test` —
NS `ns1/ns2.goisp.test`, hostmaster@goisp.test, A = own server IP.
Extra records on `wp1$S.goisp.test`:

| Record                          | Type  | Data                          |
|---------------------------------|-------|-------------------------------|
| ipv6.wp1$S.goisp.test.          | AAAA  | fd00::20                      |
| blog.wp1$S.goisp.test.          | CNAME | wp1$S.goisp.test.             |
| wp1$S.goisp.test.               | TXT   | v=spf1 mx a -all              |
| _sip._tcp.wp1$S.goisp.test.     | SRV   | 5 5060 sip.wp1$S.goisp.test. (prio 10) |

The `legacy` VM also has the parity zone `parity1.goisp.test.`.

## Email (domain mail$S.goisp.test, mailbox password `LabMailPw2026!`)

| Address                    | Kind                                        |
|----------------------------|---------------------------------------------|
| user1@mail$S.goisp.test    | mailbox                                     |
| user2@mail$S.goisp.test    | mailbox with autoresponder ("Out of office")|
| contact@mail$S.goisp.test  | alias -> user1@mail$S.goisp.test            |

## FTP users (password `LabFtpPw2026!`)

| Username  | Site             |
|-----------|------------------|
| wp1$Sftp  | wp1$S.goisp.test |
| wp2$Sftp  | wp2$S.goisp.test |

## Shell users (password `LabShellPw2026!`)

| Username | Site             | Chroot  |
|----------|------------------|---------|
| shell1$S | wp1$S.goisp.test | none    |
| shell2$S | wp2$S.goisp.test | jailkit |

## Databases (user password `LabDbPw2026!`)

| Database      | User          | Site             | Used by        |
|---------------|---------------|------------------|----------------|
| c{id}wp1$S    | c{id}wp1$S    | wp1$S.goisp.test | WordPress wp1  |
| c{id}wp2$S    | c{id}wp2$S    | wp2$S.goisp.test | WordPress wp2  |

`{id}` is the owning client's client_id (ISPConfig prefixes client DBs);
look it up with `fixtures.sh` output or `SELECT database_name FROM web_database`.

## WordPress (task 3.1, via wp-cli)

| URL                      | Admin user | Admin password  |
|--------------------------|------------|-----------------|
| http://wp1$S.goisp.test  | labadmin   | LabWpAdmin2026! |
| http://wp2$S.goisp.test  | labadmin   | LabWpAdmin2026! |
