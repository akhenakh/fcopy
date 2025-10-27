package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec" // Added for command execution
	"path/filepath"
	"strings"

	"golang.design/x/clipboard"
)

func main() {
	// Define flags
	promptPtr := flag.String("p", "", "A prompt to append after the main file contents")
	followUpFilePtr := flag.String("f", "", "Path to a file whose content will be appended after the prompt, formatted as markdown")
	outputFilePtr := flag.String("o", "", "Output to the specified file instead of clipboard")
	stdoutPtr := flag.Bool("s", false, "Output to stdout instead of clipboard")
	termCopyPtr := flag.Bool("t", false, "Use terminal-aware clipboard (OSC 52, kitty), ideal for SSH") // New flag

	// Custom usage message
	flag.Usage = func() {
		progName := filepath.Base(os.Args[0])
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <path1> [path2 ...]\n", progName)
		fmt.Fprintf(os.Stderr, "Processes files/directories, formats them as markdown, optionally appends a prompt and/or content from another file.\n")
		fmt.Fprintf(os.Stderr, "Output is sent to clipboard by default, or to a file with -o, or to stdout with -s.\n")
		fmt.Fprintf(os.Stderr, "\nArguments (Processed First):\n")
		fmt.Fprintf(os.Stderr, "  <path1> [path2 ...]  Paths to files or directories to process.\n")
		fmt.Fprintf(os.Stderr, "\nOptions (Appended in Order):\n")
		flag.PrintDefaults() // Prints help for all defined flags
		fmt.Fprintf(os.Stderr, "\nOutput Destination (Mutually Exclusive):\n")
		fmt.Fprintf(os.Stderr, "  -o <filepath>      Output to the specified file.\n")
		fmt.Fprintf(os.Stderr, "  -s                 Output to stdout.\n")
		fmt.Fprintf(os.Stderr, "  (default)          Output to clipboard. Use -t for better remote/SSH support.\n")
		fmt.Fprintf(os.Stderr, "  Note: -o, -s, and clipboard output are mutually exclusive.\n")
		fmt.Fprintf(os.Stderr, "\nOrder of Output Content:\n")
		fmt.Fprintf(os.Stderr, "  1. Content from <path1>, <path2>, ...\n")
		fmt.Fprintf(os.Stderr, "  2. Text from -p \"prompt\"\n")
		fmt.Fprintf(os.Stderr, "  3. Formatted content from -f <filepath>\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s internal/ README.md\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -p \"Review this code\" main.go\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -t -p \"Review this Go code over SSH\" main.go\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -o output.md main.go\n", progName)
		fmt.Fprintf(os.Stderr, "  %s -s internal/ | less\n", progName)
	}

	flag.Parse() // Parse the command-line flags

	// Check for mutually exclusive output options
	if *stdoutPtr && *outputFilePtr != "" {
		fmt.Fprintf(os.Stderr, "Error: -s (stdout) and -o (output file) options are mutually exclusive.\n\n")
		flag.Usage()
		os.Exit(1)
	}

	paths := flag.Args() // Get the non-flag arguments (paths)

	if len(paths) == 0 && *promptPtr == "" && *followUpFilePtr == "" {
		flag.Usage()
		os.Exit(1)
	}

	var outputBuilder strings.Builder

	// 1. Process positional path arguments
	for _, argPath := range paths {
		absPath, err := filepath.Abs(argPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting absolute path for %s: %v\n", argPath, err)
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Path does not exist: %s\n", argPath)
			} else {
				fmt.Fprintf(os.Stderr, "Error stating path %s: %v\n", argPath, err)
			}
			continue
		}

		var displayPathBase string
		if filepath.IsAbs(argPath) {
			displayPathBase = filepath.Clean(argPath)
		} else {
			displayPathBase = argPath
		}

		if info.IsDir() {
			processDirectory(absPath, displayPathBase, &outputBuilder)
		} else {
			processFile(absPath, displayPathBase, &outputBuilder)
		}
	}

	// 2. Append the prompt from -p if provided
	promptText := *promptPtr
	if promptText != "" {
		if outputBuilder.Len() > 0 {
			outputBuilder.WriteString("\n\n") // Two newlines for good separation
		}
		outputBuilder.WriteString(promptText)
		fmt.Fprintf(os.Stderr, "Appended prompt text.\n")
	}

	// 3. Append content from the -f file if provided
	followUpFilePath := *followUpFilePtr
	if followUpFilePath != "" {
		absFollowUpPath, err := filepath.Abs(followUpFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting absolute path for follow-up file -f %s: %v\n", followUpFilePath, err)
		} else {
			info, err := os.Stat(absFollowUpPath)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintf(os.Stderr, "Follow-up file -f %s does not exist.\n", followUpFilePath)
				} else {
					fmt.Fprintf(os.Stderr, "Error stating follow-up file -f %s: %v\n", followUpFilePath, err)
				}
			} else if info.IsDir() {
				fmt.Fprintf(os.Stderr, "Error: Path for -f (%s) is a directory, must be a file.\n", followUpFilePath)
			} else {
				var displayFollowUpPath string
				if filepath.IsAbs(followUpFilePath) {
					displayFollowUpPath = filepath.Clean(followUpFilePath)
				} else {
					displayFollowUpPath = followUpFilePath
				}

				if outputBuilder.Len() > 0 {
					outputBuilder.WriteString("\n\n") // Separator
				}
				processFile(absFollowUpPath, displayFollowUpPath, &outputBuilder)
			}
		}
	}

	finalOutput := outputBuilder.String()

	if strings.TrimSpace(finalOutput) == "" {
		fmt.Fprintln(os.Stderr, "Warning: Output is empty or contains only whitespace.")
	} else {
		// Calculate and print the token estimate
		charCount := len(finalOutput)
		// A common heuristic: 1 token is roughly 4 characters.
		// This is a very rough estimate but useful for LLM context windows.
		tokenEstimate := charCount / 4
		fmt.Fprintf(os.Stderr, "Estimated token count: ~%d (based on %d characters)\n", tokenEstimate, charCount)
	}

	// Output handling
	if *stdoutPtr {
		fmt.Print(finalOutput) // Print to stdout
		fmt.Fprintln(os.Stderr, "Content written to stdout.")
	} else if *outputFilePtr != "" {
		filePath := *outputFilePtr
		err := os.WriteFile(filePath, []byte(finalOutput), 0644)
		if err != nil {
			log.Fatalf("Failed to write to output file %s: %v", filePath, err)
		}
		fmt.Fprintf(os.Stderr, "Content written to file: %s\n", filePath)
	} else {
		// Clipboard output (default)
		copyToClipboard(finalOutput, *termCopyPtr)
	}
}

