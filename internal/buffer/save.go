package buffer

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"unicode"
	"unicode/utf8"

	"github.com/zyedidia/micro/internal/config"
	. "github.com/zyedidia/micro/internal/util"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// LargeFileThreshold is the number of bytes when fastdirty is forced
// because hashing is too slow
const LargeFileThreshold = 50000

// overwriteFile opens the given file for writing, truncating if one exists, and then calls
// the supplied function with the file as io.Writer object, also making sure the file is
// closed afterwards.
func overwriteFile(name string, enc encoding.Encoding, fn func(io.Writer) error) (err error) {
	var file *os.File

	if file, err = os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644); err != nil {
		return
	}

	defer func() {
		if e := file.Close(); e != nil && err == nil {
			err = e
		}
	}()

	w := transform.NewWriter(file, enc.NewEncoder())
	// w := bufio.NewWriter(file)

	if err = fn(w); err != nil {
		return
	}

	// err = w.Flush()
	return
}

// Save saves the buffer to its default path
func (b *Buffer) Save() error {
	return b.SaveAs(b.Path)
}

// SaveAs saves the buffer to a specified path (filename), creating the file if it does not exist
func (b *Buffer) SaveAs(filename string) error {
	var err error
	if b.Type.Scratch {
		return errors.New("Cannot save scratch buffer")
	}

	b.UpdateRules()
	if b.Settings["rmtrailingws"].(bool) {
		for i, l := range b.lines {
			leftover := utf8.RuneCount(bytes.TrimRightFunc(l.data, unicode.IsSpace))

			linelen := utf8.RuneCount(l.data)
			b.Remove(Loc{leftover, i}, Loc{linelen, i})
		}

		b.RelocateCursors()
	}

	if b.Settings["eofnewline"].(bool) {
		end := b.End()
		if b.RuneAt(Loc{end.X - 1, end.Y}) != '\n' {
			b.Insert(end, "\n")
		}
	}

	// Update the last time this file was updated after saving
	defer func() {
		b.ModTime, _ = GetModTime(filename)
		err = b.Serialize()
	}()

	// Removes any tilde and replaces with the absolute path to home
	absFilename, _ := ReplaceHome(filename)

	// Get the leading path to the file | "." is returned if there's no leading path provided
	if dirname := filepath.Dir(absFilename); dirname != "." {
		// Check if the parent dirs don't exist
		if _, statErr := os.Stat(dirname); os.IsNotExist(statErr) {
			// Prompt to make sure they want to create the dirs that are missing
			if b.Settings["mkparents"].(bool) {
				// Create all leading dir(s) since they don't exist
				if mkdirallErr := os.MkdirAll(dirname, os.ModePerm); mkdirallErr != nil {
					// If there was an error creating the dirs
					return mkdirallErr
				}
			} else {
				return errors.New("Parent dirs don't exist, enable 'mkparents' for auto creation")
			}
		}
	}

	var fileSize int

	enc, err := htmlindex.Get(b.Settings["encoding"].(string))
	if err != nil {
		return err
	}

	err = overwriteFile(absFilename, enc, func(file io.Writer) (e error) {
		if len(b.lines) == 0 {
			return
		}

		// end of line
		var eol []byte
		if b.Endings == FFDos {
			eol = []byte{'\r', '\n'}
		} else {
			eol = []byte{'\n'}
		}

		// write lines
		if fileSize, e = file.Write(b.lines[0].data); e != nil {
			return
		}

		for _, l := range b.lines[1:] {
			if _, e = file.Write(eol); e != nil {
				return
			}
			if _, e = file.Write(l.data); e != nil {
				return
			}
			fileSize += len(eol) + len(l.data)
		}
		return
	})

	if err != nil {
		return err
	}

	if !b.Settings["fastdirty"].(bool) {
		if fileSize > LargeFileThreshold {
			// For large files 'fastdirty' needs to be on
			b.Settings["fastdirty"] = true
		} else {
			calcHash(b, &b.origHash)
		}
	}

	b.Path = filename
	absPath, _ := filepath.Abs(filename)
	b.AbsPath = absPath
	b.isModified = false
	return err
}

// SaveWithSudo saves the buffer to the default path with sudo
func (b *Buffer) SaveWithSudo() error {
	return b.SaveAsWithSudo(b.Path)
}

// SaveAsWithSudo is the same as SaveAs except it uses a neat trick
// with tee to use sudo so the user doesn't have to reopen micro with sudo
func (b *Buffer) SaveAsWithSudo(filename string) error {
	if b.Type.Scratch {
		return errors.New("Cannot save scratch buffer")
	}

	b.UpdateRules()
	b.Path = filename
	absPath, _ := filepath.Abs(filename)
	b.AbsPath = absPath

	// Set up everything for the command
	cmd := exec.Command(config.GlobalSettings["sucmd"].(string), "tee", filename)
	cmd.Stdin = bytes.NewBuffer(b.Bytes())

	// This is a trap for Ctrl-C so that it doesn't kill micro
	// Instead we trap Ctrl-C to kill the program we're running
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			cmd.Process.Kill()
		}
	}()

	// Start the command
	cmd.Start()
	err := cmd.Wait()

	if err == nil {
		b.isModified = false
		b.ModTime, _ = GetModTime(filename)
		return b.Serialize()
	}
	return err
}
