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

## Configure VM stuff

### Applying kernel settings
(These settings reset whenever the VM reboots)
Run:
```bash
# Kernel settings
sudo sysctl -w vm.overcommit_memory=1
sudo sysctl -w kernel.panic=10
sudo sysctl -w kernel.panic_on_oops=1

# Make /proc/sys read-write inside the containers:
incus exec k3s-master -- mount -o remount,rw /proc/sys
incus exec k3s-node1 -- mount -o remount,rw /proc/sys
##Restart K3s on both nodes:
incus exec k3s-master -- systemctl restart k3s
incus exec k3s-node1 -- systemctl restart k3s-agent
```

### Make /proc/sys read-write inside the containers:
  incus exec k3s-master -- mount -o remount,rw /proc/sys
  incus exec k3s-node1 -- mount -o remount,rw /proc/sys
#### 3. Restart K3s on both nodes:
  incus exec k3s-master -- systemctl restart k3s
  incus exec k3s-node1 -- systemctl restart k3s-agent
