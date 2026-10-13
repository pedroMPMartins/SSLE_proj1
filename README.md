# System developed for SSLE Course


## Send code to the VM:
```bash
rsync -avz --delete ./ kali-vm:~/Desktop/ssle/
```


## Incus

Go into a container:
```bash
incus exec <container_name> -- sh
# OR
incus shell <container_name>
```

To exit the container just write `exit`.

Inspect a profile:
```bash
incus profile show <profile_name>
```

Get node token from the actual container:
```bash
# Must be run inside the container
cat /var/lib/rancher/k3s/server/node-token
```
