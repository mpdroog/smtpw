<?php
function msg($msg) {
	echo $msg . "\n";
}
$json = file_get_contents(__DIR__ ."/test.json");
$len = strlen($json);

$fd = fsockopen("127.0.0.1", "11300", $errno, $errstr, 10);
if (! $fd) {
	die(sprintf("%d - %s", $errno, $errstr));
}
fwrite($fd, "use email\r\n");
msg(stream_get_line($fd, 999999999999, "\r\n"));
fwrite($fd, "put 100 0 1 $len\r\n");
fwrite($fd, "$json\r\n");
msg(stream_get_line($fd, 999999999999, "\r\n"));
fwrite($fd, "quit\r\n");
fclose($fd);
