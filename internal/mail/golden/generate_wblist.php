<?php
// Golden generator for rspamd_wblist.inc.conf.master (task 6.2):
//   docker run --rm -v "$PWD":/work -w /work php:8.2-cli \
//     php internal/mail/golden/generate_wblist.php
error_reporting(E_ERROR | E_PARSE);
define('ISPC_CLASS_PATH', '/work/base/ispconfig3_install/server/lib/classes');
require ISPC_CLASS_PATH.'/tpl.inc.php';

$template = '/work/base/ispconfig3_install/server/conf/rspamd_wblist.inc.conf.master';
$fixtures = json_decode(file_get_contents('/work/internal/mail/golden/wblist_fixtures.json'), true);

foreach ($fixtures as $name => $f) {
    $tpl = new tpl();
    $tpl->newTemplate($template);
    $tpl->setVar('list_scope', $f['list_scope']);
    $tpl->setVar('record_id', $f['record_id']);
    $tpl->setVar('priority', $f['priority']);
    $tpl->setVar('from', $f['from']);
    $tpl->setVar('recipient', $f['recipient']);
    $tpl->setVar('hostname', $f['hostname']);
    $tpl->setVar('ip', $f['ip']);
    $tpl->setVar('wblist', $f['wblist']);
    file_put_contents("/work/internal/mail/golden/wblist-$name.conf", $tpl->grab());
    echo "wblist-$name.conf\n";
}
