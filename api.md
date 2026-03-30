# DST-Nucleus HTTP API
The following examples use the default listener 127.0.0.1:20888. Please adjust them according to your actual environment.

## 1. /api/status
* Method: `GET`

* Endpoint: `http://127.0.0.1:20888/api/status`

### 1.2 Success Response (JSON)
```json
{
    "status": "success",
    "locks": {
        "game_upd": false,
        "mod_upd": false,
        "master_rdy": false,
        "caves_rdy": false
    },
    "performance": {
        "baseline_cycles": 0,
        "current_cycles": 0
    },
    "data": {
        "players": 4294967295,
        "enable_caves": true,
        "enable_master": true,
        "cycles": 4294967295,
        "season": 4294967295,
        "phase": 4294967295,
        "rem_days": 4294967295,
        "temp": 2147483647,
        "is_raining": false,
        "is_snowing": false,
        "moon_phase": 4294967295,
        "nightmare": 4294967295,
        "alter_awake": false,
        "boss_timers": {
            "deerclops": 4294967295,
            "bearger": 4294967295,
            "moose": 4294967295,
            "dragonfly": 4294967295,
            "beequeen": 4294967295,
            "klaus": 4294967295,
            "toadstool": 4294967295,
            "fuelweaver": 4294967295,
            "malbatross": 4294967295,
            "lordfruitfly": 4294967295,
            "antlion": 4294967295
        }
    }
}
```
### 1.3 Field Definitions

#### Note on Uninitialized States:
Values like `4294967295` (Max uint32) and `2147483647` (Max int32) serve as sentinel initial values. If you encounter these numbers in the payload, it strictly indicates that the game server is currently offline or the boot sequence is incomplete.

#### Locks (Core State)
* `game_upd`: Boolean. True if a game update is currently being processed.

* `mod_upd`: Boolean. True if mods are being updated.

* `master_rdy` / `caves_rdy`: Boolean. Indicates if the respective worlds are fully initialized and ready.

#### Performance (CPU Metrics)
* `baseline_cycles`: The absolute number of CPU clock cycles per millisecond, derived directly from the hardware base frequency. Used as the physical time baseline.

* `current_cycles`: The maximum cycle gap detected by the scheduling probe. Dividing this by `baseline_cycles` yields the absolute execution delay (bloat) in milliseconds.

#### Data (World State)
* `players`: Current online player count. (Caps at 127; displays this maximum if exceeded).

* `cycles`: Total days survived in the world. (Caps at 2097151; displays this maximum if exceeded).

* `season`: Current season ID (0: Autumn, 1: Winter, 2: Spring, 3: Summer).

* `phase`: Current time of day (0: Day, 1: Dusk, 2: Night).

* `rem_days`: Days remaining until the next season change.

* `temp`: Internal world temperature (Raw integer). (The absolute value caps at 8191; displays this maximum if exceeded).

* `moon_phase`: Current lunar cycle phase (1-20).

* `nightmare`: Current Nightmare Cycle state in the Ruins (0: None, 1: Calm, 2: Warn, 3: Wild, 4: Dawn).

* `alter_awake`: Boolean. Indicates if the Celestial Alter is active.

#### Boss Timers

Note on the Value 127 (Timer Saturation / State Indicator):

If any boss timer returns exactly 127, it is a 7-bit saturated value indicating one of the following edge cases:

* Unmet Conditions: The boss does not currently meet the prerequisites to spawn.

* Timer Paused: The internal spawn timer for this boss is currently suspended.

* Overflow: The actual remaining time strictly exceeds our maximum representable limit of 127 units (days/minutes).


Unless otherwise specified, all values represent Days.
* `deerclops` to `lordfruitfly`:Estimated days until the boss spawns or becomes active.
* `antlion`:Unlike other bosses, this timer tracks the Antlion's rage/stomp cycle in **Minutes**, not days.

## 2. /api/events (Real-Time Telemetry Stream)

Establishes a persistent Server-Sent Events (SSE) connection. Instead of sending repeated polling requests and wasting TCP handshakes, this endpoint attaches your client directly to the core's atomic ring buffer broadcaster.

### 2.1 Request
* Method: `GET`

* Endpoint:` http://127.0.0.1:20888/api/events`

* Headers: `Accept: text/event-stream`

### 2.2 Stream Behavior & Payload
**Tick Rate (Frequency):** Pushes a complete state snapshot every **500ms**.

