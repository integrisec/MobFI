# Console

The Console is an interactive terminal to the device, embedded in
the desktop GUI. Use it when a capture raises a question that only
the live device can answer: what does that directory look like right
now, is that process running, what does the app write when you tap
the button.

The Console is **GUI only**. From the CLI, run `adb shell` or `ssh`
directly; the Console adds a terminal in the same window as your
captures, plus session transcript logging.

## Android

Select the device, open the Console tab, and start a session. MobFI
runs `adb -s <serial> shell` in a pseudo-terminal, so interactive
programs, job control, and colour output all behave normally.

Useful once you are in:

```sh
# What can the shell user actually read?
$ run-as com.example.target ls -la /data/data/com.example.target

# Is the app running, and as which uid?
$ ps -A | grep example

# What did the app just write?
$ run-as com.example.target ls -lt /data/data/com.example.target/files | head

# Live log for the target
$ logcat --pid=$(pidof com.example.target)
```

The `run-as` prefix is the same mechanism extraction uses: it works
for debuggable apps without root. On a rooted device, `su` gives
you the same reach for any app.

## iOS

iOS Console requires a **jailbroken device running `sshd`**. Stock
iOS has no shell to connect to.

Two transports:

**USB** (recommended): MobFI starts `iproxy` to forward a local port
to the device's SSH port, then connects `ssh` through it. Select the
device and leave the host field empty.

**Network**: supply the device's hostname or IP directly. Fill in
the SSH host field.

The user defaults to `root` and the port to `22`; both are
adjustable in the tab.

Useful once you are in:

```sh
# Find the app's container
$ find /var/mobile/Containers/Data/Application -maxdepth 2 -name '*.plist' 2>/dev/null

# What is installed
$ ls /var/containers/Bundle/Application/

# Watch the app's files change
$ ls -lt /var/mobile/Containers/Data/Application/<uuid>/Documents
```

**OPSEC**: SSH to a device on the network is visible to anyone
watching that network segment, and the connection appears in the
device's own logs. Over USB via `iproxy`, the traffic stays on the
cable.

### Host-key handling

On this version, the Console connects with
`StrictHostKeyChecking=accept-new` and `UserKnownHostsFile=/dev/null`,
meaning the host key is accepted and then forgotten. On a loopback
USB forward that is reasonable: the trust boundary is the cable.

Over a **network** connection it means no host-key drift detection:
an attacker on the network segment who can present a forged key will
not trigger a warning. When connecting to a device over an untrusted
network, prefer the USB transport, or SSH manually with a real
known-hosts file:

```sh
$ ssh -o UserKnownHostsFile=~/.ssh/known_hosts_mobfi \
      -o StrictHostKeyChecking=accept-new root@<device-ip>
```

## Session transcripts

Supply a log path when starting a session and MobFI writes a
transcript of everything the session produced.

**Evidence**: a transcript is the record of what you ran on the
device and what it returned. For any engagement where your actions
on a device may be questioned later, turn it on. Store the
transcript alongside the captures for that device.

The transcript captures session output, including anything you type
that the device echoes back. Assume **credentials typed into the
session end up in the file**, and handle it accordingly.

## Practical patterns

**Confirm an extraction gap.** The extract reported skipped paths;
check whether they are genuinely unreadable or whether the wrong
mechanism was used:

```sh
$ run-as com.example.target ls -la /data/data/com.example.target/cache
```

**Watch a file appear.** Diffing tells you a file changed between
captures; the Console tells you when it changes:

```sh
$ watch -n1 'run-as com.example.target ls -l /data/data/com.example.target/files'
```

**Check the app's own view.** The app may hold data in memory that
never lands on disk. `dumpsys` and `logcat` surface some of it
without touching storage.

**Verify a permission.** The Apps tab lists granted permissions;
the Console proves which are effective:

```sh
$ dumpsys package com.example.target | grep -A20 'runtime permissions'
```

## In the GUI

The Console tab supports multiple concurrent sessions, each in its
own tab, with:

- A full xterm-compatible terminal (scrollback, colour, resize).
- Copy and paste, including right-click copy on selected text.
- Per-session transcript logging.
- A status line naming the transport in use, so you always know
  whether you are on USB or the network.

<!-- screenshot: console-tab-session.png -->

## Next

- Updating: keep MobFI current.
- Troubleshooting: when a workflow does not behave.
