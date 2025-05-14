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

type options_type struct {
	sourceDir             string
	targetDir             string
	targetAudioExtension  string
	sourceAudioExtensions []string
	ffmpegCommand         string
	deleteRemovedFiles    bool
}

var options options_type

func main() {
	// Define flags
	options.sourceDir = *flag.String("source", "", "Source music directory")
	options.targetDir = *flag.String("target", "", "Target directory (e.g., iPod)")
	options.targetAudioExtension = *flag.String("target-extension", "opus", "Extension for target audio files (Does not change the file format)")
	options.sourceAudioExtensions = strings.Split(*flag.String("source-extensions", "mp3,flac,opus", "Comma-separated list of extensions to transcode from"), ",")
	options.ffmpegCommand = *flag.String("ffmpeg-audio", "", "The ffmpeg command to run on audio files")
	options.deleteRemovedFiles = *flag.Bool("delete-removed", false, "Delete files in target that are not in source")

	flag.Parse()

	if options.sourceDir == "" || options.targetDir == "" {
		fmt.Println("Source and target directories must be specified.")
		flag.Usage()
		os.Exit(1)
	}

	err := os.MkdirAll(options.targetDir, 0755)
	if err != nil {
		fmt.Println("Error creating target directory:", err)
		os.Exit(1)
	}

	err = filepath.Walk(options.sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")

		relPath, err := filepath.Rel(options.sourceDir, path)
		if err != nil {
			return err
		}

		err = os.MkdirAll(filepath.Join(options.targetDir, filepath.Dir(relPath)), 0755)
		if err != nil {
			return err
		}

		targetFile := filepath.Join(options.targetDir, strings.TrimSuffix(relPath, ext)+options.targetAudioExtension)

		if fileNeedsTranscoding(path, targetFile) {
			fmt.Printf("Transcoding: %s → %s\n", path, targetFile)
			cmd := exec.Command("bash", "-c", options.ffmpegCommand)
			cmd.Args = append(cmd.Args, targetFile)
			if err := cmd.Run(); err != nil {
				fmt.Println("Error transcoding file:", err)
				return err
			}
		} else {
			fmt.Printf("Skipping (up-to-date): %s\n", path)
		}
		return nil
	})

	if err != nil {
		fmt.Println("Error during processing:", err)
	}

	fmt.Println("Sync complete!")
}

func fileNeedsTranscoding(source, target string) bool {
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return true
	}

	sourceInfo, err := os.Stat(source)
	if err != nil {
		return false
	}

	if info.Size() != sourceInfo.Size() {
		return true
	}

	return sourceInfo.ModTime().After(info.ModTime())
}

// copyFile copies a file from src to dst. If src and dst files exist, and are
// the same, then return success. Otherise, attempt to create a hard link
// between the two files. If that fail, copy the file contents from src to dst.
func copyFile(src, dst string) (err error) {
	sfi, err := os.Stat(src)
	if err != nil {
		return
	}
	if !sfi.Mode().IsRegular() {
		// cannot copy non-regular files (e.g., directories,
		// symlinks, devices, etc.)
		return fmt.Errorf("CopyFile: non-regular source file %s (%q)", sfi.Name(), sfi.Mode().String())
	}
	dfi, err := os.Stat(dst)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
	} else {
		if !(dfi.Mode().IsRegular()) {
			return fmt.Errorf("CopyFile: non-regular destination file %s (%q)", dfi.Name(), dfi.Mode().String())
		}
		if os.SameFile(sfi, dfi) {
			return
		}
	}
	if err = os.Link(src, dst); err == nil {
		return
	}
	err = copyFileContents(src, dst)
	return
}

// copyFileContents copies the contents of the file named src to the file named
// by dst. The file will be created if it does not already exist. If the
// destination file exists, all it's contents will be replaced by the contents
// of the source file.
func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}

type syncDB struct {
	data struct {
		sourceFiles []struct {
			path string
			size int64
			date time.Time
		}
	}
}

// load loads the syncDB from a file.
func (db *syncDB) Load(path string) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		fmt.Println("Error loading syncDB:", err)
		return
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&db.data)
	if err != nil {
		fmt.Println("Error decoding syncDB:", err)
		return
	}
}

// save saves the syncDB to a file.
func (db *syncDB) Save(path string) {
	file, err := os.Create(path)
	if err != nil {
		fmt.Println("Error saving syncDB:", err)
		return
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(db.data)
	if err != nil {
		fmt.Println("Error encoding syncDB:", err)
		return
	}
	fmt.Println("SyncDB saved to", path)
}

// addFile adds a file to the syncDB.
func (db *syncDB) AddFile(path string, size int64, date time.Time) {
	db.data.sourceFiles = append(db.data.sourceFiles, struct {
		path string
		size int64
		date time.Time
	}{
		path: path,
		size: size,
		date: date,
	})
}
