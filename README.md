## Description
This project is used to implement a blockchain with an asynchronous BFT as the core consensus.

## Usage
### 1. Machine types
Machines are divided into two types:
- *Workcomputer*: just configure `servers` and `clients` at the initial stage, particularly via `ansible` tool 
- *Servers*: run daemons of `SimpleFT`, communicate with each other via P2P model
- *Clients*: run `client`, communicate with `server` via RPC model 

### 2. Precondition
- Recommended OS releases: Ubuntu 18.04 (other releases may also be OK)
- Go version: 1.12+ (with Go module enabled)
- Python version: 3.6.9+

### 3. Steps to run SimpleFT

#### 3.1 Install ansible on the work computer
Commands below are run on the *work computer*.
```shell script
sudo apt install python3-pip
sudo pip3 install --upgrade pip
pip3 install ansible
# add ~/.local/bin to your $PATH
echo 'export PATH=$PATH:~/.local/bin:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

#### 3.2 Login without passwords
Enable workcomputer to login in servers and clients without passwords.

Commands below are run on the *work computer*.
```shell script
# Generate the ssh keys (if not generated before)
ssh-keygen
ssh-copy-id -i ~/.ssh/id_rsa.pub $IP_ADDR_OF_EACH_SERVER
```

#### 3.3 Generate configurations
Generate configurations for each server.

Commands below are run on the *work computer*.

- Change `IPs`, `peers_p2p_port`, and `rpc_listen_port` in file `config_gen/config_template.yaml`
- Enter the directory `config_gen`, and run `go run main.go`

#### 3.4 Configure servers via Ansible tool
Change the `hosts` file in the directory `ansible`, the hostnames and IPs should be consistent with `config_gen/config_template.yaml`.
```shell script
# Enter the directory `ansible`
ansible-playbook conf-server.yaml -i hosts
```

#### 3.5 Run SimpleFT servers via Ansible tool
```shell script
# clean the stale processes and testbed directories
ansible-playbook clean-server.yaml -i hosts
# run yimchain servers
ansible-playbook run-server.yaml -i hosts
```