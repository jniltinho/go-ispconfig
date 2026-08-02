<?php
// Golden generator (add-mail-module task 4.2): renders sieve_filter.master
// through ISPConfig's own tpl engine with the exact variable pipeline of
// maildeliver_plugin::update, one before/after pair per fixture.
//
// Run from the repo root:
//   docker run --rm -v "$PWD":/work -w /work php:8.2-cli \
//     php internal/mail/golden/generate.php
error_reporting(E_ERROR | E_PARSE); // the 2009-era tpl engine is noisy on 8.x
define('ISPC_CLASS_PATH', '/work/base/ispconfig3_install/server/lib/classes');
require ISPC_CLASS_PATH.'/tpl.inc.php';

$template = '/work/base/ispconfig3_install/server/conf/sieve_filter.master';
$fixtures = json_decode(file_get_contents('/work/internal/mail/golden/fixtures.json'), true);

foreach ($fixtures as $name => $f) {
    foreach (array('before', 'after') as $sieve_script) {
        $tpl = new tpl();
        $tpl->newTemplate($template);

        if ($f['forward_in_lda'] == 'y' && $f['cc'] != '') {
            $tmp_addresses_arr = array();
            foreach (explode(',', $f['cc']) as $address) {
                if (trim($address) != '') $tmp_addresses_arr[] = array('address' => trim($address));
            }
            $tpl->setVar('cc', $f['cc']);
            $tpl->setLoop('ccloop', $tmp_addresses_arr);
        }

        $tpl->setVar('custom_mailfilter', str_replace("\r\n", "\n", $f['custom_mailfilter']));
        $tpl->setVar('move_junk', $f['move_junk']);
        $tpl->setVar('imap_prefix', $f['imap_prefix']);
        $tpl->setVar('start_date', str_replace(' ', 'T', $f['autoresponder_start_date']));
        $tpl->setVar('end_date', str_replace(' ', 'T', $f['autoresponder_end_date']));
        $tpl->setVar('autoresponder', $f['autoresponder']);
        $tpl->setVar('autoresponder_subject', str_replace('"', "'", $f['autoresponder_subject']));
        $tpl->setVar('autoresponder_text', str_replace('"', "'", $f['autoresponder_text']));

        $address_str = '';
        if (count($f['addresses']) > 0) {
            $address_str .= ':addresses [';
            foreach ($f['addresses'] as $rec) {
                $address_str .= '"'.$rec.'",';
            }
            $address_str = substr($address_str, 0, -1);
            $address_str .= ']';
        }
        $tpl->setVar('addresses', $address_str);
        $tpl->setVar('sieve_script', $sieve_script);

        $out = $tpl->grab();
        file_put_contents("/work/internal/mail/golden/$name.$sieve_script.sieve", $out);
        echo "$name.$sieve_script.sieve: ".strlen($out)." bytes\n";
    }
}
