#!/bin/bash
set -e


do_remove(){

}

do_purge(){
	rm -rf /var/lib/arwos-agent
}

do_other(){

}

case "$1" in
  remove)
    do_remove
    ;;
  purge)
    do_purge
    ;;
  *)
    do_other
    ;;
esac
