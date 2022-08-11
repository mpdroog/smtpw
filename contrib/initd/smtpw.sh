#!/bin/bash
# /etc/rc.d/init.d/smtpw
#
# Init.d (CentOS) script for spawning smtpw.
#
. /etc/init.d/functions

SERVICE="smtpw"
DIR="/storage/smtpw"
USER="$SERVICE"
LOG="/var/log/$SERVICE"
PSNAME="smtpw -c=./config.json"

status() {
	ps aux | grep -v grep | grep "$PSNAME" > /dev/null
	# Invert
	OK="$?"
	return $OK
}
start() {
	if status
	then
		echo "$SERVICE already running."
		exit 1
	fi

	mkdir -p "$LOG"
	echo -n "Starting $SERVICE: "
	daemon --user="$USER" "$DIR/smtpw -c=\"./config.json\" &" 1>$LOG/stdout.log 2>$LOG/stderr.log
	RETVAL=$?
	echo ""
	return $RETVAL
}
stop() {
	if status
	then
		echo -n "Shutting down $SERVICE: "
		ps -ef | grep "$PSNAME" | grep -v grep | awk '{print $2}' | xargs kill -s SIGINT
		exit $?
	fi

	echo "$SERVICE is not running"
	exit 1
}

case "$1" in
	start)
		start
	;;
	stop)
		stop
	;;
	status)
		if status
		then
			echo "$SERVICE is running."
		else
			echo "$SERVICE is not running."
		fi
	;;
	restart)
		stop
		start
	;;
	*)
		echo "Usage: $0 {start|stop|restart}"
		exit 1
	;;
esac
