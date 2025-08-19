# SimpleMusicSync

A simple music synchronization tool with optional transcoding support.

I originally intended this tool to be part of my Blutils project, but I decided to keep it separate.

## Features

* Command-line interface for syncing audio and image files from a source directory to a target directory.
* Incremental sync using a `.syncdb.json` in the target directory to avoid reprocessing unchanged files.
* Optional transcoding: provide command templates (commonly `ffmpeg`) for audio and images.
* If no command is provided for a file type, files are copied as-is.
* Include / exclude filtering using regular expressions (applied to the file's relative path). Includes override excludes.
* Supports configuring which file extensions are treated as audio and image inputs, and separate target extensions for audio/images.
* Optional deletion of files in the target that are no longer present in the source (`--delete-removed`).
* ~~Robust~~ command-template parsing: handles quoted arguments and backslash escapes when splitting template strings into arguments.

---

## Installation

You need Go installed (minimum Go 1.23.3).

```bash
# Run directly from source
go run . --help

# Or build a binary
go build -o simplemusicsync .
./simplemusicsync --help
```

---

## Usage

Basic invocation requires a source and a target directory:

```bash
simplemusicsync --source /path/to/source --target /path/to/target
```

### Key command-line options

* `--source` (required): Source directory to scan.
* `--target` (required): Target directory to write converted or copied files and the `.syncdb.json`.
* `--target-audio-extension` (default: `opus`): Extension to use for converted audio files.
* `--target-image-extension` (default: `jpeg`): Extension to use for converted images.
* `--source-audio-extensions` (default: `mp3,flac,opus`): Comma-separated list of recognized audio input extensions.
* `--source-image-extensions` (default: `jpg,jpeg,png,gif`): Comma-separated list of recognized image input extensions.
* `--ffmpeg-audio`: Command template to transcode audio. Use `$INPUT` and `$OUTPUT` placeholders.
* `--ffmpeg-image`: Command template to transcode images. Use `$INPUT` and `$OUTPUT` placeholders.
* `--delete-removed`: If present, delete files in the target that are not produced by the current run.
* `--exclude` (repeatable): Regex pattern to exclude files (matched against the file's path relative to the source). Can be specified multiple times.
* `--include` (repeatable): Regex pattern to include files (overrides excludes). Can be specified multiple times.

### Command template placeholders

When using `--ffmpeg-audio` or `--ffmpeg-image`, the program substitutes two placeholders:

* `$INPUT` – the full source file path.
* `$OUTPUT` – the full target file path.

Because the program splits the command string into arguments with proper handling of quoted strings and escapes, you can supply complex templates. When invoking from a shell, remember to escape the `$` (e.g. `\$INPUT`) or quote the whole template to avoid shell expansion.

---

## Example: iPod sync script

Here is an example script used to sync a music collection to an iPod-like target. I personally use this for my Rockbox iPod. It transcodes audio to Opus and scales cover images to a small bitmap format. Save this as a shell script and run it (note the escaping of `$` in the ffmpeg templates):

```bash
#!/bin/bash

go run . \
--source "/media/simon/fstor1/music/Music" \
--target "/media/simon/SIMONS IPOD/Music" \
--delete-removed \
--target-audio-extension "opus" \
--source-audio-extensions "mp3,flac,opus,wav,m4a" \
--ffmpeg-audio "ffmpeg -i \$INPUT -map_metadata 0 -map 0 -f ogg -c:a libopus -b:a 256k -vbr on -compression_level 10 -application audio -ar 48000 -vn -y \$OUTPUT" \
--target-image-extension "bmp" \
--source-image-extensions "jpg,png,webp" \
--ffmpeg-image "ffmpeg -i \$INPUT -vf \"scale=512:-2\" -y \$OUTPUT"
```
To use this script `ffmpeg` must be installed and available in your `PATH`.

---

## Internals & behavior notes

* The tool maintains a `.syncdb.json` file in the target directory to store information about previously processed files (source path, target path, size, modification time, and the command used). The DB is used to skip unchanged files on subsequent runs.
* If a ffmpeg (or other) command is configured for a file type, the program runs that command and treats a non-zero exit as an error for that file.
* If no command is configured for a detected file, the program copies the file from source to target instead.
* Exclude and include patterns are regular expressions (Go `regexp` syntax) and are matched against the file's relative path. Includes take precedence over excludes.
* The program specified in the `--ffmpeg-image` and `--ffmpeg-audio` flags does not need to be `ffmpeg` specifically; it can be any command that accepts the `$INPUT` and `$OUTPUT` placeholders.

---

## Limitations & TODO

* File metadata (such as timestamps or ownership) is not preserved when copying or transcoding.
* The program executes commands sequentially (no parallel processing).
* Regex errors are ignored silently for simplicity.
* The tool assumes `ffmpeg` (or whatever command you reference) is available in `PATH` when using command templates.

---

## Troubleshooting

* If files appear to be reprocessed unnecessarily, check that the `--ffmpeg-*` template string you pass is byte-for-byte identical between runs (the template is recorded in `.syncdb.json`).
* If the tool fails to run a command, it prints the combined stdout+stderr from the executed command to help debugging.
* If your include/exclude patterns don't behave as expected, test them separately on [regex101.com](https://regex101.com) or similar tools to ensure they match the intended paths.

---

## Contributing
This project is a personal tool and the code is quite messy. Unlike many of my other projects I have not and most likely will not use Conventional Commits, Semver or any other specific structure for this project. If you want to contribute, please don't. :) If there is a feature you would like to see, just make an issue and I will consider it.