**Zero-Allocation:** The core broadcasts the exact same memory pointer to all SSE listeners simultaneously, ensuring zero JSON encoding overhead per client.

**Data Structure:** The JSON payload contained in each SSE data: block is 100% identical to the response of /api/status. Please refer to Section 1.3 for the complete field dictionary and physical boundaries.

### 2.3 Example (curl)
To test the raw stream directly in your terminal, use the -N (no buffer) flag:

```
curl -N -H "Accept: text/event-stream" http://127.0.0.1:20888/api/events
```

## 3. /api/epoch/master & /api/epoch/caves (Epoch Sync Stream)

This SSE endpoint streams a raw int64 nanosecond timestamp representing the exact startup time (Epoch) of the target shard process. It is designed to handle cross-node lifecycle synchronization (e.g., Master on Server A, Caves on Server B).

### 3.1 Request
* Method: `GET` (SSE)

* Endpoints: <br>
    * `http://127.0.0.1:20888/api/epoch/master`

    * `http://127.0.0.1:20888/api/epoch/caves`

### 3.2 Payload & Behavior
The payload is a pure int64 string (e.g., 1678888888123456789). No JSON overhead.
When a shard successfully boots, the Core stamps it with a new nanosecond Epoch. If the shard process terminates, the Epoch drops to 0.

**Activation Condition (Strict XOR):** The Core will only spin up the Epoch heartbeat broadcaster if exactly one shard (Master or Caves, but not both) is enabled on this instance. If you are running both shards on the same physical server, this SSE stream remains intentionally dormant to conserve CPU cycles, as local orchestration does not require network-level causal locks.

### 3.3 Decoupled Synchronization Logic
The Core does not force Master and Caves to be strictly bound. You have full control over the sync topology via your configuration file:

* **Unilateral (One-way Sync):** You can configure the Caves to watch the Master's Epoch endpoint. If the Master restarts (the Epoch mutates) or crashes (the Epoch drops to 0 or the connection times out), the Caves process will automatically terminate and reboot to align with the new Master state.

* **Bilateral (Two-way Sync):** You can configure both shards to watch each other's endpoints. If either shard crashes or drops offline, the other will immediately suicide and reboot, guaranteeing absolute consistency across physical machines.

* **Disabled (Manual Control):** If you leave the sync URLs empty in the config, the Core will completely disable the Remote Epoch Sensor. Shards will run entirely independently. Use this if you prefer to handle cross-server orchestration using your own external scripts.

## 4. /api/command
This endpoint parasitically injects raw text (usually Lua scripts or console commands) directly into the standard input (stdin) pipe of the target game process. It completely bypasses the game's network layer.

### 4.1 Request
* Method: `POST`

* Endpoint: `http://127.0.0.1:20888/api/command?target={shard}`

* Query Parameter (`target`):

    * `master` (Default): Inject strictly into the Master shard.

    * `caves`: Inject strictly into the Caves shard.

    * `all`: Broadcast to both shards concurrently.

### 4.2 Payload (Raw Bytes)
Do NOT wrap your payload in JSON. The core performs a 1:1 byte transfer to the OS pipe.

* **Limit:** Strict 1024 bytes per request limit to prevent memory abuse.

* **Body:** The raw string or Lua code. The Core will automatically append a newline (\n) if omitted, guaranteeing execution.

Example: `c_announce("Hello from the Core")`

### 4.3 Zero-Blocking & Congestion Control
To maintain microsecond-level latency, this API employs ruthless concurrency limits. It refuses to wait for sluggish game processes.

HTTP 200 OK: Payload successfully handed over to the IPC channel.

HTTP 423 Locked: Rejected by the atomic spin-lock. Another command injection is currently being processed.

HTTP 503 Service Unavailable: The internal buffer (100 slots) is full. If the target shard is lagging and cannot consume commands fast enough, the Core will deliberately drop your payload rather than blocking the gateway.

### 4.4 Injection Examples (curl)
**Example:** Direct Inline Injection
Injects a simple broadcast message into all shards concurrently. We use the standard -d (data) flag.

```
curl -X POST "http://127.0.0.1:20888/api/command?target=all" -d 'c_announce("Hello from the Nucleus Core")'
```

## 5. Lifecycle Control (/api/start, /api/stop, /api/restart)
These endpoints control the highest-level physical state of the server.
Crucial Note: These are Zero-Latency, Non-Blocking APIs. They do not hang the HTTP request waiting for the game to fully boot or save. They instantly return `{"status": "success"}` the moment the core accepts your command, and execute the actual process orchestration in the background.