// copyToClipboard handles the logic of copying text to the system clipboard
// using various methods, prioritizing terminal-friendly ones if requested.
func copyToClipboard(content string, useTermAware bool) {
	if strings.TrimSpace(content) == "" {
		fmt.Fprintln(os.Stderr, "No content to copy to clipboard.")
		return
	}

	// Terminal-Aware Copy (OSC 52)
	// This is the best method for remote sessions with compatible terminals (Kitty, etc.)
	if useTermAware {
		term := os.Getenv("TERM")
		// Check for common terminal types that support OSC 52
		if strings.Contains(term, "kitty") || strings.Contains(term, "xterm") || os.Getenv("TMUX") != "" {
			fmt.Fprintln(os.Stderr, "Attempting clipboard copy via OSC 52 escape code...")
			encodedContent := base64.StdEncoding.EncodeToString([]byte(content))
			// OSC 52 format: \x1b]52;c;<base64-data>\x07
			// The 'c' refers to the clipboard selection.
			// Wrap in DCS for tmux compatibility: \x1bPtmux;\x1b\x1b]52;...
			if os.Getenv("TMUX") != "" {
				fmt.Printf("\x1bPtmux;\x1b\x1b]52;c;%s\x07\x1b\\", encodedContent)
			} else {
				fmt.Printf("\x1b]52;c;%s\x07", encodedContent)
			}
			fmt.Fprintln(os.Stderr, "Content sent to terminal for clipboard (OSC 52).")
			return
		}
	}

	// Kitty Kitten Clipboard (Very reliable if inside Kitty)
	// This is a great fallback if OSC 52 is disabled for some reason.
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		// First, check if the 'kitty' command is available in the PATH
		kittyPath, err := exec.LookPath("kitty")
		if err == nil {
			fmt.Fprintln(os.Stderr, "Attempting clipboard copy via `kitty +kitten clipboard`...")
			cmd := exec.Command(kittyPath, "+kitten", "clipboard")
			cmd.Stdin = strings.NewReader(content)
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err == nil {
				fmt.Fprintln(os.Stderr, "Content copied to clipboard via `kitty +kitten clipboard`.")
				return
			}
		}
	}

	// External Command-Line Tools
	// Search for common clipboard utilities.
	tools := []string{"wl-copy", "xclip -selection clipboard", "xsel --clipboard"}
	for _, tool := range tools {
		parts := strings.Fields(tool)
		path, err := exec.LookPath(parts[0])
		if err != nil {
			continue // Tool not found
		}

		fmt.Fprintf(os.Stderr, "Attempting clipboard copy via `%s`...\n", tool)
		cmd := exec.Command(path, parts[1:]...)
		cmd.Stdin = strings.NewReader(content)
		cmd.Stderr = os.Stderr // Show errors from the tool

		if err := cmd.Run(); err == nil {
			fmt.Fprintf(os.Stderr, "Content copied to clipboard via `%s`.\n", tool)
			return
		} else {
			fmt.Fprintf(os.Stderr, "Failed to copy with `%s`: %v\n", tool, err)
		}
	}

	// Fallback to Go Library
	// This often fails over SSH but is a good last resort for local desktop sessions.
	fmt.Fprintln(os.Stderr, "Falling back to default clipboard library (may not work over SSH)...")
	if err := clipboard.Init(); err != nil {
		log.Fatalf("Failed to initialize clipboard library: %v\nPlease install xclip/xsel (for X11) or wl-clipboard (for Wayland), or use the -t flag with a modern terminal.", err)
	}
	clipboard.Write(clipboard.FmtText, []byte(content))
	fmt.Fprintln(os.Stderr, "Content copied to clipboard!")
}

