package main

const 默认主世界配置 = `[NETWORK]
server_port = 11000

[SHARD]
is_master = true

[STEAM]
master_server_port = 27018
authentication_port = 8768`

const 默认洞穴配置 = `[NETWORK]
server_port = 11001

[SHARD]
is_master = false
name = Caves

[STEAM]
master_server_port = 27019
authentication_port = 8769`

const 默认cluster = `
[GAMEPLAY]
game_mode = endless
max_players = 32
pvp = false
pause_when_empty = true

[NETWORK]
cluster_description =
cluster_name = DST-Nucleus Server
cluster_password = 

[MISC]
console_enabled = true

[SHARD]
shard_enabled = true
bind_ip = 127.0.0.1
master_ip = 127.0.0.1
master_port = 10889
cluster_key = supersecretkey
`
