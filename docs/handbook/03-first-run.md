# First run

This chapter takes you from a freshly installed MobFI to a
completed capture: device detected, app extracted, tree scanned,
report written. Budget fifteen minutes.

Use an Android device with USB debugging enabled and a debuggable
test app installed, or an emulator. That is the shortest path to a
successful first capture. iOS needs more setup, covered in the
Devices and Extraction chapters.

## Step 0: confirm the tools

```sh
$ mfi doctor
```

For an Android-only first run, you need `adb` to show `ok`. Ignore
iOS rows for now.

## Step 1: the guided wizard

Running `mfi` with no arguments starts the wizard. It walks the same
core code the subcommands use, so nothing you learn here is
throwaway.

```sh
$ mfi
```

The wizard runs five steps:

1. **Detect devices.** Lists everything reachable and asks you to
   pick one. `r` re-detects (useful after plugging in a cable or
   accepting the USB debugging prompt).
2. **List apps.** Shows user-installed apps by default. `a` widens
   the list to include system apps.
3. **Extract.** Asks for a destination directory. On iOS it also
   asks which AFC scope to use.
4. **Scan and/or diff.** Offers a secrets scan of what you just
   pulled, and optionally a diff against a second capture.
5. **Report.** Prints a summary and offers to write it to a file.

Type `q` (or press Ctrl-D) at any prompt to quit cleanly.

A leading `~` in any path prompt expands to your home directory.

### What the wizard looks like

```
MobFI guided wizard  -  type `q` (or Ctrl-D) at any prompt to quit.
Advanced users can run subcommands directly; see `mfi help`.

Step 1 - Detecting devices...

  1. emulator-5554          Pixel_7_API_34  (android/emulator, ready)
Select a device [1-1], [r]e-detect, or [q]uit: 1

Step 2 - Listing apps on Pixel_7_API_34...

  1. Example Target                   com.example.target
Select an app [1-1], [a]ll (incl. system), or [q]uit: 1

Step 3 - Extract com.example.target
  Destination directory: ~/captures/target-baseline
  extracted 148 file(s), 2317884 byte(s) to /home/op/captures/target-baseline

Step 4 - Scan and/or diff
  Scan the extracted tree for secrets? [Y/n]: y
  Known-secrets file to also search (optional, blank to skip):
  scanning...
  3 finding(s).
  Diff this capture against another extracted root? [y/N]: n

Step 5 - Report
...
```

The wizard's report is always redacted. To include raw secret
values, use `mfi report -show-secrets` (see the Reporting chapter).

## Step 2: the same capture with subcommands

Everything the wizard did maps to a subcommand. Once you know the
device serial and bundle id, this is faster and scriptable.

```sh
# 1. What is connected?
$ mfi detect
ID              NAME            PLATFORM  TRANSPORT  STATE
emulator-5554   Pixel_7_API_34  android   emulator   ready

# 2. What is installed?
$ mfi apps -device emulator-5554
BUNDLE ID            NAME            VERSION  DATA PATH                     INSTALL PATH
com.example.target   Example Target  1.4.2    /data/data/com.example.target /data/app/...

# 3. Pull it
$ mfi extract -device emulator-5554 \
      -app com.example.target \
      -out ~/captures/target-baseline

# 4. Look for secrets
$ mfi scan -root ~/captures/target-baseline

# 5. Summarise
$ mfi report -root ~/captures/target-baseline -out ~/captures/target-report.json
```

## Step 3: read what you pulled

Two ways to explore the capture.

**Render a single file** from the CLI:

```sh
$ mfi render -file ~/captures/target-baseline/shared_prefs/auth.xml
```

MobFI picks a renderer by content, not by extension: SQLite, JSON,
property list (binary and XML), generic XML, plain text, and a hex
dump as the catch-all.

**Browse the whole tree** in the GUI:

```sh
$ mfi-gui
```

Open the Render tab, point it at the capture directory, and click
through the file browser. Databases open in the Database tab.

## What "good" looks like

After a successful first run you should have:

- A destination directory that mirrors the app's on-device tree.
- A non-zero file count reported by the extract step.
- A scan that completed (zero findings is a perfectly good result).
- A report file you can open.

If the extract reported **0 files**, the device state does not allow
reading that app's private data. That is the single most common
first-run outcome. Go to the Extraction chapter and check your app
and device against the capability table.

## Environment variables

| Variable | Effect |
|---|---|
| `MFI_NO_UPDATE_CHECK` | Set to any value to disable the update check at startup |
| `MFI_BACKUP_PASSWORD` | iOS backup password, used by `mfi keys -backup` instead of `-password` |
| `MFI_UPDATED` | Set internally after a self-update re-exec; do not set manually |

Prefer `MFI_BACKUP_PASSWORD` over `-password` on shared hosts:
command-line arguments are visible to other users through the
process table.

## Next

- Devices: connection methods, states, and what to do when a device
  does not appear.
- Extraction: which scope or mechanism to use for your target.
