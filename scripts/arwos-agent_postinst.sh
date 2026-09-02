#!/bin/bash
set -e


do_configure(){
	if [ -f "/etc/systemd/system/arwos-agent.service" ]; then
		systemctl start arwos-agent
		systemctl enable arwos-agent
		systemctl daemon-reload
	fi
}

do_other(){

}

case "$1" in
  configure)
    do_configure
    ;;
  *)
    do_other
    ;;
esac
