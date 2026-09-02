
# The order of execution

## Install
preinst: install
postinst: configure

## Update
prerm: upgrade
preinst: upgrade
postrm: upgrade
postinst: configure

## Remove
prerm: remove   
postrm: remove 

## Purge
prerm: remove   
postrm: remove
postrm: purge 
