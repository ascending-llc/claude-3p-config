package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/ascending-llc/claude-3p-config/policy"
)

type awsSection struct {
	key  string
	text string
}

var (
	// Capture the header separately from its trailing comment. Like AWS's INI
	// parser, the header extends to the last closing bracket on the line.
	awsSectionBoundary = regexp.MustCompile(`^\s*\[[^\r\n]+\]`)
	awsSectionHeader   = regexp.MustCompile(`^\s*\[\s*(\S+)(?:\s+(\S+))?\s*\]\s*$`)
)

// mergeAWSConfig replaces only the two CCWB sections. The original bytes remain
// available for the change check, including a BOM that must be removed.
func mergeAWSConfig(existing, snippet []byte, newline string) ([]byte, string, bool) {
	content := strings.TrimPrefix(string(existing), "\ufeff")
	if content != "" {
		newline = "\n"
		if strings.Contains(content, "\r\n") {
			newline = "\r\n"
		}
	}

	content = strings.ReplaceAll(content, "\r\n", "\n")
	newText := strings.ReplaceAll(string(snippet), "\r\n", "\n")
	sections := splitAWSSections(content)
	replacements := splitAWSSections(newText)
	wanted := make(map[string]string, 2)

	for _, section := range replacements {
		if isCCWBSection(section.key) {
			wanted[section.key] = section.text
		}
	}

	merged, old := replaceAWSSections(sections, replacements, wanted)
	if len(merged) == 0 {
		result := []byte(strings.ReplaceAll(appendAWSContent(content, newText), "\n", newline))

		return result, "", !bytes.Equal(result, existing)
	}

	var result strings.Builder

	for i, section := range merged {
		text := section.text
		if isCCWBSection(section.key) {
			text = strings.TrimRight(text, "\n") + "\n"
			if i < len(merged)-1 {
				text += "\n"
			}
		}

		result.WriteString(text)
	}

	data := []byte(strings.ReplaceAll(result.String(), "\n", newline))

	return data, strings.ReplaceAll(old, "\n", newline), !bytes.Equal(data, existing)
}

// splitAWSSections keeps each section's text, including comments and whitespace.
// Text before the first header has an empty key. Comments after a section's last
// setting usually describe the next section, so each such block becomes its own
// keyless section, which a merge never replaces or deletes.
func splitAWSSections(text string) []awsSection {
	var sections []awsSection

	start, offset := 0, 0
	key, headed := "", false

	add := func(end int) {
		if end <= start {
			return
		}

		body, comments := text[start:end], ""
		if headed {
			body, comments = splitTrailingComments(body)
		}

		sections = append(sections, awsSection{key: key, text: body})
		if comments != "" {
			sections = append(sections, awsSection{text: comments})
		}
	}

	for _, line := range strings.SplitAfter(text, "\n") {
		header := awsSectionBoundary.FindString(strings.TrimSuffix(line, "\n"))
		if header != "" {
			add(offset)

			start = offset
			key, headed = "", true

			if match := awsSectionHeader.FindStringSubmatch(header); match != nil {
				key = match[1] + " " + match[2]
			}
		}

		offset += len(line)
	}

	add(len(text))

	return sections
}

// splitTrailingComments splits off the comment lines after a section's last
// setting, together with any blank lines among or after them. Blank lines before
// the first such comment stay with the section. The header line is never split off.
func splitTrailingComments(text string) (body, comments string) {
	lines := strings.SplitAfter(text, "\n")
	cut, offset := len(text), len(text)

	for i := len(lines) - 1; i > 0; i-- {
		offset -= len(lines[i])

		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}

		if trimmed[0] != '#' && trimmed[0] != ';' {
			break
		}

		cut = offset
	}

	return text[:cut], text[cut:]
}

func isCCWBSection(key string) bool {
	return key == "sso-session "+policy.SSOSessionName || key == "profile "+policy.ProfileName
}