* Method: `POST`

* Endpoint: 

    * `http://127.0.0.1:20888/api/start`
    * `http://127.0.0.1:20888/api/stop`
    * `http://127.0.0.1:20888/api/restart`

### 5.1 /api/start
Commands the Core to spin up the server processes.

Behavior: Flips the internal Master Switch to "ON" and wakes up the boot sequence.

Conflict Prevention: If the server is already booting or running, duplicate start requests are aggressively dropped, returning `HTTP 409 Conflict` (`{"status":"error", "message":"start blocked"}`).

### 5.2 /api/stop
Triggers a highly aggressive shutdown sequence.

Behavior: Flips the internal Master Switch to "OFF" (which physically prevents the watchdog from auto-restarting the game) and broadcasts a termination order to all active processes.

The 5-Second Guillotine: The Core first sends a gentle `SIGINT` to the game, giving it a chance to gracefully save the world to the disk. If the game process freezes or ignores the signal for more than 5 seconds, the Core deploys a ruthless `SIGKILL` to annihilate the process directly from the OS kernel.

### 5.3 /api/restart
A macro endpoint that executes a clean cycle.

Behavior: Flips the Master Switch to "ON" (ensuring the server will come back), triggers the exact same shutdown sequence as /api/stop, and queues a wake-up signal for the next loop. It is the safest and fastest way to recycle the server processes without risking it staying offline.

## 6. File System I/O
The Core provides direct, bare-metal access to the underlying game configuration files. To guarantee data integrity and prevent dirty reads/writes during server orchestration, all file operations share a strict Global I/O Atomic Lock (FileIOGate).

### 6.1 /api/file/read (Atomic Stream Read)
Streams the exact bytes of a target configuration file directly to the network socket using zero-copy allocation (io.Copy).

* Method: `GET`

* Endpoint: `http://127.0.0.1:20888/api/file/read?target={target_name}`

#### Supported Targets
The `target` query parameter strictly limits access to predefined, whitelisted paths. Directory traversal attacks are physically impossible.

* `cluster`: Global cluster settings (`cluster.ini`).

* `master_server` / `caves_server`: Shard-specific settings (`server.ini`).

* `master_world` / `caves_world`: World generation overrides (`worldgenoverride.lua`).

* `master_mod` / `caves_mod`: Shard mod configurations (`modoverrides.lua`).

* `setup`: The mod Lua update script (`dedicated_server_mods_setup.lua`).

#### Hardware Protection & Edge Cases
HTTP 423 Locked: If a `/api/file/write` operation is currently holding the atomic lock, the read request will be instantly rejected to prevent dirty reads.

HTTP 413 Payload Too Large: The Core enforces a strict 1MB (`1024*1024` bytes) hard limit on any single file read. Because the Core utilizes zero-copy streaming (io.Copy) directly from disk to the network socket, memory footprint is irrelevant. This hard limit exists strictly to prevent massive files from monopolizing Disk I/O and holding the Global I/O Atomic Lock hostage, which would paralyze the entire configuration bus.

Graceful Missing File Handling: If the target file does not exist on the disk (e.g., a newly created server without overrides), the API does not error out. It returns a `HTTP 200 OK` with an empty plain text body (""), representing an empty configuration state.

### 6.2.1 /api/file/write (Anti-Corruption Stream Write)
This endpoint securely overwrites the specified configuration file. It does not buffer the payload into memory. Instead, it streams the raw incoming HTTP body directly to the physical disk.

* Method: `POST`

* Endpoint: `http://127.0.0.1:20888/api/file/write?target={target_name}`

* Payload: Raw plain text (e.g., Lua code or INI structure). Maximum size strictly locked at 1MB (`1024*1024` bytes).

#### Supported Targets
* `cluster`: Global cluster settings (`cluster.ini`).

* `master_server` / caves_server: Shard-specific settings (`server.ini`).

* `master_world` / `caves_world`: World generation overrides (`worldgenoverride.lua`).

* `mod`: **(Topology-Aware Macro)** Dynamically resolves the destination for modoverrides.lua based on the active shards configured on this specific physical node.

* **Dual Node:** If both Master and Caves are enabled, it writes to Master and instantly clones the payload to Caves, ensuring absolute cross-shard parity.

* **Distributed Node:** If only one shard is enabled (e.g., a dedicated Caves server), it writes strictly to that active shard's directory.

