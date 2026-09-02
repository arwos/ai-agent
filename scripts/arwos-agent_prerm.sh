#!/bin/bash
set -e


do_remove(){
	if [ -f "/etc/systemd/system/arwos-agent.service" ]; then
		systemctl stop arwos-agent
		systemctl disable arwos-agent
		systemctl daemon-reload
	fi
}

do_upgrade(){
	if [ -f "/etc/systemd/system/arwos-agent.service" ]; then
		systemctl stop arwos-agent
		systemctl disable arwos-agent
		systemctl daemon-reload
	fi
}

do_other(){

}

case "$1" in
  remove)
    do_remove
    ;;
  upgrade)
    do_upgrade
    ;;
  *)
    do_other
    ;;
esac
