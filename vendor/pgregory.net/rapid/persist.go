// Copyright 2020 Gregory Petrosyan <gregory.petrosyan@gmail.com>
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package rapid

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	persistDirMode     = 0775
	failfileTmpPattern = ".rapid-failfile-tmp-*"
)

func kindaSafeFilename(f string) string {
	var s strings.Builder
	for _, r := range f {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			s.WriteRune(r)
		} else {
			s.WriteRune('_')
		}
	}
	return s.String()
}

func failFileName(testName string) string {
	ts := time.Now().Format("20060102150405")
	return fmt.Sprintf("%s-%s-%d.fail", kindaSafeFilename(testName), ts, os.Getpid())
}

func saveFailFile(filename string, output []byte, buf []uint64) error {
	dir := filepath.Dir(filename)
	err := os.MkdirAll(dir, persistDirMode)
	if err != nil {
		return fmt.Errorf("failed to create directory for fail file %q: %w", filename, err)
	}

	f, err := ioutil.TempFile(dir, failfileTmpPattern)
	if err != nil {
		return fmt.Errorf("failed to create temporary file for fail file %q: %w", filename, err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	defer func() { _ = f.Close() }()

	out := strings.Split(string(output), "\n")
	for _, s := range out {
		_, err := f.WriteString("# " + s + "\n")
		if err != nil {
			return fmt.Errorf("failed to write data to fail file %q: %w", filename, err)
		}
	}

	var bs []string
	for _, u := range buf {
		bs = append(bs, fmt.Sprintf("0x%x", u))
	}

	_, err = f.WriteString(strings.Join(bs, " "))
	if err != nil {
		return fmt.Errorf("failed to write data to fail file %q: %w", filename, err)
	}

	_ = f.Close() // early close, otherwise os.Rename will fail on Windows
	err = os.Rename(f.Name(), filename)
	if err != nil {
		return fmt.Errorf("failed to save fail file %q: %w", filename, err)
	}

	return nil
}

func loadFailFile(filename string) ([]uint64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open fail file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var data string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		s := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(s, "#") || s == "" {
			continue
		}
		data = s
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to load fail file %q: %w", filename, err)
	}

	var buf []uint64
	fields := strings.Fields(data)
	for _, b := range fields {
		u, err := strconv.ParseUint(b, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to load fail file %q: %w", filename, err)
		}
		buf = append(buf, u)
	}

	return buf, nil
}
