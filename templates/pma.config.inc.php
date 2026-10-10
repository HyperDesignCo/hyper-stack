<?php
// Managed by Hyper Stack. MySQL root has no password on new installs; an older
// install's password comes from the process environment, never from this file.
$cfg['blowfish_secret'] = 'hyper-stack-local-only-0123456789abcdef';
$cfg['Servers'][1]['auth_type'] = 'config';
$cfg['Servers'][1]['host'] = '127.0.0.1';
$cfg['Servers'][1]['port'] = (int) getenv('HYPER_MYSQL_PORT');
$cfg['Servers'][1]['user'] = 'root';
$cfg['Servers'][1]['password'] = (string) getenv('HYPER_MYSQL_PASSWORD');
$cfg['Servers'][1]['AllowNoPassword'] = true;
$cfg['TempDir'] = getenv('HYPER_PMA_TMP');