// processDirectory walks a directory and processes all files within it.
func processDirectory(absDirPath string, baseDisplayPath string, builder *strings.Builder) {
	fmt.Fprintf(os.Stderr, "Processing directory: %s\n", baseDisplayPath)
	filepath.WalkDir(absDirPath, func(currentAbsPath string, d fs.DirEntry, errWalk error) error {
		if errWalk != nil {
			fmt.Fprintf(os.Stderr, "Error accessing %s: %v\n", currentAbsPath, errWalk)
			if d == nil {
				return errWalk
			}
			return nil
		}

		if d.IsDir() {
			if currentAbsPath == absDirPath {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") && d.Name() != "." && d.Name() != ".." {
				fmt.Fprintf(os.Stderr, "Skipping hidden directory: %s\n", currentAbsPath)
				return filepath.SkipDir
			}
			return nil
		}

		if strings.HasPrefix(d.Name(), ".") {
			fmt.Fprintf(os.Stderr, "Skipping hidden file: %s\n", currentAbsPath)
			return nil
		}

		relativePathInDir, err := filepath.Rel(absDirPath, currentAbsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error calculating relative path for %s (base %s): %v. Using absolute path for display.\n", currentAbsPath, absDirPath, err)
			processFile(currentAbsPath, currentAbsPath, builder)
			return nil
		}

		displayFilePath := filepath.ToSlash(filepath.Join(baseDisplayPath, relativePathInDir))
		processFile(currentAbsPath, displayFilePath, builder)
		return nil
	})
}

// processFile reads a file and appends its content formatted as a markdown code block to the builder.
func processFile(absFilePath string, displayFilePath string, builder *strings.Builder) {
	content, err := os.ReadFile(absFilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file %s (displaying as %s): %v\n", absFilePath, displayFilePath, err)
		return
	}

	if len(content) > 1*1024*1024 { // 1MB limit
		fmt.Fprintf(os.Stderr, "Skipping large file ( > 1MB): %s\n", displayFilePath)
		return
	}

	isBinary := false
	for i, b := range content {
		if b == 0 {
			if i < 10 && (len(content) > i+1 && content[i+1] == 0) {
				continue
			}
			isBinary = true
			break
		}
	}
	if isBinary {
		fmt.Fprintf(os.Stderr, "Skipping likely binary file (contains null bytes): %s\n", displayFilePath)
		return
	}

	fmt.Fprintf(os.Stderr, "Adding file: %s\n", displayFilePath)

	if builder.Len() > 0 { // Add a separator if there's existing content
		builder.WriteString("\n\n") // Ensures a blank line before the new code block
	}

	lang := getLanguageHint(absFilePath)
	header := displayFilePath
	if lang != "" {
		header = lang + " " + displayFilePath
	}

	builder.WriteString(fmt.Sprintf("```%s\n", header))
	builder.Write(content)
	// Ensure content ends with a newline before closing backticks if it doesn't already
	if len(content) > 0 && content[len(content)-1] != '\n' {
		builder.WriteByte('\n')
	}
	builder.WriteString("```\n") // Single newline after the closing backticks
}

// getLanguageHint determines a language hint from the file extension.
func getLanguageHint(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	baseName := strings.ToLower(filepath.Base(filePath))

	switch baseName {
	case "caddyfile":
		return "caddyfile"
	case "dockerfile", "containerfile":
		return "dockerfile"
	case "makefile":
		return "makefile"
	}

	switch ext {
	case ".go":
		return "go"
	case ".md", ".markdown":
		return "markdown"
	case ".sh", ".bash":
		return "bash"
	case ".py":
		return "python"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cxx", ".hpp", ".hxx", ".cc", ".hh":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".kt", ".kts":
		return "kotlin"
	case ".rs":
		return "rust"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".xml":
		return "xml"
	case ".sql":
		return "sql"
	case ".dockerfile":
		return "dockerfile"
	case ".txt", ".text":
		return "text"
	case "":
		return ""
	default:
		return strings.TrimPrefix(ext, ".")
	}
}
