package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type optionsType struct {
	sourceDir             string
	targetDir             string
	targetAudioExtension  string
	targetImageExtension  string
	sourceAudioExtensions []string
	sourceImageExtensions []string
	ffmpegAudioCommand    string
	ffmpegImageCommand    string
	deleteRemovedFiles    bool
}

var options optionsType

type SyncDBEntry struct {
	SourcePath string    `json:"sourcePath"`
	TargetPath string    `json:"targetPath"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"modTime"`
	Command    string    `json:"command"`
}

type syncDB struct {
	Entries []SyncDBEntry `json:"entries"`
}

func main() {
	sourceDir := flag.String("source", "", "Source directory")
	targetDir := flag.String("target", "", "Target directory")
	targetAudioExt := flag.String("target-audio-extension", "opus", "Extension for converted audio")
	targetImageExt := flag.String("target-image-extension", "jpeg", "Extension for converted images")
	sourceAudioExts := flag.String("source-audio-extensions", "mp3,flac,opus", "Comma-separated audio extensions")
	sourceImageExts := flag.String("source-image-extensions", "jpg,jpeg,png,gif", "Comma-separated image extensions")
	ffmpegAudio := flag.String("ffmpeg-audio", "", "FFmpeg command template for audio")
	ffmpegImage := flag.String("ffmpeg-image", "", "FFmpeg command template for images")
	deleteRemoved := flag.Bool("delete-removed", false, "Delete files in target not present in source")

	flag.Parse()

	options = optionsType{
		sourceDir:             *sourceDir,
		targetDir:             *targetDir,
		targetAudioExtension:  *targetAudioExt,
		targetImageExtension:  *targetImageExt,
		sourceAudioExtensions: strings.Split(*sourceAudioExts, ","),
		sourceImageExtensions: strings.Split(*sourceImageExts, ","),
		ffmpegAudioCommand:    *ffmpegAudio,
		ffmpegImageCommand:    *ffmpegImage,
		deleteRemovedFiles:    *deleteRemoved,
	}

	if options.sourceDir == "" || options.targetDir == "" {
		fmt.Println("Source and target directories must be specified.")
		flag.Usage()
		os.Exit(1)
	}

	options.sourceDir, _ = filepath.Abs(options.sourceDir)
	options.targetDir, _ = filepath.Abs(options.targetDir)

	if err := os.MkdirAll(options.targetDir, 0755); err != nil {
		fmt.Println("Error creating target directory:", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(options.targetDir, ".syncdb.json")
	var oldDB syncDB
	oldDB.Load(dbPath)

	var newDB syncDB

	err := filepath.Walk(options.sourceDir, func(sourcePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(sourcePath)), ".")
		isAudio := isAudioExtension(ext)
		isImage := isImageExtension(ext)

		if !isAudio && !isImage {
			return nil
		}

		relPath, _ := filepath.Rel(options.sourceDir, sourcePath)
		targetExt := options.targetAudioExtension
		ffmpegCmd := options.ffmpegAudioCommand

		if isImage {
			targetExt = options.targetImageExtension
			ffmpegCmd = options.ffmpegImageCommand
		}

		targetFile := filepath.Join(
			options.targetDir,
			strings.TrimSuffix(relPath, filepath.Ext(sourcePath))+"."+targetExt)
		relTargetPath, _ := filepath.Rel(options.targetDir, targetFile)

		var existingEntry *SyncDBEntry
		for _, e := range oldDB.Entries {
			if e.SourcePath == relPath {
				existingEntry = &e
				break
			}
		}

		sourceInfo, _ := os.Stat(sourcePath)
		needsProcessing := existingEntry == nil ||
			existingEntry.Size != sourceInfo.Size() ||
			existingEntry.Command != ffmpegCmd ||
			!existingEntry.ModTime.Equal(sourceInfo.ModTime()) ||
			!fileExists(targetFile)

		if needsProcessing {
			os.MkdirAll(filepath.Dir(targetFile), 0755)
			if ffmpegCmd != "" {
				args, err := parseCommandTemplate(ffmpegCmd, sourcePath, targetFile)
				if err != nil {
					fmt.Printf("Error parsing ffmpeg command: %v\n", err)
					return err
				}
				if len(args) == 0 {
					fmt.Printf("Empty ffmpeg command for %s\n", sourcePath)
					return nil
				}
				cmd := exec.Command(args[0], args[1:]...)
				if output, err := cmd.CombinedOutput(); err != nil {
					fmt.Printf("Error processing %s: %v\nOutput: %s\n", sourcePath, err, string(output))
					return err
				}
				fmt.Printf("Processed: %s\n", relPath)
			} else if err := copyFile(sourcePath, targetFile); err != nil {
				fmt.Printf("Error copying %s: %v\n", sourcePath, err)
				return err
			}
		} else {
			fmt.Printf("Skipping (up-to-date): %s\n", sourcePath)
		}

		newDB.Entries = append(newDB.Entries, SyncDBEntry{
			SourcePath: relPath,
			TargetPath: relTargetPath,
			Size:       sourceInfo.Size(),
			ModTime:    sourceInfo.ModTime(),
			Command:    ffmpegCmd,
		})
		return nil
	})

	if err != nil {
		fmt.Println("Error during processing:", err)
		os.Exit(1)
	}

	newDB.Save(dbPath)

	if options.deleteRemovedFiles {
		expected := make(map[string]bool)
		for _, e := range newDB.Entries {
			expected[e.TargetPath] = true
		}

		filepath.Walk(options.targetDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			relPath, _ := filepath.Rel(options.targetDir, path)
			if relPath == ".syncdb.json" || expected[relPath] {
				return nil
			}
			fmt.Printf("Deleting removed file: %s\n", path)
			return os.Remove(path)
		})
	}

	fmt.Println("Sync complete!")
}