// replaceAWSSections returns nil when neither target exists, so the caller can
// append the entire snippet, including its introductory comment.
func replaceAWSSections(sections, replacements []awsSection, wanted map[string]string) ([]awsSection, string) {
	seen := make(map[string]bool, 2)
	merged := make([]awsSection, 0, len(sections)+2)

	var old strings.Builder

	for _, section := range sections {
		replacement, target := wanted[section.key]
		if !target {
			merged = append(merged, section)

			continue
		}

		if seen[section.key] {
			old.WriteString(section.text)

			continue
		}

		if strings.TrimRight(section.text, "\n") != strings.TrimRight(replacement, "\n") {
			old.WriteString(section.text)
		}

		seen[section.key] = true
		merged = append(merged, awsSection{key: section.key, text: replacement})
	}

	if len(seen) == 0 {
		return nil, ""
	}

	for _, section := range replacements {
		if !isCCWBSection(section.key) || seen[section.key] {
			continue
		}

		last := &merged[len(merged)-1]
		last.text = strings.TrimRight(last.text, "\n") + "\n\n"

		merged = append(merged, section)
	}

	return merged, old.String()
}

func appendAWSContent(existing, addition string) string {
	if existing == "" {
		return addition
	}

	return strings.TrimRight(existing, "\n") + "\n\n" + addition
}

func awsConfigPath() (string, error) {
	path := os.Getenv("AWS_CONFIG_FILE")
	if path != "" && !strings.HasPrefix(path, "~") {
		return filepath.Clean(path), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the user home directory: %s", err.Error())
	}

	if path == "" {
		return filepath.Join(home, ".aws", "config"), nil
	}

	return filepath.Join(home, strings.TrimLeft(strings.TrimPrefix(path, "~"), `/\`)), nil
}

func updateAWSConfig(path string, in *bufio.Scanner, out io.Writer, s policy.CodexSetup) error {
	path = filepath.Clean(path)

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %s", path, err.Error())
	}

	exists := err == nil
	newline := "\n"

	if runtime.GOOS == "windows" {
		newline = "\r\n"
	}

	snippet := s.AWSConfig()

	merged, old, changed := mergeAWSConfig(existing, snippet, newline)
	if !changed {
		_, err := fmt.Fprintln(out, `.aws\config already has the CCWB settings.`)

		return err
	}

	if old != "" {
		if _, err := fmt.Fprintf(out, "Current CCWB sections:\n%s\nNew CCWB sections:\n%s\n", old, snippet); err != nil {
			return err
		}

		replace, err := confirm(in, out, "Replace them? [Y/n]")
		if err != nil {
			return err
		}

		if !replace {
			_, err := fmt.Fprintln(out, `.aws\config was left unchanged.`)

			return err
		}
	}

	if err := ensureAWSConfigUnchanged(path, existing, exists); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating the AWS config folder: %s", err.Error())
	}

	if exists {
		backup := path + ".bak-" + time.Now().Format("20060102-150405")
		if err := backupAWSConfig(backup, existing); err != nil {
			return err
		}
	}

	if err := writeAWSConfig(path, merged); err != nil {
		return err
	}

	_, outputErr := fmt.Fprintln(out, `Updated .aws\config with the CCWB settings.`)

	return outputErr
}

// ensureAWSConfigUnchanged rejects edits made since the initial read, before
// creating a backup or writing settings. It does not lock out other processes.
func ensureAWSConfigUnchanged(path string, original []byte, existed bool) error {
	current, err := os.ReadFile(filepath.Clean(path))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("checking the AWS config before writing: %s", err.Error())
	}

	if existed != (err == nil) || !bytes.Equal(current, original) {
		return fmt.Errorf("%s changed while setup was running; no settings were written; run setup again", path)
	}

	return nil
}

func backupAWSConfig(path string, data []byte) error {
	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating the AWS config backup: %s", err.Error())
	}

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)

		return fmt.Errorf("writing the AWS config backup: %s", err.Error())
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(path)

		return fmt.Errorf("closing the AWS config backup: %s", err.Error())
	}

	return nil
}

func writeAWSConfig(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".ccwb-config-*")
	if err != nil {
		return fmt.Errorf("creating a temporary AWS config: %s", err.Error())
	}

	defer func() { _ = os.Remove(file.Name()) }()

	if _, err := file.Write(data); err != nil {
		_ = file.Close()

		return fmt.Errorf("writing the AWS config: %s", err.Error())
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("closing the AWS config: %s", err.Error())
	}

	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replacing the AWS config: %s", err.Error())
	}

	return nil
}
