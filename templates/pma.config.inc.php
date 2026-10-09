<?php
// Managed by Hyper Stack. The MySQL password comes from the process environment
// (set from the OS credential store), never from this file.
$cfg['blowfish_secret'] = 'hyper-stack-local-only-0123456789abcdef';
$cfg['Servers'][1]['auth_type'] = 'config';
$cfg['Servers'][1]['host'] = '127.0.0.1';
$cfg['Servers'][1]['port'] = (int) getenv('HYPER_MYSQL_PORT');
$cfg['Servers'][1]['user'] = 'root';
$cfg['Servers'][1]['password'] = (string) getenv('HYPER_MYSQL_PASSWORD');
$cfg['Servers'][1]['AllowNoPassword'] = false;
$cfg['TempDir'] = getenv('HYPER_PMA_TMP');