func parseCommandTemplate(template string, inputPath string, outputPath string) ([]string, error) {
	args := splitCommand(template)
	for i, arg := range args {
		arg = strings.ReplaceAll(arg, "$INPUT", inputPath)
		arg = strings.ReplaceAll(arg, "$OUTPUT", outputPath)
		args[i] = arg
	}
	return args, nil
}

// splitCommand splits a command string into a slice of arguments, taking into account
// quoted substrings and escape sequences. It handles single quotes ('), double quotes ("),
// and backslashes (\) for escaping characters.
//
// Parameters:
//   - cmd: The command string to be split.
//
// Returns:
//   - A slice of strings, where each element represents an argument from the command string.
//
// Behavior:
//   - Quoted substrings are treated as a single argument, preserving spaces within the quotes.
//   - Escape sequences (e.g., \') are resolved, and the escaped character is included in the output.
//   - Whitespace outside of quotes is treated as a delimiter between arguments.
//   - Empty arguments are ignored unless explicitly quoted or escaped.
func splitCommand(cmd string) []string {
	var args []string
	var current strings.Builder
	var inQuote rune
	var escape bool

	for _, r := range cmd {
		if escape {
			current.WriteRune(r)
			escape = false
			continue
		}

		switch r {
		case '\\':
			escape = true
		case '\'', '"':
			if inQuote == 0 {
				inQuote = r
			} else if inQuote == r {
				inQuote = 0
			} else {
				current.WriteRune(r)
			}
		case ' ', '\t', '\n', '\r':
			if inQuote == 0 {
				if current.Len() > 0 || !unicode.IsSpace(r) {
					args = append(args, current.String())
					current.Reset()
				}
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

// isAudioExtension checks if the given file extension matches any of the
// audio file extensions specified in the sourceAudioExtensions slice.
// The comparison is case-insensitive.
func isAudioExtension(ext string) bool {
	for _, e := range options.sourceAudioExtensions {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

// isImageExtension checks if the given file extension matches any of the
// image file extensions specified in the sourceImageExtensions slice.
// The comparison is case-insensitive.
func isImageExtension(ext string) bool {
	for _, e := range options.sourceImageExtensions {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func (db *syncDB) Load(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	json.Unmarshal(data, db)
}

func (db *syncDB) Save(path string) {
	data, _ := json.MarshalIndent(db, "", "  ")
	os.WriteFile(path, data, 0644)
}