* **Strict Isolation Failsafe:** If neither shard is enabled on this instance, the Core drops the payload immediately and returns `HTTP 400 Bad Request` (`{"status":"error", "message":"no active shard configured on this node"}`). It will absolutely not write orphaned files into inactive directories.

* `setup`: The mod Lua update script (`dedicated_server_mods_setup.lua`). **Crucial for triggering Mod downloads on boot.**

#### The "Anti-Corruption" Engine (How it works under the hood)
To ensure that a server crash or power failure during a write operation never corrupts your game configuration, the Core uses a strict atomic commit process:

* **Temporary Streaming:** The incoming HTTP stream is written to a hidden temporary file (tmp_stream_*).

* **Hardware Flush (Sync):** The Core forces the OS to flush its I/O buffers, committing the temp file physically to the disk platters.

* **Atomic Rename (os.Rename):** The OS atomically swaps the temp file over the old configuration file. This guarantees that the game process will only ever see either the fully intact old file or the fully intact new file—never a half-written corrupted state.

* **Collision Resistance:** If the game engine happens to be reading the file at the exact millisecond the swap occurs (causing an OS lock), the Core will relentlessly retry the atomic swap up to 5 times (at 10ms intervals) before throwing an HTTP 500 error.

### 6.2.2 Write Examples (Shell Automation)
**Example: Injecting Multi-line Configuration via Bash Variable**
When writing automation scripts, you will often assemble configurations in memory before pushing them.

```bash
#!/bin/bash

MY_CONFIG=$(cat << 'EOF'
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
EOF
)

curl -X POST "http://127.0.0.1:20888/api/file/write?target=cluster"  --data-binary "$MY_CONFIG"
```

## 7. /api/update/state
This endpoint acts as the ultra-high-frequency data ingestion pipeline for the internal game probes. It is designed for absolute maximum throughput and zero memory allocation (Zero-GC overhead).

* **Method:** Strictly `POST`.

* **Endpoint:** `http://127.0.0.1:20888/api/update/state`

* **Content-Type:** Any (The parser ignores headers and operates purely on the physical payload bytes).

### 7.1 Architecture Role & Execution Isolation
Fundamentally, this API is engineered as the telemetry bridge in a distributed dual-shard architecture, allowing the Caves process to synchronize its state with the Master node.

However, it is exposed as an open ingestion port. If you have deployed custom external probes or scripts, you can freely inject telemetry data here.<br>
**Strict Boundary Warning:** The Core acts strictly as a **passive aggregator** for this endpoint. Data submitted here is written directly to the memory ring buffer and exposed purely for UI rendering via `/api/status`.** None of this telemetry data will ever be evaluated for internal orchestration.** For example, during a Graceful Restart, the gateway does not check the `players` telemetry submitted here to determine if the server is empty. Instead, it injects a raw Lua condition (`if #GetPlayerClientTable() == 0...`) directly into the game's stdin. The game engine itself physically evaluates the truth. The telemetry data here remains strictly informational and has zero physical authority.

### 7.2 Payload Format (Zero-Allocation Protocol)
**DO NOT use JSON.** The payload must be a raw string of `key=value` pairs, delimited by `&`, `;`, or `\n`.
The Core's custom byte-cursor scanner processes this payload directly in the read buffer without allocating intermediate strings.

#### Example Payload:

```plaintext
players=6&cycles=120&season=2&temp=-5&is_raining=1&deerclops=4294967295
```
### 7.3 Supported Telemetry Keys
All unlisted keys are silently skipped by the scanner.

World State:

* `players`: Online player count (`uint32`).

* `cycles`: Total survived days (`uint32`).

* `season`: Current season enum (`uint32`).

* `phase`: Day/Dusk/Night phase enum (`uint32`).

* `rem_days`: Remaining days in the current season (`uint32`).

* `temp`: Absolute world temperature (`int32`).

* `moon_phase`: Moon phase enum (`uint32`).

* `nightmare`: Ruin nightmare phase enum (`uint32`).

Boolean Flags (Optimized Parsing):
Accepts `1`, `t`, or `T` as true; anything else is evaluated as false.

* `is_raining`: Precipitation state.

* `is_snowing`: Snow state.

* `alter_awake`: Celestial alter state.

Boss Radar (Countdown Timers):
If invalid data is sent, the engine instantly defaults to 4294967295 (Inactive/Not spawned).

Keys: `deerclops`, `bearger`, `moose`, `dragonfly`, `beequeen`, `klaus`, `toadstool`, `fuelweaver`, `malbatross`, `lordfruitfly`, `antlion`.