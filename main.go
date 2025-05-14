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
)

type optionsType struct {
	sourceDir             string
	targetDir             string
	targetAudioExtension  string
	sourceAudioExtensions []string
	ffmpegCommand         string
	deleteRemovedFiles    bool
}

var options optionsType

type SyncDBEntry struct {
	SourcePath string    `json:"sourcePath"`
	TargetPath string    `json:"targetPath"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"modTime"`
}

type syncDB struct {
	Entries []SyncDBEntry `json:"entries"`
}

func main() {
	sourceDir := flag.String("source", "", "Source music directory")
	targetDir := flag.String("target", "", "Target directory (e.g., iPod)")
	targetAudioExtension := flag.String("target-extension", "opus", "Extension for target audio files")
	sourceExtensions := flag.String("source-extensions", "mp3,flac,opus", "Comma-separated list of extensions to transcode from")
	ffmpegCommand := flag.String("ffmpeg-audio", "", "FFmpeg command template (use $INPUT and $OUTPUT for paths)")
	deleteRemovedFiles := flag.Bool("delete-removed", false, "Delete files in target not present in source")

	flag.Parse()

	options = optionsType{
		sourceDir:             *sourceDir,
		targetDir:             *targetDir,
		targetAudioExtension:  *targetAudioExtension,
		sourceAudioExtensions: strings.Split(*sourceExtensions, ","),
		ffmpegCommand:         *ffmpegCommand,
		deleteRemovedFiles:    *deleteRemovedFiles,
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
		if err != nil || info.IsDir() || !isSourceExtension(filepath.Ext(sourcePath)) {
			return err
		}

		relPath, _ := filepath.Rel(options.sourceDir, sourcePath)
		ext := filepath.Ext(sourcePath)
		targetFile := filepath.Join(options.targetDir, strings.TrimSuffix(relPath, ext)+"."+options.targetAudioExtension)
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
			!existingEntry.ModTime.Equal(sourceInfo.ModTime()) ||
			!fileExists(targetFile)

		if needsProcessing {
			os.MkdirAll(filepath.Dir(targetFile), 0755)
			if options.ffmpegCommand != "" {
				cmdStr := strings.ReplaceAll(options.ffmpegCommand, "$INPUT", fmt.Sprintf("\"%s\"", sourcePath))
				cmdStr = strings.ReplaceAll(cmdStr, "$OUTPUT", fmt.Sprintf("\"%s\"", targetFile))
				cmd := exec.Command("bash", "-c", cmdStr)
				if output, err := cmd.CombinedOutput(); err != nil {
					fmt.Printf("Error transcoding %s: %v\nOutput: %s\n", sourcePath, err, string(output))
					return err
				}
				fmt.Printf("Transcoded: %s → %s\n", sourcePath, targetFile)
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

func isSourceExtension(ext string) bool {
	ext = strings.TrimPrefix(strings.ToLower(ext), ".")
	for _, e := range options.sourceAudioExtensions {
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
